/*
© 2020 Red Hat, Inc.

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
package broker

import (
	"context"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

type federator struct {
	dynClient          dynamic.Interface
	restMapper         meta.RESTMapper
	targetNamespace    string
	localClusterID     string
	keepMetadataFields map[string]bool
}

func NewFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, targetNamespace,
	localClusterID string, keepMetadataField ...string) federate.Federator {
	f := &federator{
		dynClient:          dynClient,
		restMapper:         restMapper,
		targetNamespace:    targetNamespace,
		localClusterID:     localClusterID,
		keepMetadataFields: map[string]bool{"name": true, "namespace": true, util.LabelsField: true, "annotations": true},
	}

	for _, field := range keepMetadataField {
		f.keepMetadataFields[field] = true
	}

	return f
}

func (f *federator) Distribute(obj runtime.Object) error {
	klog.V(log.LIBTRACE).Infof("In Distribute for %#v", obj)

	toDistribute, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	if f.localClusterID != "" {
		setNestedField(toDistribute.Object, f.localClusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	f.prepareResourceForSync(toDistribute)

	_, err = util.CreateOrUpdate(context.TODO(), resource.ForDynamic(resourceClient), toDistribute,
		func(obj runtime.Object) (runtime.Object, error) {
			// Preserve the existing metadata info (except Labels and Annotations), specifically the ResourceVersion which must
			// be set on an update operation.
			existing := obj.(*unstructured.Unstructured)
			existing.SetLabels(toDistribute.GetLabels())
			existing.SetAnnotations(toDistribute.GetAnnotations())
			setNestedField(toDistribute.Object, util.GetMetadata(existing), util.MetadataField)
			return toDistribute, nil
		})

	return err
}

func (f *federator) Delete(obj runtime.Object) error {
	toDelete, resourceClient, err := f.toUnstructured(obj)
	if err != nil {
		return err
	}

	klog.V(log.LIBTRACE).Infof("Deleting resource: %#v", toDelete)

	return resourceClient.Delete(context.TODO(), toDelete.GetName(), metav1.DeleteOptions{})
}

func (f *federator) toUnstructured(from runtime.Object) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
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

func (f *federator) prepareResourceForSync(obj *unstructured.Unstructured) {
	//  Remove metadata fields that are set by the API server on creation.
	metadata := util.GetMetadata(obj)
	for field := range metadata {
		if !f.keepMetadataFields[field] {
			unstructured.RemoveNestedField(obj.Object, util.MetadataField, field)
		}
	}
}

func setNestedField(to map[string]interface{}, value interface{}, fields ...string) {
	if value != nil {
		err := unstructured.SetNestedField(to, value, fields...)
		if err != nil {
			klog.Errorf("Error setting value (%v) for nested field %v in object %v: %v", value, fields, to, err)
		}
	}
}
