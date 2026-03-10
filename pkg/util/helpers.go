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
	"context"
	"crypto/x509"
	"sync/atomic"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	MetadataField    = "metadata"
	LabelsField      = "labels"
	AnnotationsField = "annotations"
	StatusField      = "status"
)

var (
	lastBadCertificate atomic.Value
	// helperLogger is used for error hooks which need to be package-level variables.
	helperLogger = log.Logger{Logger: logf.Log}
)

var BuildRestMapper = func(restConfig *rest.Config) (meta.RESTMapper, error) {
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
	to, err := resource.ToUnstructured(from)
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

func GetMetadata(from *unstructured.Unstructured) map[string]any {
	value, _, _ := unstructured.NestedFieldNoCopy(from.Object, MetadataField)
	if value != nil {
		return value.(map[string]any)
	}

	return map[string]any{}
}

func GetSpec(obj *unstructured.Unstructured) any {
	return GetNestedField(obj, "spec")
}

func GetNestedField(obj *unstructured.Unstructured, fields ...string) any {
	nested, _, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	utilruntime.Must(errors.Wrapf(err, "error retrieving %v field for %#v", fields, obj))

	return nested
}

func SetNestedField(to map[string]any, value any, fields ...string) {
	if value != nil {
		err := unstructured.SetNestedField(to, value, fields...)
		utilruntime.Must(errors.Wrapf(err, "error setting value (%v) for nested field %v in object %v", value, fields, to))
	}
}

// CopyImmutableMetadata copies the static metadata fields (except Labels and Annotations) from one resource to another.
func CopyImmutableMetadata(from, to *unstructured.Unstructured) *unstructured.Unstructured {
	value, _, _ := unstructured.NestedFieldCopy(from.Object, MetadataField)
	if value == nil {
		return to
	}

	fromMetadata := value.(map[string]any)
	err := unstructured.SetNestedStringMap(fromMetadata, to.GetLabels(), LabelsField)
	utilruntime.Must(err)

	err = unstructured.SetNestedStringMap(fromMetadata, to.GetAnnotations(), AnnotationsField)
	utilruntime.Must(err)

	SetNestedField(to.Object, fromMetadata, MetadataField)

	return to
}

func DeeplyEmpty(m map[string]any) bool {
	for _, v := range m {
		switch t := v.(type) {
		case map[string]any:
			if !DeeplyEmpty(t) {
				return false
			}
		default:
			return false
		}
	}

	return true
}

var (
	ErrorHook = helperLogger.Errorf
	FatalHook = helperLogger.FatalfOnError
)

func AddCertificateErrorHandler(fatal bool) {
	logCertificateError := ErrorHook
	if fatal {
		logCertificateError = FatalHook
	}

	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers,
		func(_ context.Context, err error, _ string, _ ...any) {
			// The generic handler has already logged the error, no need to repeat if we don't want extra detail
			var unknownAuthorityError x509.UnknownAuthorityError
			if errors.As(err, &unknownAuthorityError) && lastBadCertificate.Swap(unknownAuthorityError.Cert) != unknownAuthorityError.Cert {
				logCertificateError(err, "Certificate error: %s", resource.ToJSON(err))
			}

			var certificateInvalidError x509.CertificateInvalidError
			if errors.As(err, &certificateInvalidError) && lastBadCertificate.Swap(certificateInvalidError.Cert) != certificateInvalidError.Cert {
				logCertificateError(err, "Certificate error: %s", resource.ToJSON(err))
			}
		})
}
