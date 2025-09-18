/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certificate

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	goerrors "errors"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/maps"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	CASecretName        = "submariner-ca"
	CertValidity        = 365 * 24 * time.Hour // 1 year // 10 years
	CAKeyFileName       = "ca.key"
	CACertFileName      = "ca.crt"
	CAVersionAnnotation = "submariner.io/ca-version"
)

type SignerConfig struct {
	RestConfig *rest.Config
	DynClient  dynamic.Interface
	RestMapper meta.RESTMapper
}

type Signer interface {
	Start(ctx context.Context, namespace string) error
	Stop(namespace string)
}

type signerImpl struct {
	restMapper meta.RESTMapper
	dynClient  dynamic.Interface
	syncerMap  sync.Map
}

var (
	CACheckInterval = 24 * time.Hour
	CACertValidity  = 10 * 365 * 24 * time.Hour // Check CA daily
	RotateBefore    = 90 * 24 * time.Hour       // 90 days
)

func NewSigner(config SignerConfig) (Signer, error) {
	s := &signerImpl{}

	var (
		err  error
		errs []error
	)

	if config.RestMapper == nil {
		s.restMapper, err = util.BuildRestMapper(config.RestConfig)
		errs = append(errs, errors.Wrap(err, "error building the REST mapper"))
	}

	if config.DynClient == nil {
		s.dynClient, err = resource.NewDynamicClient(config.RestConfig)
		errs = append(errs, errors.Wrap(err, "error creating dynamic client"))
	}

	return s, goerrors.Join(errs...)
}

func (s *signerImpl) Start(ctx context.Context, namespace string) error {
	if _, exists := s.syncerMap.Load(namespace); exists {
		return nil
	}

	// Issue/check CA certificate before starting the syncer
	if err := s.issueCA(ctx, namespace); err != nil {
		return errors.Wrapf(err, "error issuing CA certificate for namespace %s", namespace)
	}

	stopCh := make(chan struct{})

	//nolint:contextcheck // NewResourceSyncer doesn't accept context parameter
	secretSyncer, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
		Name:            "Cert Signer",
		SourceClient:    s.dynClient,
		SourceNamespace: namespace,
		RestMapper:      s.restMapper,
		ResourceType:    &corev1.Secret{},
		ShouldProcess: func(obj *unstructured.Unstructured, op syncer.Operation) bool {
			return op != syncer.Delete && obj.GetLabels()[SigningRequestLabelKey] != "" && obj.GetAnnotations()[RequestSignedLabelKey] == ""
		},
		Transform: func(from runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
			secret := from.(*corev1.Secret)

			err := s.signSecret(wait.ContextForChannel(stopCh), secret)
			if err != nil {
				logger.Errorf(err, "error signing Secret %q", secret.Name)
			}

			return secret, err != nil
		},
		Federator: federate.NewUpdateFederator(s.dynClient, s.restMapper, namespace,
			func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured {
				existingSecret := resource.MustFromUnstructured(oldObj, &corev1.Secret{})
				updatedSecret := resource.MustFromUnstructured(newObj, &corev1.Secret{})

				maps.Ensure(&existingSecret.Annotations)[RequestSignedLabelKey] = "true"
				existingSecret.Data[TLSDataKey] = updatedSecret.Data[TLSDataKey]
				existingSecret.Data[CADataKey] = updatedSecret.Data[CADataKey]

				return resource.MustToUnstructured(existingSecret)
			}),
	})
	if err != nil {
		return errors.Wrap(err, "error creating resource syncer")
	}

	if err := secretSyncer.Start(stopCh); err != nil {
		return errors.Wrap(err, "error starting resource syncer")
	}

	s.syncerMap.Store(namespace, stopCh)

	// Start periodic CA check for this namespace
	//nolint:contextcheck // startPeriodicCACheck uses its own context on another thread.
	s.startPeriodicCACheck(namespace, stopCh)

	return nil
}

func (s *signerImpl) Stop(namespace string) {
	if v, exists := s.syncerMap.LoadAndDelete(namespace); exists {
		close(v.(chan struct{}))
	}
}

