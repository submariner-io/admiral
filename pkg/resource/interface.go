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

type Interface[T runtime.Object] interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (T, error)
	Create(ctx context.Context, obj T, options metav1.CreateOptions) (T, error)
	Update(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
	UpdateStatus(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
	List(ctx context.Context, options metav1.ListOptions) ([]T, error)
}

type InterfaceFuncs[T runtime.Object] struct {
	GetFunc          func(ctx context.Context, name string, options metav1.GetOptions) (T, error)
	CreateFunc       func(ctx context.Context, obj T, options metav1.CreateOptions) (T, error)
	UpdateFunc       func(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
	UpdateStatusFunc func(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error)
	DeleteFunc       func(ctx context.Context, name string, options metav1.DeleteOptions) error
	ListFunc         func(ctx context.Context, options metav1.ListOptions) ([]T, error)
}

func (i *InterfaceFuncs[T]) Get(ctx context.Context, name string, options metav1.GetOptions) (T, error) {
	return i.GetFunc(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs[T]) Create(ctx context.Context, obj T, options metav1.CreateOptions) (T, error) {
	return i.CreateFunc(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs[T]) Update(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error) {
	return i.UpdateFunc(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs[T]) UpdateStatus(ctx context.Context, obj T, options metav1.UpdateOptions) (T, error) {
	if i.UpdateStatusFunc == nil {
		// The function isn't defined so assume the status subresource isn't supported.
		return DefaultUpdateStatus(ctx, obj, options)
	}

	return i.UpdateStatusFunc(ctx, obj, options)
}

func (i *InterfaceFuncs[T]) Delete(ctx context.Context, name string,
	options metav1.DeleteOptions, //nolint:gocritic // hugeParam - we're matching K8s API
) error {
	return i.DeleteFunc(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (i *InterfaceFuncs[T]) List(ctx context.Context, options metav1.ListOptions) ([]T, error) {
	return i.ListFunc(ctx, options)
}

// DefaultUpdateStatus returns NotFound error indicating the status subresource isn't supported.
//
//nolint:gocritic // hugeParam - we're matching K8s API
func DefaultUpdateStatus[T runtime.Object](_ context.Context, _ T, _ metav1.UpdateOptions) (T, error) {
	return *new(T), apierrors.NewNotFound(schema.GroupResource{}, "")
}

func MustExtractList[T runtime.Object](from runtime.Object) []T {
	if from == nil {
		return nil
	}

	objs, err := meta.ExtractList(from)
	utilruntime.Must(err)

	list := []T{}
	for i := range objs {
		list = append(list, objs[i].(T))
	}

	return list
}
