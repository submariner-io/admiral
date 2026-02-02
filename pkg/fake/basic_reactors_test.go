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
		It("should set Name field", func(ctx SpecContext) {
			actual := t.assertCreateSuccess(ctx, t.pod)
			Expect(actual.Name).To(HavePrefix(t.pod.GenerateName))
		})
	})

	It("should set the ResourceVersion field", func(ctx SpecContext) {
		actual := t.assertCreateSuccess(ctx, t.pod)
		Expect(actual.ResourceVersion).To(Equal("1"))
	})

	It("should set the UID field", func(ctx SpecContext) {
		actual := t.assertCreateSuccess(ctx, t.pod)
		Expect(actual.UID).ToNot(BeEmpty())
	})

	Specify("should set the CreationTimestamp field if not specified", func(ctx SpecContext) {
		now := metav1.Now()
		actual := t.assertCreateSuccess(ctx, t.pod)
		Expect(actual.CreationTimestamp.After(now.Add(-time.Second * 5))).To(BeTrue())
	})

	Specify("should not set the CreationTimestamp field if specified", func(ctx SpecContext) {
		cst := metav1.Time{Time: metav1.Now().Add(time.Hour)}
		t.pod.CreationTimestamp = cst
		actual := t.assertCreateSuccess(ctx, t.pod)
		Expect(actual.CreationTimestamp).To(Equal(cst))
	})

	When("the Name and GenerateName fields are empty", func() {
		It("should return an error", func(ctx SpecContext) {
			t.pod.GenerateName = ""
			_, err := t.doCreate(ctx, t.pod)
			Expect(err).To(HaveOccurred())
		})
	})

	When("the ResourceVersion field is set", func() {
		It("should return an error", func(ctx SpecContext) {
			t.pod.ResourceVersion = "2"
			_, err := t.doCreate(ctx, t.pod)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Update", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func(ctx SpecContext) {
		t.pod = t.assertCreateSuccess(ctx, t.pod)
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

	It("should update the resource", func(ctx SpecContext) {
		actual := t.doUpdateSuccess(ctx)
		Expect(actual.Spec).To(Equal(t.pod.Spec))
		Expect(actual.ResourceVersion > t.pod.ResourceVersion).To(BeTrue())
	})

	When("the Name field is empty", func() {
		It("should return an Invalid error", func(ctx SpecContext) {
			t.pod.Name = ""
			_, err := t.doUpdate(ctx)
			Expect(apierrors.IsInvalid(err)).To(BeTrue())
		})
	})

	When("the ResourceVersion field doesn't match", func() {
		It("should return a Conflict error", func(ctx SpecContext) {
			t.pod.ResourceVersion = "111"
			_, err := t.doUpdate(ctx)
			Expect(apierrors.IsConflict(err)).To(BeTrue())
		})
	})

	When("the resource is deleting and the finalizers are empty", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"some-finalizer"}
		})

		It("should delete the resource", func(ctx SpecContext) {
			t.pod.SetDeletionTimestamp(ptr.To(metav1.Now()))
			t.pod = t.doUpdateSuccess(ctx)

			t.pod.Finalizers = nil
			_, err := t.doUpdate(ctx)
			Expect(err).To(Succeed())

			_, err = t.doGet(ctx, t.pod.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})
})

var _ = Describe("Delete", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func(ctx SpecContext) {
		t.pod = t.assertCreateSuccess(ctx, t.pod)
	})

	It("should delete the resource", func(ctx SpecContext) {
		Expect(t.doDelete(ctx, metav1.DeleteOptions{})).To(Succeed())
		_, err := t.doGet(ctx, t.pod.Name)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	When("a specific ResourceVersion is requested", func() {
		Context("and it matches", func() {
			It("should delete the resource", func(ctx SpecContext) {
				Expect(t.doDelete(ctx, metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{
						ResourceVersion: &t.pod.ResourceVersion,
					},
				})).To(Succeed())
				_, err := t.doGet(ctx, t.pod.Name)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("and it doesn't match", func() {
			It("should return a Conflict error", func(ctx SpecContext) {
				err := t.doDelete(ctx, metav1.DeleteOptions{
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

		It("should set the DeletionTimestamp field", func(ctx SpecContext) {
			Expect(t.doDelete(ctx, metav1.DeleteOptions{})).To(Succeed())
			actual, err := t.doGet(ctx, t.pod.Name)
			Expect(err).To(Succeed())
			Expect(actual.GetDeletionTimestamp()).ToNot(BeNil())

			Expect(t.doDelete(ctx, metav1.DeleteOptions{})).To(Succeed())
			unchanged, err := t.doGet(ctx, t.pod.Name)
			Expect(err).To(Succeed())
			Expect(unchanged.GetResourceVersion()).To(Equal(actual.GetResourceVersion()))
		})
	})
})

var _ = Describe("List", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func(ctx SpecContext) {
		t.pod = t.assertCreateSuccess(ctx, t.pod)
		t.assertCreateSuccess(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-pod",
			},
		})
	})

	When("a field selector is specified", func() {
		It("should return the correct resources", func(ctx SpecContext) {
			list, err := t.client.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{
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

	JustBeforeEach(func(ctx SpecContext) {
		var err error

		watcher, err = t.client.CoreV1().Pods(testNamespace).Watch(ctx, metav1.ListOptions{
			LabelSelector: k8slabels.SelectorFromSet(t.pod.Labels).String(),
		})
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		watcher.Stop()
	})

	When("a label selector is specified", func() {
		It("should correctly filter the resources", func(ctx SpecContext) {
			t.pod = t.assertCreateSuccess(ctx, t.pod)

			select {
			case event, ok := <-watcher.ResultChan():
				Expect(ok).To(BeTrue())

				//nolint:exhaustive // Other types handled in default case.
				switch event.Type {
				case watch.Added:
					Expect(resource.MustToMeta(event.Object).GetName()).To(Equal(t.pod.Name))
				default:
					Fail("Received unexpected watch event: " + resource.ToJSON(event))
				}
			case <-time.After(1 * time.Second):
				Fail("Did not receive expected watch event")
			}

			t.assertCreateSuccess(ctx, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-pod",
				},
			})

			select {
			case event := <-watcher.ResultChan():
				Fail("Received unexpected watch event: " + resource.ToJSON(event))
			case <-time.After(300 * time.Millisecond):
			}
		})
	})
})

var _ = Describe("DeleteCollection", func() {
	t := newBasicReactorsTestDriver()

	JustBeforeEach(func(ctx SpecContext) {
		t.pod = t.assertCreateSuccess(ctx, t.pod)
	})

	It("should delete the correct resources", func(ctx SpecContext) {
		otherPod := t.assertCreateSuccess(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "other-pod",
			},
		})

		err := t.client.CoreV1().Pods(testNamespace).DeleteCollection(ctx, metav1.DeleteOptions{},
			metav1.ListOptions{
				LabelSelector: k8slabels.SelectorFromSet(t.pod.Labels).String(),
			})

		Expect(err).To(Succeed())

		_, err = t.doGet(ctx, t.pod.Name)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		t.assertGetSuccess(ctx, otherPod)
	})
})

var _ = Describe("Namespace verification", func() {
	t := newBasicReactorsTestDriver()

	BeforeEach(func() {
		fake.AddVerifyNamespaceReactor(&t.client.Fake, "*")
	})

	assertMissingNamespaceErr := func(err error) {
		Expect(resource.IsMissingNamespaceErr(err)).To(BeTrue())
		Expect(resource.ExtractMissingNamespaceFromErr(err)).To(Equal(testNamespace))
	}

	createNamespace := func(ctx context.Context, name string) {
		_, err := t.client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		}, metav1.CreateOptions{})
		Expect(err).To(Succeed())
	}

	When("a namespace does not exist", func() {
		Specify("requests should return the appropriate error", func(ctx SpecContext) {
			_, err := t.doCreate(ctx, t.pod)
			assertMissingNamespaceErr(err)

			_, err = t.doGet(ctx, t.pod.Name)
			assertMissingNamespaceErr(err)

			_, err = t.doUpdate(ctx)
			assertMissingNamespaceErr(err)

			assertMissingNamespaceErr(t.doDelete(ctx, metav1.DeleteOptions{}))

			_, err = t.client.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
			assertMissingNamespaceErr(err)
		})
	})

	When("a request has no namespace", func() {
		It("should succeed", func(ctx SpecContext) {
			_, err := t.client.CoreV1().Pods("").Create(ctx, t.pod, metav1.CreateOptions{})
			Expect(err).To(Succeed())
		})
	})

	When("a namespace does exist", func() {
		Specify("requests should succeed", func(ctx SpecContext) {
			createNamespace(ctx, testNamespace)

			t.pod.Name = "test-pod"
			t.assertCreateSuccess(ctx, t.pod)

			_, err := t.client.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
			Expect(err).To(Succeed())

			Expect(t.doDelete(ctx, metav1.DeleteOptions{})).To(Succeed())
		})
	})

	When("a namespace is deleted", func() {
		const noDeleteNS = "no-delete"

		It("should delete all contained resources", func(ctx SpecContext) {
			createNamespace(ctx, testNamespace)
			createNamespace(ctx, noDeleteNS)

			t.pod.Name = "test-pod"
			t.assertCreateSuccess(ctx, t.pod)

			_, err := t.client.CoreV1().Services(testNamespace).Create(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "should-delete",
				},
			}, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			_, err = t.client.CoreV1().Services(noDeleteNS).Create(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "should-not-delete",
				},
			}, metav1.CreateOptions{})
			Expect(err).To(Succeed())

			// Delete the namespace

			Expect(t.client.CoreV1().Namespaces().Delete(ctx, testNamespace, metav1.DeleteOptions{})).To(Succeed())

			_, err = t.doGet(ctx, t.pod.Name)
			assertMissingNamespaceErr(err)

			// Recreate the namespace

			createNamespace(ctx, testNamespace)

			_, err = t.doGet(ctx, t.pod.Name)
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			_, err = t.client.CoreV1().Services(testNamespace).Get(ctx, "should-delete", metav1.GetOptions{})
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			_, err = t.client.CoreV1().Services(noDeleteNS).Get(ctx, "should-not-delete", metav1.GetOptions{})
			Expect(err).To(Succeed())
		})
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

func (t *basicReactorsTestDriver) doGet(ctx context.Context, name string) (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Get(ctx, name, metav1.GetOptions{})
}

func (t *basicReactorsTestDriver) assertGetSuccess(ctx context.Context, p *corev1.Pod) *corev1.Pod {
	actual, err := t.doGet(ctx, p.Name)
	Expect(err).To(Succeed())
	Expect(actual).To(Equal(p))

	return actual
}

func (t *basicReactorsTestDriver) doCreate(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Create(ctx, pod, metav1.CreateOptions{})
}

func (t *basicReactorsTestDriver) assertCreateSuccess(ctx context.Context, pod *corev1.Pod) *corev1.Pod {
	created, err := t.doCreate(ctx, pod)
	Expect(err).To(Succeed())

	return t.assertGetSuccess(ctx, created)
}

func (t *basicReactorsTestDriver) doUpdate(ctx context.Context) (*corev1.Pod, error) {
	return t.client.CoreV1().Pods(testNamespace).Update(ctx, t.pod, metav1.UpdateOptions{})
}

func (t *basicReactorsTestDriver) doUpdateSuccess(ctx context.Context) *corev1.Pod {
	updated, err := t.doUpdate(ctx)
	Expect(err).To(Succeed())

	return t.assertGetSuccess(ctx, updated)
}

//nolint:gocritic // Ignore hugeParam
func (t *basicReactorsTestDriver) doDelete(ctx context.Context, opts metav1.DeleteOptions) error {
	return t.client.CoreV1().Pods(testNamespace).Delete(ctx, t.pod.Name, opts)
}
