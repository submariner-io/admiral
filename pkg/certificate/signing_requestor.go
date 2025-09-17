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
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	goerrors "errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/slices"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/watcher"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	SigningRequestLabelKey = "submariner.io/csr-request"
	RequestSignedLabelKey  = "submariner.io/csr-request-signed"
	PrivateKeyDataKey      = "tls.key"
	RSABitSize             = 2048
	CSRDataKey             = "csr.pem"
	TLSDataKey             = "tls.crt"
	CADataKey              = "ca.crt"

	// Certificate renewal constants.
	CertRenewBefore   = 30 * 24 * time.Hour // Renew 30 days before expiration
	CertCheckInterval = 12 * time.Hour      // Check certificate expiration every 12 hours
)

type OnSignedFn func(secretData map[string][]byte) error

type KeyGeneratorFn func(ips []string) ([]byte, []byte, error)

type certInfo struct {
	name      string
	ips       []string
	onSigned  OnSignedFn
	expiresAt time.Time
}

type SigningRequestor interface {
	Issue(ctx context.Context, name string, ips []string, onSigned OnSignedFn) error
	Remove(ctx context.Context, name string) error
	Uninstall(ctx context.Context) error

	// SetKeyGenerator is intended for unit tests.
	SetKeyGenerator(kg KeyGeneratorFn)
}

type signingRequestorImpl struct {
	localNamespace     string
	localClusterID     string
	localSecretClient  dynamic.ResourceInterface
	brokerSecretClient dynamic.ResourceInterface
	keyGenerator       KeyGeneratorFn

	// Certificate management fields
	issuedCerts sync.Map // map[secretName]certInfo
}

var logger = log.Logger{Logger: logf.Log.WithName("Certificate")}

