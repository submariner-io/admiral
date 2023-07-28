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
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

type Interface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error)
	Create(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error)
	Update(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	UpdateStatus(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
	List(ctx context.Context, options metav1.ListOptions) ([]runtime.Object, error)
}

type InterfaceFuncs struct {
	GetFunc          func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error)
	CreateFunc       func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error)
	UpdateFunc       func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	UpdateStatusFunc func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	DeleteFunc       func(ctx context.Context, name string, options metav1.DeleteOptions) error
	ListFunc         func(ctx context.Context, options metav1.ListOptions) ([]runtime.Object, error)
}

func (i *InterfaceFuncs) Get(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
	return i.GetFunc(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs) Create(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
	return i.CreateFunc(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs) Update(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
	return i.UpdateFunc(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs) UpdateStatus(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
	if i.UpdateStatusFunc == nil {
		// The function isn't defined so assume the status subresource isn't supported.
		return DefaultUpdateStatus(ctx, obj, options)
	}

	return i.UpdateStatusFunc(ctx, obj, options)
}

func (i *InterfaceFuncs) Delete(ctx context.Context, name string,
	options metav1.DeleteOptions, //nolint:gocritic // hugeParam - we're matching K8s API
) error {
	return i.DeleteFunc(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs) List(ctx context.Context, options metav1.ListOptions) ([]runtime.Object, error) {
	return i.ListFunc(ctx, options)
}

// DefaultUpdateStatus returns NotFound error indicating the status subresource isn't supported.
//
//nolint:gocritic // hugeParam - we're matching K8s API
func DefaultUpdateStatus(_ context.Context, _ runtime.Object, _ metav1.UpdateOptions) (runtime.Object, error) {
	return nil, apierrors.NewNotFound(schema.GroupResource{}, "")
}

func MustExtractList(from runtime.Object) []runtime.Object {
	if from == nil {
		return nil
	}

	objs, err := meta.ExtractList(from)
	utilruntime.Must(err)

	return objs
}
