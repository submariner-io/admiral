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

//nolint:wrapcheck // These functions are effectively wrappers so no need to wrap errors.
package federate

import (
	"context"

	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type baseFederator struct {
	dynClient          dynamic.Interface
	restMapper         meta.RESTMapper
	targetNamespace    string
	keepMetadataFields map[string]bool
}

var logger = log.Logger{Logger: logf.Log.WithName("Federator")}

func newBaseFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace string,
	keepMetadataField ...string,
) *baseFederator {
	b := &baseFederator{
		dynClient:          dynClient,
		restMapper:         restMapper,
		targetNamespace:    targetNamespace,
		keepMetadataFields: map[string]bool{"name": true, "namespace": true, util.LabelsField: true, util.AnnotationsField: true},
	}

	for _, field := range keepMetadataField {
		b.keepMetadataFields[field] = true
	}

	return b
}

func (f *baseFederator) Delete(obj runtime.Object) error {
	toDelete, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	logger.V(log.LIBTRACE).Infof("Deleting resource: %#v", toDelete)

	return resourceClient.Delete(context.TODO(), toDelete.GetName(), metav1.DeleteOptions{})
}

func (f *baseFederator) toUnstructured(from runtime.Object) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	to, gvr, err := util.ToUnstructuredResource(from, f.restMapper)
	if err != nil {
		return nil, nil, err
	}

	ns := f.targetNamespace
	if ns == corev1.NamespaceAll {
		ns = to.GetNamespace()
	}

	to.SetNamespace(ns)

	return to, f.dynClient.Resource(*gvr).Namespace(ns), nil
}

func (f *baseFederator) prepareResourceForSync(obj *unstructured.Unstructured) {
	//  Remove metadata fields that are set by the API server on creation.
	metadata := util.GetMetadata(obj)
	for field := range metadata {
		if !f.keepMetadataFields[field] {
			unstructured.RemoveNestedField(obj.Object, util.MetadataField, field)
		}
	}
}
