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

package resource

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func ToUnstructured(from runtime.Object) (*unstructured.Unstructured, error) {
	return ToUnstructuredUsingScheme(from, scheme.Scheme)
}

func ToUnstructuredUsingScheme(from runtime.Object, usingScheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	switch f := from.(type) {
	case *unstructured.Unstructured:
		return f.DeepCopy(), nil
	default:
		to := &unstructured.Unstructured{}
		err := usingScheme.Convert(from, to, nil)
		if err != nil {
			return nil, errors.Wrapf(err, "error converting %#v to unstructured.Unstructured", from)
		}

		return to, nil
	}
}

func MustToUnstructured(from runtime.Object) *unstructured.Unstructured {
	return MustToUnstructuredUsingScheme(from, scheme.Scheme)
}

func MustToUnstructuredUsingScheme(from runtime.Object, usingScheme *runtime.Scheme) *unstructured.Unstructured {
	u, err := ToUnstructuredUsingScheme(from, usingScheme)
	utilruntime.Must(err)

	return u
}

// MustToUnstructuredUsingDefaultConverter uses runtime.DefaultUnstructuredConverter which doesn't use a runtime.Scheme
// and thus the returned Unstructured will not have the type metadata field populated.
func MustToUnstructuredUsingDefaultConverter(from runtime.Object) *unstructured.Unstructured {
	var u *unstructured.Unstructured

	switch f := from.(type) {
	case *unstructured.Unstructured:
		u = f.DeepCopy()
	default:
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(from)
		if err != nil {
			panic(err)
		}

		u = &unstructured.Unstructured{Object: m}
	}

	// 'from' may have already contained the type metadata fields. To be consistent with this function's API contract,
	// remove the fields just in case since we can't guarantee the fields will always be populated.
	unstructured.RemoveNestedField(u.Object, "kind")
	unstructured.RemoveNestedField(u.Object, "apiVersion")

	return u
}

func MustFromUnstructured[T runtime.Object](u *unstructured.Unstructured, objType T) T {
	return MustFromUnstructuredUsingScheme(u, objType, scheme.Scheme)
}

func MustFromUnstructuredUsingScheme[T runtime.Object](u *unstructured.Unstructured, objType T, usingScheme *runtime.Scheme) T {
	actual := objType.DeepCopyObject().(T)
	err := usingScheme.Convert(u, actual, nil)
	utilruntime.Must(err)

	return actual
}

func MustToMeta(obj interface{}) metav1.Object {
	objMeta, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	return objMeta
}

func EnsureValidName(name string) string {
	// K8s only allows lower case alphanumeric characters, '-' or '.'. Regex used for validation is
	// '[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*'
	return strings.Map(func(c rune) rune {
		c = unicode.ToLower(c)
		if !unicode.IsDigit(c) && !unicode.IsLower(c) && c != '-' && c != '.' {
			return '-'
		}

		return c
	}, name)
}

func ToJSON(o any) string {
	out, _ := json.MarshalIndent(o, "", "  ")
	return string(out)
}

// TrimManagedFields is a cache.TransformFunc that removes the ManagedFields metadata field if present. Note that if 'obj' does not
// implement metav1.Object then it is ignored and no error is returned.
func TrimManagedFields(obj interface{}) (interface{}, error) {
	objMeta, err := meta.Accessor(obj)
	if err == nil && len(objMeta.GetManagedFields()) > 0 {
		objMeta.SetManagedFields(nil)
	}

	return obj, nil
}
