package broker

import (
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

type federator struct {
	dynClient       dynamic.Interface
	restMapper      meta.RESTMapper
	brokerNamespace string
	localClusterID  string
}

var keepMetadataFields = map[string]bool{"name": true, "namespace": true, util.LabelsField: true, "annotations": true}

func NewFederator(dynClient dynamic.Interface, restMapper meta.RESTMapper, brokerNamespace string, localClusterID string) federate.Federator {
	return &federator{
		dynClient:       dynClient,
		restMapper:      restMapper,
		brokerNamespace: brokerNamespace,
		localClusterID:  localClusterID,
	}
}

func (f *federator) Distribute(resource runtime.Object) error {
	klog.V(log.DEBUG).Infof("In Distribute for %#v", resource)

	toDistribute, resourceClient, err := f.toUnstructured(resource)
	if err != nil {
		return err
	}

	if f.localClusterID != "" {
		setNestedField(toDistribute.Object, f.localClusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	f.prepareResourceForSync(toDistribute)
	toDistribute.SetNamespace(f.brokerNamespace)

	_, err = util.CreateOrUpdate(resourceClient, toDistribute, func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
		// Preserve the existing metadata info (except Labels and Annotations), specifically the ResourceVersion which must
		// be set on an update operation.
		existing.SetLabels(toDistribute.GetLabels())
		existing.SetAnnotations(toDistribute.GetAnnotations())
		setNestedField(toDistribute.Object, util.GetMetadata(existing), util.MetadataField)
		return toDistribute, nil
	})

	return err
}

func (f *federator) Delete(resource runtime.Object) error {
	toDelete, resourceClient, err := f.toUnstructured(resource)
	if err != nil {
		return err
	}

	klog.V(log.DEBUG).Infof("Deleting resource: %#v", toDelete)

	return resourceClient.Delete(toDelete.GetName(), &metav1.DeleteOptions{})
}

func (f *federator) toUnstructured(from runtime.Object) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	to, gvr, err := util.ToUnstructuredResource(from, f.restMapper)
	if err != nil {
		return nil, nil, err
	}

	return to, f.dynClient.Resource(*gvr).Namespace(f.brokerNamespace), nil
}

func (f *federator) prepareResourceForSync(resource *unstructured.Unstructured) {
	//  Remove metadata fields that are set by the API server on creation.
	metadata := util.GetMetadata(resource)
	for field := range metadata {
		if !keepMetadataFields[field] {
			unstructured.RemoveNestedField(resource.Object, util.MetadataField, field)
		}
	}

	unstructured.RemoveNestedField(resource.Object, "status")
	resource.SetNamespace(f.brokerNamespace)
}

func setNestedField(to map[string]interface{}, value interface{}, fields ...string) {
	if value != nil {
		err := unstructured.SetNestedField(to, value, fields...)
		if err != nil {
			klog.Errorf("Error setting value (%v) for nested field %v in object %v: %v", value, fields, to, err)
		}
	}
}