func (s *signerImpl) signSecret(ctx context.Context, secret *corev1.Secret) error {
	// Get CA secret using dynamic client
	caSecretClient := s.dynClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(secret.Namespace)
	caSecretUnstructured, err := caSecretClient.Get(ctx, CASecretName, metav1.GetOptions{})
	if err != nil {
		return errors.Wrapf(err, "failed to get CA secret for signing secret %q", secret.Name)
	}

	caSecret := resource.MustFromUnstructured(caSecretUnstructured, &corev1.Secret{})

	caCertPEM := caSecret.Data[CACertFileName]
	caKeyPEM := caSecret.Data[CAKeyFileName]

	caCert, err := ParseCertificateFromPEM(caCertPEM)
	if err != nil {
		return errors.Wrapf(err, "failed to parse CA certificate for signing secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	caKey, err := ParsePKCS1PrivateKeyFromPEM(caKeyPEM)
	if err != nil {
		return errors.Wrapf(err, "failed to parse CA private key for signing secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	csr, err := ParseCertificateRequestFromPEM(secret.Data[CSRDataKey])
	if err != nil {
		return errors.Wrapf(err, "failed to parse CSR PEM for secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	if err := csr.CheckSignature(); err != nil {
		return errors.Wrapf(err, "CSR signature invalid for secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	// Create certificate from CSR
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return errors.Wrapf(err, "failed to generate serial number for secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	certTemplate := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(CertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		IPAddresses:  csr.IPAddresses, // Copy IP SANs from CSR
		DNSNames:     csr.DNSNames,    // Copy DNS SANs from CSR
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, caCert, csr.PublicKey, caKey)
	if err != nil {
		return errors.Wrapf(err, "failed to sign CSR for secret \"%s/%s\"", secret.Namespace, secret.Name)
	}

	signedCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	secret.Data[TLSDataKey] = signedCertPEM
	secret.Data[CADataKey] = caCertPEM

	return nil
}

// issueCA issues a CA certificate in the specified namespace, creating or rotating it if necessary.
func (s *signerImpl) issueCA(ctx context.Context, namespace string) error {
	caSecretClient := s.dynClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(namespace)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CASecretName,
			Namespace: namespace,
			Annotations: map[string]string{
				CAVersionAnnotation: "1", // Default version for new CA
			},
		},
	}

	result, _, err := util.CreateOrUpdateWithOptions(ctx, util.CreateOrUpdateOptions[*unstructured.Unstructured]{
		Client: resource.ForDynamic(caSecretClient),
		Obj:    resource.MustToUnstructured(secret),
		MutateOnCreate: func(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
			secret := resource.MustFromUnstructured(obj, &corev1.Secret{})
			err := s.signCASecret(secret, secret.Annotations[CAVersionAnnotation])
			return resource.MustToUnstructured(secret), err
		},
		MutateOnUpdate: func(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
			existing := resource.MustFromUnstructured(obj, &corev1.Secret{})

			shouldReissue, newVersion := s.shouldReissueCA(existing, namespace)

			if !shouldReissue {
				return obj, nil // No update needed
			}

			err := s.signCASecret(existing, newVersion)
			return resource.MustToUnstructured(existing), err
		},
	})
	if result == util.OperationResultNone || err != nil {
		return errors.Wrapf(err, "failed to create or update CA Secret")
	}

	if result == util.OperationResultCreated {
		logger.Infof("Successfully created CA Secret %s in namespace %s", secret.Name, namespace)
	} else if result == util.OperationResultUpdated {
		logger.Infof("Successfully rotated CA Secret %s in namespace %s", secret.Name, namespace)
	}

	// Re-sign all existing CSRs with the new CA (handles both creation and rotation cases)
	if err := s.resignAllCSRs(ctx, namespace); err != nil {
		logger.Error(err, "Failed to re-sign existing CSRs with new CA", "namespace", namespace)
		// Don't return error - CA creation succeeded, CSR re-signing is best effort
	}

	return nil
}

// signCASecret generates and signs a CA certificate, storing it in the provided secret.
func (s *signerImpl) signCASecret(secret *corev1.Secret, version string) error {
	// Generate new CA certificate
	keyPEM, certPEM, err := CreatePEMEncodedKeyAndCertificate("submariner-ca", CACertValidity)

	maps.Ensure(&secret.Data)[CAKeyFileName] = keyPEM
	secret.Data[CACertFileName] = certPEM

	// Ensure annotations exist and set the version
	maps.Ensure(&secret.Annotations)[CAVersionAnnotation] = version

	return errors.Wrapf(err, "failed to create CA certificate")
}

// shouldReissueCA checks if the CA certificate should be reissued based on expiration time.
// Returns (shouldReissue, newVersion, error).
func (s *signerImpl) shouldReissueCA(secret *corev1.Secret, namespace string) (bool, string) {
	// Get current version from existing secret
	currentVersion := "1"
	if existingVersion, exists := secret.Annotations[CAVersionAnnotation]; exists {
		currentVersion = existingVersion
	}

	cert, err := ParseCertificateFromPEM(secret.Data[CACertFileName])
	if err != nil {
		logger.Errorf(err, "Failed to parse existing CA cert, re-issuing for namespace %q", namespace)
		return true, currentVersion
	}

	timeRemaining := time.Until(cert.NotAfter)
	logger.V(log.TRACE).Info("Existing CA", "namespace", namespace, "expiresIn", timeRemaining.String(),
		"notAfter", cert.NotAfter.Format(time.RFC3339), "version", currentVersion)

	if timeRemaining < RotateBefore {
		newVersion := s.incrementVersion(currentVersion)
		logger.Infof("CA is expiring in %s — rotating it for namespace %s from version %s to %s",
			timeRemaining, namespace, currentVersion, newVersion)

		return true, newVersion
	}

	logger.V(log.TRACE).Info("CA is still valid — no rotation needed", "namespace", namespace, "version", currentVersion)

	return false, currentVersion
}

// incrementVersion increments the CA version string, handling both numeric and non-numeric versions.
func (s *signerImpl) incrementVersion(currentVersion string) string {
	if version, err := strconv.Atoi(currentVersion); err == nil {
		return strconv.Itoa(version + 1)
	}

	// Fallback if current version is not a number
	logger.Warningf("Current CA version %s is not a number, falling back to version 1", currentVersion)

	return "1"
}

// startPeriodicCACheck starts a periodic check for CA certificate expiration.
func (s *signerImpl) startPeriodicCACheck(namespace string, stopCh <-chan struct{}) {
	go wait.Until(func() {
		logger.V(log.TRACE).Infof("Performing periodic CA check for namespace %q", namespace)

		// CA exists, check if it needs rotation
		if err := s.issueCA(wait.ContextForChannel(stopCh), namespace); err != nil {
			logger.Errorf(err, "Failed periodic CA check for namespace %q", namespace)
		}
	}, CACheckInterval, stopCh)

	logger.Infof("Started periodic CA check for namespace %s with interval %s", namespace, CACheckInterval)
}

// resignAllCSRs finds all existing CSR secrets and re-signs them with the new CA.
func (s *signerImpl) resignAllCSRs(ctx context.Context, namespace string) error {
	secretClient := s.dynClient.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(namespace)

	// List all secrets with the CSR request label
	secretList, err := secretClient.List(ctx, metav1.ListOptions{
		LabelSelector: SigningRequestLabelKey,
	})
	if err != nil {
		return errors.Wrapf(err, "failed to list CSR secrets")
	}

	resignCount := 0

	for _, item := range secretList.Items {
		err := util.Update(ctx, resource.ForDynamic(secretClient), &item,
			func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
				annotations := existing.GetAnnotations()
				delete(annotations, RequestSignedLabelKey)
				existing.SetAnnotations(annotations)

				return existing, nil
			})

		if err != nil {
			logger.Errorf(err, "Failed to mark CSR secret \"%s/%s\" for re-signing", namespace, item.GetName())
		} else {
			logger.Infof("Marked CSR secret \"%s/%s\" for re-signing", namespace, item.GetName())
		}

		resignCount++
	}

	return nil
}
