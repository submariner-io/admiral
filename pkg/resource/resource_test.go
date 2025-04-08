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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
)

var _ = Describe("EnsureValidName", func() {
	When("the string is valid", func() {
		It("should not convert it", func() {
			Expect(resource.EnsureValidName("digits-1234567890")).To(Equal("digits-1234567890"))
			Expect(resource.EnsureValidName("example.com")).To(Equal("example.com"))
		})
	})

	When("the string has upper case letters", func() {
		It("should convert to lower", func() {
			Expect(resource.EnsureValidName("No-UPPER-caSe-aLLoweD")).To(Equal("no-upper-case-allowed"))
		})
	})

	When("the string has non-alphanumeric letters", func() {
		It("should convert them appropriately", func() {
			Expect(resource.EnsureValidName("no-!@*()#$-chars")).To(Equal("no---------chars"))
		})
	})

	When("the string starts or ends with an invalid char", func() {
		It("should strip the char", func() {
			Expect(resource.EnsureValidName("-abc")).To(Equal("abc"))
			Expect(resource.EnsureValidName("abc-")).To(Equal("abc"))
			Expect(resource.EnsureValidName("-abc-")).To(Equal("abc"))
			Expect(resource.EnsureValidName("%abc*")).To(Equal("abc"))
		})
	})
})

var _ = Describe("TrimManagedFields", func() {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pod",
			Namespace:   "ns",
			Labels:      map[string]string{"app": "test"},
			Annotations: map[string]string{"foo": "bar"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: "image",
					Name:  "httpd",
				},
			},
		},
	}

	When("the object has metadata", func() {
		It("should succeed and trim the ManagedFields", func() {
			podWithMF := pod.DeepCopy()
			podWithMF.ManagedFields = []metav1.ManagedFieldsEntry{
				{
					Manager:    "kubectl",
					Operation:  metav1.ManagedFieldsOperationApply,
					APIVersion: "v1",
					Time:       ptr.To(metav1.Now()),
					FieldsType: "FieldsV1",
					FieldsV1:   ptr.To(metav1.FieldsV1{}),
				},
			}

			retObj, err := resource.TrimManagedFields(podWithMF)
			Expect(err).To(Succeed())
			Expect(podWithMF).To(Equal(pod))
			Expect(retObj).To(BeIdenticalTo(podWithMF))

			_, err = resource.TrimManagedFields(podWithMF)
			Expect(err).To(Succeed())
			Expect(podWithMF).To(Equal(pod))
		})
	})

	When("the object does not have metadata", func() {
		It("should succeed and return the object", func() {
			obj := &cache.DeletedFinalStateUnknown{
				Key: "key",
				Obj: pod,
			}

			retObj, err := resource.TrimManagedFields(obj)
			Expect(err).To(Succeed())
			Expect(retObj).To(Equal(obj))
		})
	})
})

var _ = Describe("ToUnstructured", func() {
	Context("with an Unstructured instance", func() {
		It("should return a copy", func() {
			obj := newUnstructuredPod()

			u, err := resource.ToUnstructured(obj)
			Expect(err).To(Succeed())
			Expect(u).To(Equal(obj))

			_ = unstructured.SetNestedField(u.Object, "host", "spec", "hostName")
			Expect(u).ToNot(Equal(obj))
		})
	})

	Context("with a Pod instance", func() {
		It("should return a correct Unstructured instance", func() {
			pod := newPod()

			u, err := resource.ToUnstructured(pod)
			Expect(err).To(Succeed())
			Expect(u.GetName()).To(Equal(pod.Name))
			Expect(u.GetNamespace()).To(Equal(pod.Namespace))
			Expect(u.GetLabels()).To(Equal(pod.Labels))

			s, _, _ := unstructured.NestedString(u.Object, "spec", "nodeName")
			Expect(s).To(Equal(pod.Spec.NodeName))
		})
	})
})

