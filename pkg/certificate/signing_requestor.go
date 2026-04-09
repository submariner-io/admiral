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
	goerrors "errors"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/maps"
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
	CertRenewBefore        = 30 * 24 * time.Hour // Renew 30 days before expiration
	CertCheckInterval      = 12 * time.Hour      // Check certificate expiration every 12 hours
)

type OnSignedFn func(secretData map[string][]byte) error

type certInfo struct {
	onSigned  OnSignedFn
	expiresAt time.Time
}

type SigningRequestor interface {
	Issue(ctx context.Context, name string, ips []string, onSigned OnSignedFn) error
	Remove(ctx context.Context, name string) error
	Uninstall(ctx context.Context) error
}

type signingRequestorImpl struct {
	localNamespace     string
	localClusterID     string
	localSecretClient  dynamic.ResourceInterface
	brokerSecretClient dynamic.ResourceInterface
	certRenewBefore    time.Duration
	certCheckInterval  time.Duration

	// Certificate management fields
	issuedCerts sync.Map // map[secretName]certInfo
}

var logger = log.Logger{Logger: logf.Log.WithName("Certificate")}

//nolint:gocritic // Ignore hugeParam - minimal performance hit, we modify our copy
func StartSigningRequestor(ctx context.Context, syncerConfig broker.SyncerConfig, stopCh <-chan struct{}) (SigningRequestor, error) {
	return StartSigningRequestorWithOpts(ctx, syncerConfig, stopCh, CertCheckInterval, CertRenewBefore)
}

//nolint:gocritic // Ignore hugeParam - minimal performance hit, we modify our copy
func StartSigningRequestorWithOpts(ctx context.Context, syncerConfig broker.SyncerConfig, stopCh <-chan struct{},
	certCheckInterval time.Duration, certRenewBefore time.Duration,
) (SigningRequestor, error) {
	sr := &signingRequestorImpl{
		localNamespace:    syncerConfig.LocalNamespace,
		localClusterID:    syncerConfig.LocalClusterID,
		certRenewBefore:   certRenewBefore,
		certCheckInterval: certCheckInterval,
	}

	syncerConfig.Name = "CertSR"

	localFederator := federate.NewUpdateFederator(syncerConfig.LocalClient, syncerConfig.RestMapper, syncerConfig.LocalNamespace,
		func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured {
			existingSecret := resource.MustFromUnstructured(oldObj, &corev1.Secret{})
			updatedSecret := resource.MustFromUnstructured(newObj, &corev1.Secret{})

			maps.Ensure(&existingSecret.Annotations)[RequestSignedLabelKey] = updatedSecret.Annotations[RequestSignedLabelKey]
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

	brokerSyncer, err := broker.NewSyncer(ctx, syncerConfig)
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
	sr.startCertificateRenewalMonitoring(stopCh) //nolint:contextcheck // Intentionally not propagating `ctx` here as it's request-scoped.

	return sr, nil
}

func (s *signingRequestorImpl) Issue(ctx context.Context, name string, ips []string, onSigned OnSignedFn) error {
	if onSigned == nil {
		return errors.New("OnSignedFn cannot be nil")
	}

	if len(ips) == 0 {
		return errors.New("ips cannot be empty")
	}

	keyPEM, csrPEM, err := s.generateKeyAndCSR(ips)
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
			existingIPs, err := ExtractIPsFromCertificateRequestPEM(existing.Data[CSRDataKey])
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
		s.issuedCerts.LoadOrStore(newSecret.Name, certInfo{
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
	s.issuedCerts.Range(func(key, value any) bool {
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

func (s *signingRequestorImpl) generateKeyAndCSR(ips []string) ([]byte, []byte, error) {
	return CreatePEMEncodedKeyAndCertificateRequest("submariner-"+s.localClusterID, ips)
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
	go wait.Until(func() {
		s.checkCertificateRenewal(wait.ContextForChannel(stopCh))
	}, s.certCheckInterval, stopCh)

	logger.Infof("Started certificate renewal monitoring with interval %s", s.certCheckInterval)
}

// updateCertificateExpiration extracts and stores the certificate expiration time.
func (s *signingRequestorImpl) updateCertificateExpiration(secret *corev1.Secret) error {
	cert, err := ParseCertificateFromPEM(secret.Data[TLSDataKey])
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
func (s *signingRequestorImpl) checkCertificateRenewal(ctx context.Context) {
	s.issuedCerts.Range(func(key, value any) bool {
		secretName := key.(string)
		info := value.(certInfo)

		// Skip if expiration time is not set yet
		if info.expiresAt.IsZero() {
			return true
		}

		// Check if certificate needs renewal
		timeUntilExpiry := time.Until(info.expiresAt)
		if timeUntilExpiry <= s.certRenewBefore {
			logger.Infof("Certificate %q expires in %s, renewing", secretName, timeUntilExpiry.String())

			// Renew the certificate by clearing the signed annotation.
			err := util.MustUpdate(ctx, resource.ForDynamic(s.localSecretClient),
				resource.MustToUnstructured(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName}}),
				func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
					annotations := existing.GetAnnotations()
					delete(annotations, RequestSignedLabelKey)
					existing.SetAnnotations(annotations)

					return existing, nil
				})
			if err != nil {
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