//nolint:gocritic // Ignore hugeParam - minimal performance hit, we modify our copy
func StartSigningRequestor(syncerConfig broker.SyncerConfig, stopCh <-chan struct{}) (SigningRequestor, error) {
	sr := &signingRequestorImpl{
		localNamespace: syncerConfig.LocalNamespace,
		localClusterID: syncerConfig.LocalClusterID,
	}

	sr.keyGenerator = sr.generateKeyAndCSR

	syncerConfig.Name = "CertSR"

	localFederator := federate.NewUpdateFederator(syncerConfig.LocalClient, syncerConfig.RestMapper, syncerConfig.LocalNamespace,
		func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured {
			existingSecret := resource.MustFromUnstructured(oldObj, &corev1.Secret{})
			updatedSecret := resource.MustFromUnstructured(newObj, &corev1.Secret{})

			if existingSecret.Annotations == nil {
				existingSecret.Annotations = map[string]string{}
			}

			existingSecret.Annotations[RequestSignedLabelKey] = updatedSecret.Annotations[RequestSignedLabelKey]
			existingSecret.Data[TLSDataKey] = updatedSecret.Data[TLSDataKey]
			existingSecret.Data[CADataKey] = updatedSecret.Data[CADataKey]

			return resource.MustToUnstructured(existingSecret)
		})
	localFederator.LogEvents(syncerConfig.Name + ":broker -> local")

	labelSelector := k8slabels.SelectorFromSet(map[string]string{
		SigningRequestLabelKey: sr.localClusterID,
	}).String()

	syncerConfig.LocalClusterID = ""
	syncerConfig.ResourceConfigs = []broker.ResourceConfig{
		{
			LocalSourceNamespace:     syncerConfig.LocalNamespace,
			LocalSourceLabelSelector: labelSelector,
			LocalResourceType:        &corev1.Secret{},
			LocalShouldProcess: func(obj *unstructured.Unstructured, op syncer.Operation) bool {
				return op == syncer.Delete || obj.GetAnnotations()[RequestSignedLabelKey] == ""
			},
			LocalResourcesEquivalent: func(obj1, obj2 *unstructured.Unstructured) bool {
				secret1 := resource.MustFromUnstructured(obj1, &corev1.Secret{})
				secret2 := resource.MustFromUnstructured(obj2, &corev1.Secret{})
				return bytes.Equal(secret1.Data[CSRDataKey], secret2.Data[CSRDataKey])
			},
			LocalFederator: localFederator,
			TransformLocalToBroker: func(from runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
				// We don't sync the private data key to the broker for security.
				delete(from.(*corev1.Secret).Data, PrivateKeyDataKey)
				return from, false
			},
			BrokerResourceType: &corev1.Secret{},
			BrokerShouldProcess: func(obj *unstructured.Unstructured, op syncer.Operation) bool {
				return op == syncer.Update && obj.GetAnnotations()[RequestSignedLabelKey] != ""
			},
		},
	}

	brokerSyncer, err := broker.NewSyncer(syncerConfig)
	if err != nil {
		return nil, errors.Wrap(err, "error creating broker syncer")
	}

	if err := brokerSyncer.Start(stopCh); err != nil {
		return nil, errors.Wrap(err, "error starting broker syncer")
	}

	sr.brokerSecretClient = brokerSyncer.GetBrokerClient().Resource(corev1.SchemeGroupVersion.WithResource("secrets")).
		Namespace(brokerSyncer.GetBrokerNamespace())

	sr.localSecretClient = brokerSyncer.GetLocalClient().Resource(corev1.SchemeGroupVersion.WithResource("secrets")).
		Namespace(syncerConfig.LocalNamespace)

	localSecretWatcher, err := watcher.New(&watcher.Config{
		RestConfig: syncerConfig.LocalRestConfig,
		RestMapper: syncerConfig.RestMapper,
		Client:     syncerConfig.LocalClient,
		Scheme:     syncerConfig.Scheme,
		ResourceConfigs: []watcher.ResourceConfig{
			{
				Name:                syncerConfig.Name + ":local Secret watcher",
				ResourceType:        &corev1.Secret{},
				SourceLabelSelector: labelSelector,
				ShouldProcess: func(obj *unstructured.Unstructured, op syncer.Operation) bool {
					return op != syncer.Delete && obj.GetAnnotations()[RequestSignedLabelKey] != ""
				},
				Handler: watcher.EventHandlerFuncs{
					OnCreateFunc: sr.onLocalSecretSigned,
					OnUpdateFunc: sr.onLocalSecretSigned,
				},
				SourceNamespace: syncerConfig.LocalNamespace,
			},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "error creating secret watcher")
	}

	if err := localSecretWatcher.Start(stopCh); err != nil {
		return nil, errors.Wrap(err, "error starting secret watcher")
	}

	// Start certificate renewal monitoring
	sr.startCertificateRenewalMonitoring(stopCh)

	return sr, nil
}

func (s *signingRequestorImpl) Issue(ctx context.Context, name string, ips []string, onSigned OnSignedFn) error {
	if onSigned == nil {
		return errors.New("OnSignedFn cannot be nil")
	}

	if len(ips) == 0 {
		return errors.New("ips cannot be empty")
	}

	keyPEM, csrPEM, err := s.keyGenerator(ips)
	if err != nil {
		return errors.Wrapf(err, "error generating key and CSR data for %q and IPs %v", name, ips)
	}

	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.secretName(name),
			Namespace: s.localNamespace,
			Labels: map[string]string{
				SigningRequestLabelKey: s.localClusterID,
			},
		},
		Data: map[string][]byte{
			PrivateKeyDataKey: keyPEM,
			CSRDataKey:        csrPEM,
		},
	}

	result, _, err := util.CreateOrUpdateWithOptions(ctx, util.CreateOrUpdateOptions[*unstructured.Unstructured]{
		Client: resource.ForDynamic(s.localSecretClient),
		Obj:    resource.MustToUnstructured(newSecret),
		MutateOnUpdate: func(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
			existing := resource.MustFromUnstructured(obj, &corev1.Secret{})

			// Check if we need to regenerate the CSR by comparing the IPs
			// We need to extract the IPs from the existing CSR to compare
			var needsNewCSR bool

			// Parse existing CSR to extract IPs and compare
			existingIPs, err := s.extractIPsFromCSR(existing.Data[CSRDataKey])
			if err != nil {
				logger.Warningf("Failed to parse existing CSR for secret %q: %v", existing.Name, err)
				needsNewCSR = true
			} else {
				// Compare IP lists
				needsNewCSR = !slices.Equivalent(existingIPs, ips, slices.Key[string])
			}

			if needsNewCSR {
				existing.Data[PrivateKeyDataKey] = newSecret.Data[PrivateKeyDataKey]
				existing.Data[CSRDataKey] = newSecret.Data[CSRDataKey]

				// Clear signed annotation since we have new CSR data
				delete(existing.Annotations, RequestSignedLabelKey)
			}
			// If needsNewCSR is false, we preserve the existing CSR data and signed state

			return resource.MustToUnstructured(existing), nil
		},
	})
	if err == nil {
		// Track certificate for renewal (expiration will be set when certificate is signed)
		s.issuedCerts.Store(newSecret.Name, certInfo{
			name:      name,
			ips:       ips,
			onSigned:  onSigned,
			expiresAt: time.Time{}, // Will be updated when certificate is signed
		})
	}

	if result == util.OperationResultCreated {
		logger.Infof("Successfully created CSR Secret %q", newSecret.Name)
	} else if result == util.OperationResultUpdated {
		logger.Infof("Successfully updated CSR Secret %q", newSecret.Name)
	}

	return errors.Wrapf(err, "error creating or updating CSR Secret %q", newSecret.Name)
}

func (s *signingRequestorImpl) Remove(ctx context.Context, name string) error {
	secretName := s.secretName(name)
	s.issuedCerts.Delete(secretName)

	return goerrors.Join(deleteIfPresent(ctx, s.localSecretClient, secretName),
		deleteIfPresent(ctx, s.brokerSecretClient, secretName))
}

