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
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Interface interface {
	Get(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error)
	Create(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error)
	Update(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	Delete(ctx context.Context, name string, options metav1.DeleteOptions) error
}

type InterfaceFuncs struct {
	GetFunc    func(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error)
	CreateFunc func(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error)
	UpdateFunc func(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error)
	DeleteFunc func(ctx context.Context, name string, options metav1.DeleteOptions) error
}

func (i *InterfaceFuncs) Get(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
	return i.GetFunc(ctx, name, options)
}

func (i *InterfaceFuncs) Create(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
	return i.CreateFunc(ctx, obj, options)
}

func (i *InterfaceFuncs) Update(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
	return i.UpdateFunc(ctx, obj, options)
}

func (i *InterfaceFuncs) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	return i.DeleteFunc(ctx, name, options)
}