var _ = Describe("ToUnstructuredUsingScheme", func() {
	When("the Pod kind is not in the scheme", func() {
		It("should return an error", func() {
			_, err := resource.ToUnstructuredUsingScheme(&corev1.Pod{}, runtime.NewScheme())
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("MustToUnstructuredUsingScheme", func() {
	When("the Pod kind is not in the scheme", func() {
		It("should panic", func() {
			Expect(func() {
				_ = resource.MustToUnstructuredUsingScheme(&corev1.Pod{}, runtime.NewScheme())
			}).To(Panic())
		})
	})
})

var _ = Describe("MustToUnstructuredUsingDefaultConverter", func() {
	Context("with an Unstructured instance", func() {
		It("should return a copy", func() {
			obj := newUnstructuredPod()

			u := resource.MustToUnstructuredUsingDefaultConverter(newUnstructuredPodWithKind())
			Expect(u).To(Equal(obj))

			_ = unstructured.SetNestedField(u.Object, "host", "spec", "hostName")
			Expect(u).ToNot(Equal(obj))
		})
	})

	Context("with a Pod instance", func() {
		It("should return a correct Unstructured instance", func() {
			pod := newPod()

			u := resource.MustToUnstructuredUsingDefaultConverter(pod)
			Expect(u.GetName()).To(Equal(pod.Name))
			Expect(u.GetNamespace()).To(Equal(pod.Namespace))
			Expect(u.GetLabels()).To(Equal(pod.Labels))

			s, _, _ := unstructured.NestedString(u.Object, "spec", "nodeName")
			Expect(s).To(Equal(pod.Spec.NodeName))
		})
	})
})

var _ = Describe("MustFromUnstructured", func() {
	It("should return a correct Unstructured instance", func() {
		u := newUnstructuredPodWithKind()
		pod := resource.MustFromUnstructured(u, &corev1.Pod{})

		Expect(pod.Name).To(Equal(u.GetName()))
		Expect(pod.Namespace).To(Equal(u.GetNamespace()))
		Expect(pod.Labels).To(Equal(u.GetLabels()))

		s, _, _ := unstructured.NestedString(u.Object, "spec", "nodeName")
		Expect(s).To(Equal(pod.Spec.NodeName))
	})

	When("the Kind field is missing", func() {
		It("should panic", func() {
			Expect(func() {
				_ = resource.MustFromUnstructured(newUnstructuredPod(), &corev1.Pod{})
			}).To(Panic())
		})
	})
})

var _ = Describe("MustFromUnstructuredUsingScheme", func() {
	When("the Pod kind is not in the scheme", func() {
		It("should panic", func() {
			Expect(func() {
				_ = resource.MustFromUnstructuredUsingScheme(newUnstructuredPodWithKind(), &corev1.Pod{}, runtime.NewScheme())
			}).To(Panic())
		})
	})
})

var _ = Describe("MustToMeta", func() {
	It("should return the meta Object", func() {
		pod := newPod()
		Expect(resource.MustToMeta(pod).GetName()).To(Equal(pod.Name))

		u := newUnstructuredPod()
		Expect(resource.MustToMeta(u).GetName()).To(Equal(u.GetName()))
	})

	When("the instance isn't a meta Object", func() {
		It("should panic", func() {
			Expect(func() {
				_ = resource.MustToMeta(&corev1.PodSpec{})
			}).To(Panic())
		})
	})
})

var _ = Describe("ToJSON", func() {
	It("should return the JSON string", func() {
		pod := newPod()
		json := resource.ToJSON(pod)

		Expect(json).To(ContainSubstring("%q: %q", "name", pod.Name))
		Expect(json).To(ContainSubstring("%q: %q", "namespace", pod.Namespace))
		Expect(json).To(ContainSubstring("%q: %q", "nodeName", pod.Spec.NodeName))
	})
})

func newUnstructuredPod() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"metadata": map[string]any{
				"name":      "test-pod",
				"namespace": "ns",
				"labels":    map[string]any{"app": "test"},
			},
			"spec": map[string]any{
				"nodeName": "node",
			},
		},
	}
}

func newUnstructuredPodWithKind() *unstructured.Unstructured {
	u := newUnstructuredPod()
	u.SetKind("Pod")
	u.SetAPIVersion("v1")

	return u
}

func newPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "ns",
			Labels:    map[string]string{"app": "test"},
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}
}
