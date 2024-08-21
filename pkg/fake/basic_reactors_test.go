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

package fake_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

const testNamespace = "test-ns"

var _ = Describe("Create", func() {
	t := newBasicReactorsTestDriver()

	When("the GenerateName field is set", func() {
		It("should set Name field", func() {
			actual := t.assertCreateSuccess(t.pod)
			Expect(actual.Name).To(HavePrefix(t.pod.GenerateName))
		})
	})

	It("should set the ResourceVersion field", func() {
		actual := t.assertCreateSuccess(t.pod)
		Expect(actual.ResourceVersion).To(Equal("1"))
	})

	It("should set the UID field", func() {
		actual := t.assertCreateSuccess(t.pod)
		Expect(actual.UID).ToNot(BeEmpty())
	})

	When("the Name and GenerateName fields are empty", func() {
		It("should return an error", func() {
			t.pod.GenerateName = ""
			_, err := t.doCreate(t.pod)
			Expect(err).To(HaveOccurred())
		})
	})

	When("the ResourceVersion field is set", func() {
		It("should return an error", func() {
			t.pod.ResourceVersion = "2"
			_, err := t.doCreate(t.pod)
			Expect(err).To(HaveOccurred())
		})
	})

	Context("with namespace verification", func() {
		BeforeEach(func() {
			fake.AddVerifyNamespaceReactor(&t.client.Fake, "pods")
		})

		When("the namespace does not exist", func() {
			It("should return an error", func() {
				_, err := t.doCreate(t.pod)
				Expect(resource.IsMissingNamespaceErr(err)).To(BeTrue())
				Expect(resource.ExtractMissingNamespaceFromErr(err)).To(Equal(testNamespace))
			})
		})

		When("the namespace does exist", func() {
			It("should succeed", func() {
				_, err := t.client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
				}, metav1.CreateOptions{})
				Expect(err).To(Succeed())

				t.assertCreateSuccess(t.pod)
			})
		})
	})
})

var _ = Describe("Update", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func() {
		t.pod = t.assertCreateSuccess(t.pod)
		t.pod.Namespace = ""
		t.pod.Spec = corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: "image2",
					Name:  "server2",
				},
			},
		}
	})

	It("should update the resource", func() {
		actual := t.doUpdateSuccess()
		Expect(actual.Spec).To(Equal(t.pod.Spec))
		Expect(actual.ResourceVersion > t.pod.ResourceVersion).To(BeTrue())
	})

	When("the Name field is empty", func() {
		It("should return an Invalid error", func() {
			t.pod.Name = ""
			_, err := t.doUpdate()
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})
	})

	When("the ResourceVersion field doesn't match", func() {
		It("should return a Conflict error", func() {
			t.pod.ResourceVersion = "111"
			_, err := t.doUpdate()
			Expect(apierrors.IsConflict(err)).To(BeTrue())
		})
	})

	When("the resource is deleting and the finalizers are empty", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"some-finalizer"}
		})

		It("should delete the resource", func() {
			t.pod.SetDeletionTimestamp(ptr.To(metav1.Now()))
			t.pod = t.doUpdateSuccess()

			t.pod.Finalizers = nil
			_, err := t.doUpdate()
			Expect(err).To(Succeed())

			_, err = t.doGet(t.pod.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})
})

var _ = Describe("Delete", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func() {
		t.pod = t.assertCreateSuccess(t.pod)
	})

	It("should delete the resource", func() {
		Expect(t.doDelete(metav1.DeleteOptions{})).To(Succeed())
		_, err := t.doGet(t.pod.Name)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	When("a specific ResourceVersion is requested", func() {
		Context("and it matches", func() {
			It("should delete the resource", func() {
				Expect(t.doDelete(metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{
						ResourceVersion: &t.pod.ResourceVersion,
					},
				})).To(Succeed())
				_, err := t.doGet(t.pod.Name)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("and it doesn't match", func() {
			It("should return a Conflict error", func() {
				err := t.doDelete(metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{
						ResourceVersion: ptr.To("111"),
					},
				})
				Expect(apierrors.IsConflict(err)).To(BeTrue())
			})
		})
	})

	When("there's remaining finalizers", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"some-finalizer"}
		})

		It("should set the DeletionTimestamp field", func() {
			Expect(t.doDelete(metav1.DeleteOptions{})).To(Succeed())
			actual, err := t.doGet(t.pod.Name)
			Expect(err).To(Succeed())
			Expect(actual.GetDeletionTimestamp()).ToNot(BeNil())

			Expect(t.doDelete(metav1.DeleteOptions{})).To(Succeed())
			unchanged, err := t.doGet(t.pod.Name)
			Expect(err).To(Succeed())
			Expect(unchanged.GetResourceVersion()).To(Equal(actual.GetResourceVersion()))
		})
	})
})

