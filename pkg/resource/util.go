/*
© 2021 Red Hat, Inc.

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
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

func ToUnstructured(from runtime.Object) (*unstructured.Unstructured, error) {
	switch f := from.(type) {
	case *unstructured.Unstructured:
		return f.DeepCopy(), nil
	default:
		to := &unstructured.Unstructured{}
		err := scheme.Scheme.Convert(from, to, nil)
		if err != nil {
			return nil, errors.WithMessagef(err, "error converting %#v to unstructured.Unstructured", from)
		}

		return to, nil
	}
}

func ToMeta(obj runtime.Object) metav1.Object {
	objMeta, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	return objMeta
}
