/*
© 2020 Red Hat, Inc.

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
package util

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	testV1 "github.com/submariner-io/admiral/test/apis/admiral.submariner.io/v1"
	"github.com/submariner-io/shipyard/test/e2e/framework"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

func ToasterGVR() *schema.GroupVersionResource {
	return &schema.GroupVersionResource{
		Group:    testV1.SchemeGroupVersion.Group,
		Version:  testV1.SchemeGroupVersion.Version,
		Resource: "toasters",
	}
}

func DeleteAllToasters(client dynamic.Interface, namespace, clusterName string) {
	DeleteAllOf(client, ToasterGVR(), namespace, clusterName)
}

func DeleteAllOf(client dynamic.Interface, gvr *schema.GroupVersionResource, namespace, clusterName string) {
	By(fmt.Sprintf("Deleting all %s in namespace %q from %q", gvr.Resource, namespace, clusterName))

	resource := client.Resource(*gvr).Namespace(namespace)
	Expect(resource.DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})).To(Succeed())

	framework.AwaitUntil(fmt.Sprintf("list %s in namespace %q from %q", gvr.Resource, namespace, clusterName), func() (i interface{},
		e error) {
		return resource.List(context.TODO(), metav1.ListOptions{})
	}, func(result interface{}) (bool, string, error) {
		list := result.(*unstructured.UnstructuredList)
		if len(list.Items) != 0 {
			return false, fmt.Sprintf("%d %s still remain", len(list.Items), gvr.Resource), nil
		}

		return true, "", nil
	})
}

func CreateToaster(client dynamic.Interface, toaster *testV1.Toaster, clusterName string) *testV1.Toaster {
	By(fmt.Sprintf("Creating Toaster %q in namespace %q in %q", toaster.Name, toaster.Namespace, clusterName))

	obj, err := client.Resource(*ToasterGVR()).Namespace(toaster.Namespace).Create(context.TODO(), test.ToUnstructured(toaster),
		metav1.CreateOptions{})
	Expect(err).To(Succeed())

	newToaster := &testV1.Toaster{}
	Expect(scheme.Scheme.Convert(obj, newToaster, nil)).To(Succeed())

	return newToaster
}

func DeleteToaster(client dynamic.Interface, toDelete runtime.Object, clusterName string) {
	meta, err := metaapi.Accessor(toDelete)
	Expect(err).To(Succeed())

	By(fmt.Sprintf("Deleting Toaster %q in namespace %q from %q", meta.GetName(), meta.GetNamespace(), clusterName))

	msg := fmt.Sprintf("delete Toaster %q in namespace %q from %q", meta.GetName(), meta.GetNamespace(), clusterName)
	framework.AwaitUntil(msg, func() (i interface{}, e error) {
		return nil, client.Resource(*ToasterGVR()).Namespace(meta.GetNamespace()).Delete(context.TODO(), meta.GetName(), metav1.DeleteOptions{})
	}, framework.NoopCheckResult)
}

func AddLabel(toaster *testV1.Toaster, key, value string) *testV1.Toaster {
	if toaster.Labels == nil {
		toaster.Labels = map[string]string{}
	}

	toaster.Labels[key] = value

	return toaster
}

func NewToaster(name, namespace string) *testV1.Toaster {
	return &testV1.Toaster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: testV1.ToasterSpec{
			Manufacturer: "Cuisinart",
			ModelNumber:  "1234T",
		},
	}
}
