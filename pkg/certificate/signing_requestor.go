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
	goerrors "errors"
	"fmt"
	"sync"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
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
	"k8s.io/client-go/dynamic"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	SigningRequestLabelKey = "submariner.io/csr-request"
	RequestSignedLabelKey  = "submariner.io/csr-request-signed"
	PrivateKeyDataKey      = "tls.key"
	CSRDataKey             = "csr.pem"
	TLSDataKey             = "tls.crt"
	CADataKey              = "ca.crt"
)

type OnSignedFn func(secretData map[string][]byte) error

type KeyGeneratorFn func(ips []string) ([]byte, []byte, error)

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
	onSignedMap        sync.Map
	keyGenerator       KeyGeneratorFn
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
			if !bytes.Equal(existing.Data[CSRDataKey], newSecret.Data[CSRDataKey]) {
				// The CSR data changed so clear the signed annotation, so it will be re-signed.
				delete(existing.Annotations, RequestSignedLabelKey)
			}

			existing.Data[PrivateKeyDataKey] = newSecret.Data[PrivateKeyDataKey]
			existing.Data[CSRDataKey] = newSecret.Data[CSRDataKey]

			return resource.MustToUnstructured(existing), nil
		},
	})
	if err == nil {
		s.onSignedMap.Store(newSecret.Name, onSigned)
	}

	if result == util.OperationResultCreated {
		logger.Infof("Successfully created CSR Secret %q", newSecret.Name)
	} else if result == util.OperationResultUpdated {
		logger.Infof("Successfully updated CSR Secret %q", newSecret.Name)
	}

	return errors.Wrapf(err, "error creating or updating CSR Secret %q", newSecret.Name)
}

func (s *signingRequestorImpl) Remove(ctx context.Context, name string) error {
	s.onSignedMap.Delete(s.secretName(name))

	return goerrors.Join(deleteIfPresent(ctx, s.localSecretClient, s.secretName(name)),
		deleteIfPresent(ctx, s.brokerSecretClient, s.secretName(name)))
}

func (s *signingRequestorImpl) Uninstall(ctx context.Context) error {
	s.onSignedMap.Range(func(key, value interface{}) bool {
		s.onSignedMap.Delete(key)
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
	// TODO - implement
	return []byte{1, 2, 3}, []byte(fmt.Sprintf("%v", ips)), nil
}

func (s *signingRequestorImpl) onLocalSecretSigned(obj runtime.Object, numRequeues int) bool {
	// warningLogInterval defines how often to log warnings for missing callbacks.
	const warningLogInterval = 12

	secret := obj.(*corev1.Secret)

	v, ok := s.onSignedMap.Load(secret.Name)
	if !ok {
		if numRequeues > 0 && numRequeues%warningLogInterval == 0 {
			logger.Warningf("Received signed Secret for %q with no OnSigned callback registered", secret.Name)
		}

		return true
	}

	err := v.(OnSignedFn)(secret.Data)
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
