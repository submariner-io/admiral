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

package federate

import (
	"context"

	"github.com/submariner-io/admiral/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type createFederator struct {
	*baseFederator
}

func NewCreateFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string) Federator {
	return &createFederator{
		baseFederator: newBaseFederator(dynClient, restMapper, targetNamespace),
	}
}

//nolint:wrapcheck // This function is effectively a wrapper so no need to wrap errors.
func (f *createFederator) Distribute(obj runtime.Object) error {
	logger.V(log.LIBTRACE).Infof("In Distribute for %#v", obj)

	toDistribute, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	f.prepareResourceForSync(toDistribute)

	_, err = resourceClient.Create(context.TODO(), toDistribute, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}

	return err
}