func (s *signingRequestorImpl) Uninstall(ctx context.Context) error {
	// Clear certificate tracking
	s.issuedCerts.Range(func(key, value interface{}) bool {
		s.issuedCerts.Delete(key)
		return true
	})

	listOpts := metav1.ListOptions{
		LabelSelector: k8slabels.SelectorFromSet(map[string]string{
			SigningRequestLabelKey: s.localClusterID,
		}).String(),
	}

	return goerrors.Join(s.localSecretClient.DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts),
		s.brokerSecretClient.DeleteCollection(ctx, metav1.DeleteOptions{}, listOpts))
}

func (s *signingRequestorImpl) SetKeyGenerator(kg KeyGeneratorFn) {
	s.keyGenerator = kg
}

func (s *signingRequestorImpl) generateKeyAndCSR(ips []string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, RSABitSize)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to generate RSA key")
	}

	ipAddresses := []net.IP{}

	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return nil, nil, errors.New("invalid IP address in SAN: " + ip)
		}

		ipAddresses = append(ipAddresses, parsed)
	}

	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   "submariner-" + s.localClusterID,
			Organization: []string{"submariner.io"},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
		IPAddresses:        ipAddresses,
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, privateKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create certificate request")
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	return keyPEM, csrPEM, nil
}

func (s *signingRequestorImpl) onLocalSecretSigned(obj runtime.Object, numRequeues int) bool {
	// warningLogInterval defines how often to log warnings for missing callbacks.
	const warningLogInterval = 12

	secret := obj.(*corev1.Secret)

	v, ok := s.issuedCerts.Load(secret.Name)
	if !ok {
		if numRequeues > 0 && numRequeues%warningLogInterval == 0 {
			logger.Warningf("Received signed Secret for %q with no OnSigned callback registered", secret.Name)
		}

		return true
	}

	certInfo := v.(certInfo)

	// Update certificate expiration time for renewal tracking
	err := s.updateCertificateExpiration(secret)
	if err != nil {
		logger.Errorf(err, "Failed to update certificate expiration for secret %q", secret.Name)
		return false
	}

	err = certInfo.onSigned(secret.Data)
	if err == nil {
		return false
	}

	logger.Errorf(err, "OnSignedFn returned an error for secret %q", secret.Name)

	return true
}

func (s *signingRequestorImpl) secretName(name string) string {
	return fmt.Sprintf("%s-%s", name, s.localClusterID)
}

func deleteIfPresent(ctx context.Context, client dynamic.ResourceInterface, name string) error {
	err := client.Delete(ctx, name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}

	return errors.Wrapf(err, "error deleting Secret %q", name)
}

// startCertificateRenewalMonitoring starts periodic monitoring of certificate expiration.
func (s *signingRequestorImpl) startCertificateRenewalMonitoring(stopCh <-chan struct{}) {
	go wait.Until(s.checkCertificateRenewal, CertCheckInterval, stopCh)

	logger.Infof("Started certificate renewal monitoring with interval %s", CertCheckInterval)
}

// updateCertificateExpiration extracts and stores the certificate expiration time.
func (s *signingRequestorImpl) updateCertificateExpiration(secret *corev1.Secret) error {
	certPEM := secret.Data[TLSDataKey]

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.Errorf("failed to find certificate PEM in %q for secret %q", TLSDataKey, secret.Name)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return errors.Wrapf(err, "failed to parse certificate for secret %q", secret.Name)
	}

	// Update the expiration time in our tracking map
	if v, ok := s.issuedCerts.Load(secret.Name); ok {
		info := v.(certInfo)
		info.expiresAt = cert.NotAfter
		s.issuedCerts.Store(secret.Name, info)

		logger.Infof("Updated certificate expiration for secret %q: expires at %s",
			secret.Name, cert.NotAfter.Format(time.RFC3339))
	}

	return nil
}

// checkCertificateRenewal checks all tracked certificates and renews those close to expiration.
func (s *signingRequestorImpl) checkCertificateRenewal() {
	s.issuedCerts.Range(func(key, value interface{}) bool {
		secretName := key.(string)
		info := value.(certInfo)

		// Skip if expiration time is not set yet
		if info.expiresAt.IsZero() {
			return true
		}

		// Check if certificate needs renewal
		timeUntilExpiry := time.Until(info.expiresAt)
		if timeUntilExpiry <= CertRenewBefore {
			logger.Infof("Certificate %q expires in %s, renewing", secretName, timeUntilExpiry.String())

			// Renew the certificate by re-issuing it
			if err := s.Issue(context.TODO(), info.name, info.ips, info.onSigned); err != nil {
				logger.Errorf(err, "Failed to renew certificate %q", secretName)
			} else {
				logger.Infof("Successfully initiated renewal for certificate %q", secretName)
			}
		} else {
			logger.V(log.TRACE).Infof("Certificate %q expires in %s, no renewal needed", secretName, timeUntilExpiry.String())
		}

		return true
	})
}

// extractIPsFromCSR parses a CSR PEM and extracts the IP addresses from the Subject Alternative Names.
func (s *signingRequestorImpl) extractIPsFromCSR(csrPEM []byte) ([]string, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, errors.New("failed to decode CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse CSR")
	}

	ips := make([]string, 0, len(csr.IPAddresses))
	for _, ip := range csr.IPAddresses {
		ips = append(ips, ip.String())
	}

	return ips, nil
}
