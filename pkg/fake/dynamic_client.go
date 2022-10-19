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

package fake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
)

type DynamicClient struct {
	*fake.FakeDynamicClient
	namespaceableResources map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface
}

type namespaceableResource struct {
	dynamic.NamespaceableResourceInterface
	mutex           sync.Mutex
	resourceClients map[string]dynamic.ResourceInterface
}

type DynamicResourceClient struct {
	dynamic.ResourceInterface
	created                      chan string
	updated                      chan string
	deleted                      chan string
	FailOnCreate                 error
	PersistentFailOnCreate       atomic.Value
	FailOnUpdate                 error
	PersistentFailOnUpdate       atomic.Value
	CheckResourceVersionOnUpdate bool
	FailOnDelete                 error
	PersistentFailOnDelete       atomic.Value
	FailOnGet                    error
	PersistentFailOnGet          atomic.Value
}

func NewDynamicClient(scheme *runtime.Scheme, objects ...runtime.Object) *DynamicClient {
	f := fake.NewSimpleDynamicClient(scheme, objects...)

	AddDeleteCollectionReactor(&f.Fake, schema.GroupVersionKind{Group: "fake-dynamic-client-group", Version: "v1", Kind: ""})
	AddFilteringListReactor(&f.Fake)

	return &DynamicClient{
		FakeDynamicClient:      f,
		namespaceableResources: map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface{},
	}
}

func (f *DynamicClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	f.Lock()
	defer f.Unlock()

	i := f.namespaceableResources[gvr]
	if i != nil {
		return i
	}

	f.namespaceableResources[gvr] = &namespaceableResource{
		NamespaceableResourceInterface: f.FakeDynamicClient.Resource(gvr),
		resourceClients:                map[string]dynamic.ResourceInterface{},
	}

	return f.namespaceableResources[gvr]
}

func (f *namespaceableResource) Namespace(namespace string) dynamic.ResourceInterface {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	i := f.resourceClients[namespace]
	if i != nil {
		return i
	}

	f.resourceClients[namespace] = &DynamicResourceClient{
		ResourceInterface: f.NamespaceableResourceInterface.Namespace(namespace),
		created:           make(chan string, 10000),
		updated:           make(chan string, 10000),
		deleted:           make(chan string, 10000),
	}

	return f.resourceClients[namespace]
}

func getError(from atomic.Value) error {
	o := from.Load()
	if o == nil {
		return nil
	}

	switch v := o.(type) {
	case string:
		if v != "" {
			return errors.New(v)
		}
	case error:
		return v
	}

	return nil
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (f *DynamicResourceClient) Create(ctx context.Context, obj *unstructured.Unstructured, options v1.CreateOptions,
	subresources ...string,
) (*unstructured.Unstructured, error) {
	f.created <- obj.GetName()

	fail := f.FailOnCreate
	if fail != nil {
		f.FailOnCreate = nil
		return nil, fail
	}

	fail = getError(f.PersistentFailOnCreate)
	if fail != nil {
		return nil, fail
	}

	obj.SetUID(uuid.NewUUID())
	obj.SetResourceVersion("1")

	return f.ResourceInterface.Create(ctx, obj, options, subresources...)
}

//nolint:gocritic // hugeParam - we're matching K8s API
func (f *DynamicResourceClient) Update(ctx context.Context, obj *unstructured.Unstructured, options v1.UpdateOptions,
	subresources ...string,
) (*unstructured.Unstructured, error) {
	f.updated <- obj.GetName()

	fail := f.FailOnUpdate
	if fail != nil {
		f.FailOnUpdate = nil
		return nil, fail
	}

	fail = getError(f.PersistentFailOnUpdate)
	if fail != nil {
		return nil, fail
	}

	if f.CheckResourceVersionOnUpdate {
		existing, _ := f.ResourceInterface.Get(ctx, obj.GetName(), v1.GetOptions{})
		if existing != nil && existing.GetResourceVersion() != obj.GetResourceVersion() {
			return nil, apierrors.NewConflict(schema.GroupResource{}, obj.GetName(),
				fmt.Errorf("resource version %q does not match expected %q", obj.GetResourceVersion(),
					existing.GetResourceVersion()))
		}

		if existing != nil && existing.GetResourceVersion() != "" {
			v, _ := strconv.Atoi(existing.GetResourceVersion())
			obj.SetResourceVersion(strconv.Itoa(v + 1))
		}
	}

	return f.ResourceInterface.Update(ctx, obj, options, subresources...)
}

func (f *DynamicResourceClient) Delete(ctx context.Context, name string,
	options v1.DeleteOptions, //nolint:gocritic // Match K8s API
	subresources ...string,
) error {
	f.deleted <- name

	fail := f.FailOnDelete
	if fail != nil {
		f.FailOnDelete = nil
		return fail
	}

	fail = getError(f.PersistentFailOnDelete)
	if fail != nil {
		return fail
	}

	return f.ResourceInterface.Delete(ctx, name, options, subresources...)
}

func (f *DynamicResourceClient) Get(ctx context.Context, name string, options v1.GetOptions,
	subresources ...string,
) (*unstructured.Unstructured, error) {
	fail := f.FailOnGet
	if fail != nil {
		f.FailOnGet = nil
		return nil, fail
	}

	fail = getError(f.PersistentFailOnGet)
	if fail != nil {
		return nil, fail
	}

	return f.ResourceInterface.Get(ctx, name, options, subresources...)
}

func (f *DynamicResourceClient) VerifyNoCreate(name string) {
	Consistently(f.created, 300*time.Millisecond).ShouldNot(Receive(Equal(name)), "Create was unexpectedly called")
}

func (f *DynamicResourceClient) VerifyNoUpdate(name string) {
	Consistently(f.updated, 300*time.Millisecond).ShouldNot(Receive(Equal(name)), "Update was unexpectedly called")
}

func (f *DynamicResourceClient) VerifyNoDelete(name string) {
	Consistently(f.deleted, 300*time.Millisecond).ShouldNot(Receive(Equal(name)), "Delete was unexpectedly called")
}
