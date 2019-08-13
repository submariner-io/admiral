package kubefed

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctlutil "sigs.k8s.io/kubefed/pkg/controller/util"
	kubefedopt "sigs.k8s.io/kubefed/pkg/kubefedctl/options"
)

const (
	federatedKindPrefix         = "Federated"
	errorSettingFederatedFields = "error setting fields necessary for federating: %v"
)

var systemMetadataFields = []string{"selfLink", "uid", "resourceVersion", "generation", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds"}

type UnstructuredConversionError struct {
	err      error
	resource runtime.Object
}

func (f *federator) Distribute(resource runtime.Object, clusterNames ...string) error {
	fedResource, err := createFederatedResource(f.scheme, resource)
	if err != nil {
		return err
	}

	if len(clusterNames) > 0 {
		unstructured.RemoveNestedField(fedResource.Object, ctlutil.SpecField, ctlutil.PlacementField)
		err = ctlutil.SetClusterNames(fedResource, clusterNames)
		if err != nil {
			return fmt.Errorf(errorSettingFederatedFields, err)
		}
	}

	// Update first, as it will probably be the most common operation
	err = f.kubeFedClient.Update(context.TODO(), fedResource)
	if err == nil {
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}

	return f.kubeFedClient.Create(context.TODO(), fedResource)
}

func (f *federator) Delete(resource runtime.Object) error {
	panic("not implemented")
}

func createFederatedResource(scheme *runtime.Scheme, resource runtime.Object) (*unstructured.Unstructured, error) {
	targetResource := &unstructured.Unstructured{}
	err := scheme.Convert(resource, targetResource, nil)
	if err != nil {
		return nil, &UnstructuredConversionError{err, resource}
	}

	fedResource := &unstructured.Unstructured{}
	setBasicMetaFields(fedResource, targetResource)

	// TODO(mpeterson): Figure out how to use the one from KubeFed
	// kubefedfed.RemoveUnwantedFields(targetResource)
	removeUnwantedFields(targetResource)

	// Adding the template field and including the targetResource's object as its content.
	err = unstructured.SetNestedField(fedResource.Object, targetResource.Object, ctlutil.SpecField, ctlutil.TemplateField)
	if err != nil {
		return nil, fmt.Errorf(errorSettingFederatedFields, err)
	}

	// Adding an empty selector for the Placement field's MatchLabel. An empty selector means
	// all clusters.
	err = unstructured.SetNestedStringMap(fedResource.Object, map[string]string{}, ctlutil.SpecField, ctlutil.PlacementField, ctlutil.ClusterSelectorField, ctlutil.MatchLabelsField)
	if err != nil {
		return nil, fmt.Errorf(errorSettingFederatedFields, err)
	}

	return fedResource, nil
}

func setBasicMetaFields(resource, base *unstructured.Unstructured) {
	fedKind := federatedKindPrefix + base.GetKind()
	resource.SetKind(fedKind)

	// TODO(mpeterson): Modify to get the type config and generalize this usage
	gv := schema.GroupVersion{
		Group:   kubefedopt.DefaultFederatedGroup,
		Version: kubefedopt.DefaultFederatedVersion,
	}

	resource.SetAPIVersion(gv.String())
	resource.SetName(base.GetName())
	resource.SetNamespace(base.GetNamespace())
}

// This function removes metadata information that is being added by a running Kubernetes API
// and that effectively exists only on objects retrieved from the API. This metadata is thus
// only of interest for the objects living in the API but not for a new object that we will
// create such in our use case.
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

func (err *UnstructuredConversionError) Error() string {
	return fmt.Sprintf(
		"error converting to unstructured.Unstructured: %v\n(RES=%#v)",
		err.err,
		err.resource,
	)
}
