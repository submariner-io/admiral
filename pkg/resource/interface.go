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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Interface interface {
	Get(name string, options metav1.GetOptions) (runtime.Object, error)
	Create(obj runtime.Object) (runtime.Object, error)
	Update(obj runtime.Object) (runtime.Object, error)
	Delete(name string, options *metav1.DeleteOptions) error
}

type InterfaceFuncs struct {
	GetFunc    func(name string, options metav1.GetOptions) (runtime.Object, error)
	CreateFunc func(obj runtime.Object) (runtime.Object, error)
	UpdateFunc func(obj runtime.Object) (runtime.Object, error)
	DeleteFunc func(name string, options *metav1.DeleteOptions) error
}

func (i *InterfaceFuncs) Get(name string, options metav1.GetOptions) (runtime.Object, error) {
	return i.GetFunc(name, options)
}

func (i *InterfaceFuncs) Create(obj runtime.Object) (runtime.Object, error) {
	return i.CreateFunc(obj)
}

func (i *InterfaceFuncs) Update(obj runtime.Object) (runtime.Object, error) {
	return i.UpdateFunc(obj)
}

func (i *InterfaceFuncs) Delete(name string, options *metav1.DeleteOptions) error {
	return i.DeleteFunc(name, options)
}
