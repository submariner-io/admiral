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
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type UpdateFn func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured

type updateFederator struct {
	*baseFederator
	update UpdateFn
}

func NewUpdateFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string, update UpdateFn) Federator {
	return &updateFederator{
		baseFederator: newBaseFederator(dynClient, restMapper, targetNamespace),
		update:        update,
	}
}

func NewUpdateStatusFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string) Federator {
	return NewUpdateFederator(dynClient, restMapper, targetNamespace,
		func(oldObj *unstructured.Unstructured, newObj *unstructured.Unstructured) *unstructured.Unstructured {
			util.SetNestedField(oldObj.Object, util.GetNestedField(newObj, util.StatusField), util.StatusField)
			return oldObj
		})
}

//nolint:wrapcheck // This function is effectively a wrapper so no need to wrap errors.
func (f *updateFederator) Distribute(obj runtime.Object) error {
	logger.V(log.LIBTRACE).Infof("In Distribute for %#v", obj)

	toUpdate, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	f.prepareResourceForSync(toUpdate)

	return util.Update(context.TODO(), resource.ForDynamic(resourceClient), toUpdate, func(obj runtime.Object) (runtime.Object, error) {
		return f.update(obj.(*unstructured.Unstructured), toUpdate), nil
	})
}
