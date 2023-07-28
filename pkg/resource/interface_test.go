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

package resource_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("Interface", func() {
	Context("ForDaemonSet", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForDaemonSet(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: appsv1.DaemonSetSpec{
				MinReadySeconds: 3,
			},
		})
	})

	Context("ForDeployment", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForDeployment(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: appsv1.DeploymentSpec{
				MinReadySeconds: 3,
			},
		})
	})

	Context("ForNamespace", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForNamespace(k8sfake.NewSimpleClientset())
		}, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		})
	})

	Context("ForPod", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForPod(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: corev1.PodSpec{
				Hostname: "my-host",
			},
		})
	})

	Context("ForService", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForService(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		})
	})

	Context("ForServiceAccount", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForServiceAccount(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
		})
	})

	Context("ForClusterRole", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForClusterRole(k8sfake.NewSimpleClientset())
		}, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		})
	})

	Context("ForClusterRoleBinding", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForClusterRoleBinding(k8sfake.NewSimpleClientset())
		}, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		})
	})

	Context("ForRole", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForRole(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
		})
	})

	Context("ForRoleBinding", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForRoleBinding(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
		})
	})

	Context("ForConfigMap", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForConfigMap(k8sfake.NewSimpleClientset(), test.LocalNamespace)
		}, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Data: map[string]string{"one": "two"},
		})
	})

	Context("ForListableControllerClient", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForListableControllerClient(clientfake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
				test.LocalNamespace, &corev1.Pod{}, &corev1.PodList{})
		}, &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: corev1.PodSpec{
				Hostname: "my-host",
			},
		})
	})

	Context("ForDynamic", func() {
		testInterfaceFuncs(func() resource.Interface {
			return resource.ForDynamic(dynamicfake.NewSimpleDynamicClient(scheme.Scheme).Resource(
				schema.GroupVersionResource{
					Group:    corev1.SchemeGroupVersion.Group,
					Version:  corev1.SchemeGroupVersion.Version,
					Resource: "pods",
				}).Namespace(test.LocalNamespace))
		}, resource.MustToUnstructured(&corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Pod",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: test.LocalNamespace,
			},
			Spec: corev1.PodSpec{
				Hostname: "my-host",
			},
		}))
	})
})

func testInterfaceFuncs(newInterface func() resource.Interface, initialObj runtime.Object) {
	Specify("verify functions", func() {
		sanitize := func(o runtime.Object) runtime.Object {
			m := resource.MustToMeta(o)
			m.SetResourceVersion("")
			m.SetCreationTimestamp(metav1.Time{})

			return o
		}

		i := newInterface()
		initialObj = sanitize(initialObj)

		another := initialObj.DeepCopyObject()
		resource.MustToMeta(another).SetName(resource.MustToMeta(initialObj).GetName() + "-2")

		// Create
		actual, err := i.Create(context.Background(), initialObj, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		objMeta := resource.MustToMeta(actual)
		actual = sanitize(actual)
		Expect(actual).To(Equal(initialObj))

		// Get
		obj, err := i.Get(context.Background(), objMeta.GetName(), metav1.GetOptions{})
		Expect(err).To(Succeed())
		Expect(sanitize(obj)).To(Equal(actual))

		// Update
		objMeta.SetLabels(map[string]string{"foo": "bar"})
		obj, err = i.Update(context.Background(), actual, metav1.UpdateOptions{})
		Expect(err).To(Succeed())
		Expect(sanitize(obj)).To(Equal(actual))

		// List
		list, err := i.List(context.Background(), metav1.ListOptions{})
		Expect(err).To(Succeed())
		Expect(list).To(HaveLen(1))
		Expect(sanitize(list[0])).To(Equal(actual))

		// List with label selector
		_, err = i.Create(context.Background(), another, metav1.CreateOptions{})
		Expect(err).To(Succeed())

		list, err = i.List(context.Background(), metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(objMeta.GetLabels()).String(),
		})
		Expect(err).To(Succeed())
		Expect(list).To(HaveLen(1))
		Expect(sanitize(list[0])).To(Equal(actual))

		// Delete
		err = i.Delete(context.Background(), objMeta.GetName(), metav1.DeleteOptions{})
		Expect(err).To(Succeed())
		_, err = i.Get(context.Background(), objMeta.GetName(), metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
}
