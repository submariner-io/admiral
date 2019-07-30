package kubefed

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
	ctlutil "sigs.k8s.io/kubefed/pkg/controller/util"
	kubefedopt "sigs.k8s.io/kubefed/pkg/kubefedctl/options"
)

const (
	federatedKindPrefix         = "Federated"
	errorUnstructuredConversion = "Error converting to unstructured.Unstructured: %v"
	errorSettingFederatedFields = "Error setting fields necessary for federating: %v"
	errorUpdateResource         = "Error updating the resource, assuming it does not exist and creating new one: %v"
	errorCreateResource         = "Error creating the resource: %v"
)

var systemMetadataFields = []string{"selfLink", "uid", "resourceVersion", "generation", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds"}

func (f *federator) Distribute(resource runtime.Object, clusterNames ...string) error {
	fedResource, err := f.createFederatedResource(resource)
	if err != nil {
		return err
	}

	if len(clusterNames) > 0 {
		unstructured.RemoveNestedField(fedResource.Object, ctlutil.SpecField, ctlutil.PlacementField)
		err = ctlutil.SetClusterNames(fedResource, clusterNames)
		if err != nil {
			// Path not covered by unit tests because it's trivial behavior yet complex to test
			return fmt.Errorf(errorSettingFederatedFields, err)
		}
	}

	// Update first, as it will probably be the most common operation
	err = f.kubeFedClient.Update(context.TODO(), fedResource)
	if err == nil {
		return nil
	}

	klog.Infof(errorUpdateResource, err)

	err = f.kubeFedClient.Create(context.TODO(), fedResource)
	if err != nil {
		// Path not covered by unit tests because it's trivial behavior yet complex to test
		return fmt.Errorf(errorCreateResource, err)
	}

	return nil
}

func (f *federator) Delete(resource runtime.Object) error {
	panic("not implemented")
}

func (f *federator) createFederatedResource(resource runtime.Object) (*unstructured.Unstructured, error) {
	targetResource := &unstructured.Unstructured{}
	err := f.scheme.Convert(resource, targetResource, nil)
	if err != nil {
		return nil, fmt.Errorf(errorUnstructuredConversion, err)
	}

	fedResource := &unstructured.Unstructured{}
	setBasicMetaFields(fedResource, targetResource)

	// TODO(mpeterson): Figure out how to use the one from KubeFed
	// kubefedfed.RemoveUnwantedFields(targetResource)
	removeUnwantedFields(targetResource)

	err = unstructured.SetNestedField(fedResource.Object, targetResource.Object, ctlutil.SpecField, ctlutil.TemplateField)
	if err != nil {
		// Path not covered by unit tests because it's trivial behavior yet complex to test
		return nil, fmt.Errorf(errorSettingFederatedFields, err)
	}
	err = unstructured.SetNestedStringMap(fedResource.Object, map[string]string{}, ctlutil.SpecField, ctlutil.PlacementField, ctlutil.ClusterSelectorField, ctlutil.MatchLabelsField)
	if err != nil {
		// Path not covered by unit tests because it's trivial behavior yet complex to test
		return nil, fmt.Errorf(errorSettingFederatedFields, err)
	}

	return fedResource, nil
}

func setBasicMetaFields(resource, base *unstructured.Unstructured) {
	fedKind := federatedKindPrefix + base.GetKind()
	resource.SetKind(fedKind)

	gv := schema.GroupVersion{
		Group:   kubefedopt.DefaultFederatedGroup,
		Version: kubefedopt.DefaultFederatedVersion,
	}
	resource.SetAPIVersion(gv.String())

	resource.SetName(base.GetName())
	resource.SetNamespace(base.GetNamespace())
}

func removeUnwantedFields(resource *unstructured.Unstructured) {
	for _, field := range systemMetadataFields {
		unstructured.RemoveNestedField(resource.Object, "metadata", field)
		// For resources with pod template subresource (jobs, deployments, replicasets)
		unstructured.RemoveNestedField(resource.Object, "spec", "template", "metadata", field)
	}
	unstructured.RemoveNestedField(resource.Object, "metadata", "name")
	unstructured.RemoveNestedField(resource.Object, "metadata", "namespace")
	unstructured.RemoveNestedField(resource.Object, "apiVersion")
	unstructured.RemoveNestedField(resource.Object, "kind")
	unstructured.RemoveNestedField(resource.Object, "status")
}
