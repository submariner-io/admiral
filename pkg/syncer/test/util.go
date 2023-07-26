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

// Package test provides test utilities for the Syncer.
package test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	RemoteNamespace = "remote-ns"
	LocalNamespace  = "local-ns"
)

func GetResourceAndError(resourceInterface dynamic.ResourceInterface, obj runtime.Object) (*unstructured.Unstructured, error) {
	meta, err := metaapi.Accessor(obj)
	Expect(err).To(Succeed())

	return resourceInterface.Get(context.TODO(), meta.GetName(), metav1.GetOptions{})
}

func GetResource(resourceInterface dynamic.ResourceInterface, obj runtime.Object) *unstructured.Unstructured {
	ret, err := GetResourceAndError(resourceInterface, obj)
	Expect(err).To(Succeed())

	return ret
}

func CreateResource(resourceInterface dynamic.ResourceInterface, obj runtime.Object) *unstructured.Unstructured {
	u := ToUnstructured(obj)
	u.SetResourceVersion("")

	created, err := resourceInterface.Create(context.TODO(), u, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	return created
}

func UpdateResource(resourceInterface dynamic.ResourceInterface, obj runtime.Object) *unstructured.Unstructured {
	err := util.Update(context.Background(), resource.ForDynamic(resourceInterface), obj,
		util.Replace(obj))
	Expect(err).To(Succeed())

	return GetResource(resourceInterface, obj)
}

func VerifyResource(resourceInterface dynamic.ResourceInterface, expected *corev1.Pod, expNamespace, clusterID string) {
	actual := GetPod(resourceInterface, expected)

	Expect(actual.GetName()).To(Equal(expected.GetName()))
	Expect(actual.GetNamespace()).To(Equal(expNamespace))
	Expect(actual.GetAnnotations()).To(Equal(expected.GetAnnotations()))
	Expect(actual.GetOwnerReferences()).To(Equal(expected.GetOwnerReferences()))
	Expect(actual.Spec).To(Equal(expected.Spec))
	Expect(actual.Status).To(Equal(expected.Status))

	Expect(actual.GetUID()).NotTo(Equal(expected.GetUID()))
	Expect(actual.GetResourceVersion()).NotTo(Equal(expected.GetResourceVersion()))

	duplicate := make(map[string]string)
	for k, v := range expected.GetLabels() {
		duplicate[k] = v
	}

	if clusterID != "" {
		duplicate[federate.ClusterIDLabelKey] = clusterID
	}

	Expect(actual.GetLabels()).To(Equal(duplicate))
}

func GetPod(resourceInterface dynamic.ResourceInterface, from *corev1.Pod) *corev1.Pod {
	actual := &corev1.Pod{}

	raw := GetResource(resourceInterface, from)
	err := scheme.Scheme.Convert(raw, actual, nil)
	Expect(err).To(Succeed())

	return actual
}

func NewPod(namespace string) *corev1.Pod {
	return NewPodWithImage(namespace, "nginx")
}

func NewPodWithImage(namespace, imageName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-pod",
			Namespace:       namespace,
			UID:             uuid.NewUUID(),
			ResourceVersion: "10",
			Labels:          map[string]string{"app": "test"},
			Annotations:     map[string]string{"foo": "bar"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: imageName,
					Name:  "httpd",
				},
			},
		},
	}
}

func GetRESTMapperAndGroupVersionResourceFor(obj runtime.Object) (metaapi.RESTMapper, *schema.GroupVersionResource) {
	restMapper := GetRESTMapperFor(obj)
	return restMapper, GetGroupVersionResourceFor(restMapper, obj)
}

func GetRESTMapperFor(objs ...runtime.Object) metaapi.RESTMapper {
	gvs := make([]schema.GroupVersion, 0, len(objs))
	gvks := make([]schema.GroupVersionKind, 0, len(objs))

	for _, obj := range objs {
		gvk := GetGroupVersionKindFor(obj)
		gvks = append(gvks, gvk)
		gvs = append(gvs, gvk.GroupVersion())
	}

	restMapper := metaapi.NewDefaultRESTMapper(gvs)

	for _, gvk := range gvks {
		restMapper.Add(gvk, metaapi.RESTScopeNamespace)
	}

	return restMapper
}

func GetGroupVersionKindFor(obj runtime.Object) schema.GroupVersionKind {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	Expect(err).To(Succeed())
	Expect(gvks).ToNot(BeEmpty())

	return gvks[0]
}

func GetGroupVersionResourceFor(restMapper metaapi.RESTMapper, obj runtime.Object) *schema.GroupVersionResource {
	gvk := GetGroupVersionKindFor(obj)
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	Expect(err).To(Succeed())

	return &mapping.Resource
}

func PrepInitialClientObjs(namespace, clusterID string, initObjs ...runtime.Object) []runtime.Object {
	newObjs := make([]runtime.Object, 0, len(initObjs))

	for _, obj := range initObjs {
		raw := ToUnstructured(obj)
		raw.SetUID(uuid.NewUUID())
		raw.SetResourceVersion("1")

		if namespace != "" {
			raw.SetNamespace(namespace)
		}

		if clusterID != "" {
			labels := raw.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}

			labels[federate.ClusterIDLabelKey] = clusterID
			raw.SetLabels(labels)
		}

		newObjs = append(newObjs, raw)
	}

	return newObjs
}

func ToUnstructured(obj runtime.Object) *unstructured.Unstructured {
	raw, err := resource.ToUnstructured(obj)
	Expect(err).To(Succeed())

	return raw
}

func SetClusterIDLabel(obj runtime.Object, clusterID string) runtime.Object {
	meta, err := metaapi.Accessor(obj)
	Expect(err).To(Succeed())

	labels := meta.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	if clusterID == "" {
		delete(labels, federate.ClusterIDLabelKey)
	} else {
		labels[federate.ClusterIDLabelKey] = clusterID
	}

	meta.SetLabels(labels)

	return obj
}

func AwaitResource(client dynamic.ResourceInterface, name string) *unstructured.Unstructured {
	return AwaitAndVerifyResource(client, name, nil)
}

func AwaitAndVerifyResource(client dynamic.ResourceInterface, name string,
	verify func(*unstructured.Unstructured) bool,
) *unstructured.Unstructured {
	var found *unstructured.Unstructured

	Eventually(func() error {
		obj, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if verify == nil || verify(obj) {
			found = obj
			return nil
		}

		return fmt.Errorf("resource %q was found but not verified", name)
	}, 5*time.Second, 50*time.Millisecond).Should(Succeed())

	return found
}

func AwaitNoResource(client dynamic.ResourceInterface, name string) {
	Eventually(func() bool {
		_, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true
		}

		Expect(err).To(Succeed())

		return false
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "Resource %q still exists", name)
}
