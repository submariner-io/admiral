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
	"k8s.io/client-go/dynamic"
)

type dynamicType struct {
	client dynamic.ResourceInterface
}

func (d *dynamicType) Get(name string, options metav1.GetOptions) (runtime.Object, error) {
	return d.client.Get(name, options)
}

func (d *dynamicType) Create(obj runtime.Object) (runtime.Object, error) {
	raw, err := ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return d.client.Create(raw, metav1.CreateOptions{})
}

func (d *dynamicType) Update(obj runtime.Object) (runtime.Object, error) {
	raw, err := ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return d.client.Update(raw, metav1.UpdateOptions{})
}

func (d *dynamicType) Delete(name string, options *metav1.DeleteOptions) error {
	return d.client.Delete(name, options)
}

func ForDynamic(client dynamic.ResourceInterface) Interface {
	return &dynamicType{client: client}
}
