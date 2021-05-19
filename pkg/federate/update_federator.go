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
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

type updateFederator struct {
	*baseFederator
	subresources []string
}

func NewUpdateFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string, subresources ...string) Federator {
	return &updateFederator{
		baseFederator: newBaseFederator(dynClient, restMapper, targetNamespace),
		subresources:  subresources,
	}
}

func NewUpdateStatusFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string) Federator {
	return NewUpdateFederator(dynClient, restMapper, targetNamespace, "status")
}

func (f *updateFederator) Distribute(obj runtime.Object) error {
	klog.V(log.LIBTRACE).Infof("In Distribute for %#v", obj)

	toUpdate, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	f.prepareResourceForSync(toUpdate)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		klog.V(log.LIBTRACE).Infof("Updating resource: %#v", toUpdate)

		_, err = resourceClient.Update(context.TODO(), toUpdate, metav1.UpdateOptions{}, f.subresources...)
		if apierrors.IsNotFound(err) {
			return nil
		}

		if apierrors.IsConflict(err) {
			existing, err := resourceClient.Get(context.TODO(), toUpdate.GetName(), metav1.GetOptions{})
			if err != nil {
				return err
			}

			toUpdate = preserveMetadata(existing, toUpdate)
		}

		return err
	})
}
