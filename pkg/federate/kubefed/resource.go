package kubefed

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var _ error = &UnstructuredConversionError{}

func (f *Federator) Distribute(resource runtime.Object) error {
	fedResource, err := createFederatedResource(f.scheme, resource)
	if err != nil {
		return err
	}

	// Update first, as it will probably be the most common operation
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(fedResource.GroupVersionKind())

		getErr := f.kubeFedClient.Get(context.TODO(), client.ObjectKey{Namespace: fedResource.GetNamespace(), Name: fedResource.GetName()}, existing)
		if getErr != nil {
			return getErr
		}

		// Replace the "spec" field in the existing object with the updated field from "fedResource". We preserve the
		// existing metadata info, specifically the ResourceVersion which must be set on an update operation.
		// Note - we set a valid "spec" field earlier in "fedResource" so no need to handle the other return values.
		newSpec, _, _ := unstructured.NestedFieldNoCopy(fedResource.Object, SpecField)

		oldSpec, _, _ := unstructured.NestedFieldNoCopy(existing.Object, SpecField)
		if reflect.DeepEqual(oldSpec, newSpec) {
			// No need to issue the update if the spec didn't change
			return nil
		}

		if setErr := unstructured.SetNestedField(existing.Object, newSpec, SpecField); setErr != nil {
			return fmt.Errorf(errorSettingFederatedFields, setErr)
		}

		klog.V(2).Infof("Updating federated resource: %#v", existing)

		return f.kubeFedClient.Update(context.TODO(), existing)
	})

	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("error updating federated resource %#v: %v", fedResource, err)
	}

	klog.V(2).Infof("Creating federated resource: %#v", fedResource)

	if err = f.kubeFedClient.Create(context.TODO(), fedResource); err != nil {
		return fmt.Errorf("error creating federated resoource %#v: %v", fedResource, err)
	}

	return nil
}

func (f *Federator) Delete(resource runtime.Object) error {
	fedResource, err := createFederatedResource(f.scheme, resource)
	if err != nil {
		return err
	}

	return f.kubeFedClient.Delete(context.TODO(), fedResource)
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
	err = unstructured.SetNestedField(fedResource.Object, targetResource.Object, SpecField, TemplateField)
	if err != nil {
		return nil, fmt.Errorf(errorSettingFederatedFields, err)
	}
	// Adding an empty selector for the Placement field's MatchLabel. An empty selector means
	// all clusters.
	err = unstructured.SetNestedStringMap(fedResource.Object, map[string]string{}, SpecField, PlacementField, ClusterSelectorField, MatchLabelsField)
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
		Group:   DefaultFederatedGroup,
		Version: DefaultFederatedVersion,
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
