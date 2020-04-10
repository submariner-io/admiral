package util

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog"
)

const (
	MetadataField = "metadata"
	LabelsField   = "labels"
)

func BuildRestMapper(restConfig *rest.Config) (meta.RESTMapper, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating discovery client: %v", err)
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, fmt.Errorf("error retrieving API group resources: %v", err)
	}

	return restmapper.NewDiscoveryRESTMapper(groupResources), nil
}

func ToUnstructuredResource(from runtime.Object, restMapper meta.RESTMapper) (*unstructured.Unstructured, *schema.GroupVersionResource, error) {
	to, err := ToUnstructured(from)
	if err != nil {
		return nil, nil, err
	}

	gvr, err := FindGroupVersionResource(to, restMapper)
	if err != nil {
		return nil, nil, err
	}

	return to, gvr, nil
}

func ToUnstructured(from runtime.Object) (*unstructured.Unstructured, error) {
	to := &unstructured.Unstructured{}
	err := scheme.Scheme.Convert(from, to, nil)
	if err != nil {
		return nil, errors.WithMessagef(err, "error converting %#v to unstructured.Unstructured", from)
	}

	return to, nil
}

func FindGroupVersionResource(from *unstructured.Unstructured, restMapper meta.RESTMapper) (*schema.GroupVersionResource, error) {
	gvk := from.GroupVersionKind()
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, errors.WithMessagef(err, "error getting REST mapper for %#v", gvk)
	}

	klog.V(log.DEBUG).Infof("Found %#v", mapping.Resource)

	return &mapping.Resource, nil
}

func GetMetadata(from *unstructured.Unstructured) map[string]interface{} {
	value, _, _ := unstructured.NestedFieldNoCopy(from.Object, MetadataField)
	if value != nil {
		return value.(map[string]interface{})
	}

	return map[string]interface{}{}
}
