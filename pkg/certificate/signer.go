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
	"sync"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type SignerConfig struct {
	RestConfig *rest.Config
	DynClient  dynamic.Interface
	RestMapper meta.RESTMapper
}

type Signer interface {
	Start(namespace string) error
	Stop(namespace string)
}

type signerImpl struct {
	restMapper meta.RESTMapper
	dynClient  dynamic.Interface
	syncerMap  sync.Map
}

func NewSigner(config SignerConfig) (Signer, error) {
	s := &signerImpl{}

	var err error

	if config.RestMapper == nil {
		if s.restMapper, err = util.BuildRestMapper(config.RestConfig); err != nil {
			return nil, errors.Wrap(err, "error building the REST mapper")
		}
	}

	if config.DynClient == nil {
		if s.dynClient, err = resource.NewDynamicClient(config.RestConfig); err != nil {
			return nil, errors.Wrap(err, "error creating dynamic client")
		}
	}

	return s, nil
}

func (s *signerImpl) Start(namespace string) error {
	if _, exists := s.syncerMap.Load(namespace); exists {
		return nil
	}

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

			err := s.signSecret(secret)
			if err != nil {
				logger.Errorf(err, "error signing Secret %q", secret.Name)
			}

			return secret, err != nil
		},
		Federator: federate.NewUpdateFederator(s.dynClient, s.restMapper, namespace,
			func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured {
				existingSecret := resource.MustFromUnstructured(oldObj, &corev1.Secret{})
				updatedSecret := resource.MustFromUnstructured(newObj, &corev1.Secret{})

				if existingSecret.Annotations == nil {
					existingSecret.Annotations = map[string]string{}
				}

				existingSecret.Annotations[RequestSignedLabelKey] = "true"
				existingSecret.Data[TLSDataKey] = updatedSecret.Data[TLSDataKey]
				existingSecret.Data[CADataKey] = updatedSecret.Data[CADataKey]

				return resource.MustToUnstructured(existingSecret)
			}),
	})
	if err != nil {
		return errors.Wrap(err, "error creating resource syncer")
	}

	stopCh := make(chan struct{})

	if err := secretSyncer.Start(stopCh); err != nil {
		return errors.Wrap(err, "error starting resource syncer")
	}

	s.syncerMap.Store(namespace, stopCh)

	return nil
}

func (s *signerImpl) Stop(namespace string) {
	if v, exists := s.syncerMap.LoadAndDelete(namespace); exists {
		close(v.(chan struct{}))
	}
}

// TODO - implement
//
//nolint:unparam // (error) is always nil - remove once implemented
func (s *signerImpl) signSecret(secret *corev1.Secret) error {
	secret.Data[TLSDataKey] = []byte("tls-" + string(secret.Data[CSRDataKey]))
	secret.Data[CADataKey] = []byte("ca-" + string(secret.Data[CSRDataKey]))

	return nil
}
