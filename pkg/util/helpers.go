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
package util

import (
	"fmt"

	"github.com/pkg/errors"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
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

func ToUnstructuredResource(from runtime.Object, restMapper meta.RESTMapper) (*unstructured.Unstructured, *schema.GroupVersionResource,
	error) {
	to, err := resourceUtil.ToUnstructured(from)
	if err != nil {
		return nil, nil, err
	}

	gvr, err := FindGroupVersionResource(to, restMapper)
	if err != nil {
		return nil, nil, err
	}

	return to, gvr, nil
}

func FindGroupVersionResource(from *unstructured.Unstructured, restMapper meta.RESTMapper) (*schema.GroupVersionResource, error) {
	gvk := from.GroupVersionKind()
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, errors.WithMessagef(err, "error getting REST mapper for %#v", gvk)
	}

	return &mapping.Resource, nil
}

func GetMetadata(from *unstructured.Unstructured) map[string]interface{} {
	value, _, _ := unstructured.NestedFieldNoCopy(from.Object, MetadataField)
	if value != nil {
		return value.(map[string]interface{})
	}

	return map[string]interface{}{}
}

func GetSpec(obj *unstructured.Unstructured) interface{} {
	return GetNestedField(obj, "spec")
}

func GetNestedField(obj *unstructured.Unstructured, fields ...string) interface{} {
	nested, _, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if err != nil {
		klog.Errorf("Error retrieving %v field for %#v: %v", fields, obj, err)
	}

	return nested
}
