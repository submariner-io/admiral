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
)

const (
	MetadataField    = "metadata"
	LabelsField      = "labels"
	AnnotationsField = "annotations"
	StatusField      = "status"
)

func BuildRestMapper(restConfig *rest.Config) (meta.RESTMapper, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, errors.Wrap(err, "error creating discovery client")
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, errors.Wrap(err, "error retrieving API group resources")
	}

	return restmapper.NewDiscoveryRESTMapper(groupResources), nil
}

func ToUnstructuredResource(from runtime.Object, restMapper meta.RESTMapper,
) (*unstructured.Unstructured, *schema.GroupVersionResource, error) {
	to, err := resourceUtil.ToUnstructured(from)
	if err != nil {
		return nil, nil, err //nolint:wrapcheck // ok to return as is
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
		return nil, errors.Wrapf(err, "error getting REST mapper for %#v", gvk)
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
		panic(fmt.Sprintf("Error retrieving %v field for %#v: %v", fields, obj, err))
	}

	return nested
}

func SetNestedField(to map[string]interface{}, value interface{}, fields ...string) {
	if value != nil {
		err := unstructured.SetNestedField(to, value, fields...)
		if err != nil {
			panic(fmt.Sprintf("Error setting value (%v) for nested field %v in object %v: %v", value, fields, to, err))
		}
	}
}

// CopyImmutableMetadata copies the static metadata fields (except Labels and Annotations) from one resource to another.
func CopyImmutableMetadata(from, to *unstructured.Unstructured) *unstructured.Unstructured {
	value, _, _ := unstructured.NestedFieldCopy(from.Object, MetadataField)
	if value == nil {
		return to
	}

	fromMetadata := value.(map[string]interface{})
	err := unstructured.SetNestedStringMap(fromMetadata, to.GetLabels(), LabelsField)
	if err != nil {
		panic(err)
	}

	err = unstructured.SetNestedStringMap(fromMetadata, to.GetAnnotations(), AnnotationsField)
	if err != nil {
		panic(err)
	}

	SetNestedField(to.Object, fromMetadata, MetadataField)

	return to
}