var _ = Describe("List", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func() {
		t.pod = t.assertCreateSuccess(t.pod)
		t.assertCreateSuccess(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-pod",
			},
		})
	})

	When("a field selector is specified", func() {
		It("should return the correct resources", func() {
			list, err := t.client.CoreV1().Pods(testNamespace).List(context.Background(), metav1.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("metadata.name", t.pod.Name).String(),
			})

			Expect(err).To(Succeed())
			Expect(list.Items).To(HaveLen(1))
			Expect(list.Items[0].Name).To(Equal(t.pod.Name))
		})
	})
})

var _ = Describe("Watch", func() {
	t := newBasicReactorsTestDriver()

	var watcher watch.Interface

	JustBeforeEach(func() {
		var err error

		watcher, err = t.client.CoreV1().Pods(testNamespace).Watch(context.Background(), metav1.ListOptions{
			LabelSelector: k8slabels.SelectorFromSet(t.pod.Labels).String(),
		})
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		watcher.Stop()
	})

	When("a label selector is specified", func() {
		It("should correctly filter the resources", func() {
			t.pod = t.assertCreateSuccess(t.pod)

			select {
			case event, ok := <-watcher.ResultChan():
				Expect(ok).To(BeTrue())

				//nolint:exhaustive // Other types handled in default case.
				switch event.Type {
				case watch.Added:
					Expect(resource.MustToMeta(event.Object).GetName()).To(Equal(t.pod.Name))
				default:
					Fail(fmt.Sprintf("Received unexpected watch event: %s", resource.ToJSON(event)))
				}
			case <-time.After(1 * time.Second):
				Fail("Did not receive expected watch event")
			}

			t.assertCreateSuccess(&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-pod",
				},
			})

			select {
			case event := <-watcher.ResultChan():
				Fail(fmt.Sprintf("Received unexpected watch event: %s", resource.ToJSON(event)))
			case <-time.After(300 * time.Millisecond):
			}
		})
	})
})

var _ = Describe("DeleteCollection", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func() {
		t.pod = t.assertCreateSuccess(t.pod)
	})

	It("should delete the correct resources", func() {
		otherPod := t.assertCreateSuccess(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-pod",
			},
		})

		err := t.client.CoreV1().Pods(testNamespace).DeleteCollection(context.Background(), metav1.DeleteOptions{},
			metav1.ListOptions{
				LabelSelector: k8slabels.SelectorFromSet(t.pod.Labels).String(),
			})

		Expect(err).To(Succeed())

		_, err = t.doGet(t.pod.Name)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		t.assertGetSuccess(otherPod)
	})
})

type basicReactorsTestDriver struct {
	client *k8sfake.Clientset
	pod    *corev1.Pod
}

func newBasicReactorsTestDriver() *basicReactorsTestDriver {
	t := &basicReactorsTestDriver{}

	BeforeEach(func() {
		t.client = k8sfake.NewClientset()
		fake.AddBasicReactors(&t.client.Fake)

		t.pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "pod-",
				Labels:       map[string]string{"app": "test"},
				Annotations:  map[string]string{"foo": "bar"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "image1",
						Name:  "server1",
					},
				},
			},
		}
	})

	return t
}

func (t *basicReactorsTestDriver) doGet(name string) (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
}

func (t *basicReactorsTestDriver) assertGetSuccess(p *corev1.Pod) *corev1.Pod {
	actual, err := t.doGet(p.Name)
	Expect(err).To(Succeed())
	Expect(actual).To(Equal(p))

	return actual
}

func (t *basicReactorsTestDriver) doCreate(pod *corev1.Pod) (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Create(context.Background(), pod, metav1.CreateOptions{})
}

func (t *basicReactorsTestDriver) assertCreateSuccess(pod *corev1.Pod) *corev1.Pod {
	created, err := t.doCreate(pod)
	Expect(err).To(Succeed())

	return t.assertGetSuccess(created)
}

func (t *basicReactorsTestDriver) doUpdate() (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Update(context.Background(), t.pod, metav1.UpdateOptions{})
}

func (t *basicReactorsTestDriver) doUpdateSuccess() *corev1.Pod {
	updated, err := t.doUpdate()
	Expect(err).To(Succeed())

	return t.assertGetSuccess(updated)
}

//nolint:gocritic // Ignore hugeParam
func (t *basicReactorsTestDriver) doDelete(opts metav1.DeleteOptions) error {
	return t.client.CoreV1().Pods(testNamespace).Delete(context.Background(), t.pod.Name, opts)
}
