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

//nolint:wrapcheck // These functions are pass-through wrappers for the k8s APIs.
package resource

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type dynamicType struct {
	client dynamic.ResourceInterface
}

func (d *dynamicType) Get(ctx context.Context, name string, options metav1.GetOptions) (*unstructured.Unstructured, error) {
	return d.client.Get(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (d *dynamicType) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	return d.client.Create(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (d *dynamicType) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	return d.client.Update(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (d *dynamicType) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	return d.client.UpdateStatus(ctx, obj, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (d *dynamicType) Delete(ctx context.Context, name string, options metav1.DeleteOptions) error {
	return d.client.Delete(ctx, name, options)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (d *dynamicType) List(ctx context.Context, options metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return d.client.List(ctx, options)
}

func ForDynamic(client dynamic.ResourceInterface) Interface[*unstructured.Unstructured] {
	return kubernetesStatusInterfaceAdapter[*unstructured.Unstructured, *unstructured.UnstructuredList](&dynamicType{client: client})
}
