package fake

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

type dynamicClient struct {
	dynamic.Interface
	namespaceableResources map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface
}

type namespaceableResource struct {
	dynamic.NamespaceableResourceInterface
	resourceClients map[string]dynamic.ResourceInterface
}

type DynamicResourceClient struct {
	dynamic.ResourceInterface
	FailOnCreate error
	FailOnUpdate error
	FailOnDelete error
	FailOnGet    error
}

func NewDynamicClient(objects ...runtime.Object) dynamic.Interface {
	f := fake.NewSimpleDynamicClient(scheme.Scheme, objects...)
	return &dynamicClient{
		Interface:              f,
		namespaceableResources: map[schema.GroupVersionResource]dynamic.NamespaceableResourceInterface{},
	}
}

func (f *dynamicClient) Resource(gvr schema.GroupVersionResource) dynamic.NamespaceableResourceInterface {
	i := f.namespaceableResources[gvr]
	if i != nil {
		return i
	}

	f.namespaceableResources[gvr] = &namespaceableResource{
		NamespaceableResourceInterface: f.Interface.Resource(gvr),
		resourceClients:                map[string]dynamic.ResourceInterface{},
	}

	return f.namespaceableResources[gvr]
}

func (f *namespaceableResource) Namespace(namespace string) dynamic.ResourceInterface {
	i := f.resourceClients[namespace]
	if i != nil {
		return i
	}

	f.resourceClients[namespace] = &DynamicResourceClient{ResourceInterface: f.NamespaceableResourceInterface.Namespace(namespace)}
	return f.resourceClients[namespace]
}

func (f *DynamicResourceClient) Create(obj *unstructured.Unstructured, options v1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	fail := f.FailOnCreate
	if fail != nil {
		f.FailOnCreate = nil
		return nil, fail
	}

	obj.SetUID(uuid.NewUUID())
	obj.SetResourceVersion("1")
	return f.ResourceInterface.Create(obj, options, subresources...)
}

func (f *DynamicResourceClient) Update(obj *unstructured.Unstructured, options v1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	fail := f.FailOnUpdate
	if fail != nil {
		f.FailOnUpdate = nil
		return nil, fail
	}

	return f.ResourceInterface.Update(obj, options, subresources...)
}

func (f *DynamicResourceClient) Delete(name string, options *v1.DeleteOptions, subresources ...string) error {
	fail := f.FailOnDelete
	if fail != nil {
		f.FailOnDelete = nil
		return fail
	}

	return f.ResourceInterface.Delete(name, options, subresources...)
}

func (f *DynamicResourceClient) Get(name string, options v1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	fail := f.FailOnGet
	if fail != nil {
		f.FailOnGet = nil
		return nil, fail
	}

	return f.ResourceInterface.Get(name, options, subresources...)
}
