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
	"k8s.io/client-go/dynamic"
)

type dynamicType struct {
	client dynamic.ResourceInterface
}

func (d *dynamicType) Get(ctx context.Context, name string, options metav1.GetOptions) (runtime.Object, error) {
	return d.client.Get(ctx, name, options)
}

func (d *dynamicType) Create(ctx context.Context, obj runtime.Object, options metav1.CreateOptions) (runtime.Object, error) {
	raw, err := ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return d.client.Create(ctx, raw, options)
}

func (d *dynamicType) Update(ctx context.Context, obj runtime.Object, options metav1.UpdateOptions) (runtime.Object, error) {
	raw, err := ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return d.client.Update(ctx, raw, options)
}

func (d *dynamicType) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	return d.client.Delete(ctx, name, options)
}

func ForDynamic(client dynamic.ResourceInterface) Interface {
	return &dynamicType{client: client}
}
