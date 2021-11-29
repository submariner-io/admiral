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

package finalizer_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/finalizer"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeFake "k8s.io/client-go/kubernetes/fake"
)

const finalizerName = "test-finalizer"

func TestFinalizer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Finalizer Suite")
}

var _ = Describe("Add", func() {
	var (
		t     *testDriver
		added bool
		err   error
	)

	BeforeEach(func() {
		t = newTestDriver()
	})

	JustBeforeEach(func() {
		t.justBeforeEach()
		added, err = finalizer.Add(context.TODO(), t.client, t.pod, finalizerName)
	})

	When("the resource has no Finalizers", func() {
		It("should add the new one", func() {
			Expect(err).To(Succeed())
			Expect(added).To(BeTrue())
			t.assertFinalizers(finalizerName)
		})

		Context("and update initially fails with a conflict error", func() {
			BeforeEach(func() {
				fake.ConflictOnUpdateReactor(&t.kubeClient.Fake, "pods")
			})

			It("should eventually succeed", func() {
				Expect(err).To(Succeed())
				Expect(added).To(BeTrue())
				t.assertFinalizers(finalizerName)
			})
		})
	})

	When("the resource has other Finalizers", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"other"}
		})

		It("should append the new one", func() {
			Expect(err).To(Succeed())
			Expect(added).To(BeTrue())
			t.assertFinalizers("other", finalizerName)
		})
	})

	When("the resource already has the Finalizer", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{finalizerName}
		})

		It("should not try to re-add it", func() {
			Expect(err).To(Succeed())
			Expect(added).To(BeFalse())
			test.EnsureNoActionsForResource(&t.kubeClient.Fake, "pods", "get", "update")
			t.assertFinalizers(finalizerName)
		})
	})

	When("the resource is being deleted", func() {
		BeforeEach(func() {
			now := metav1.Now()
			t.pod.DeletionTimestamp = &now
		})

		It("should not add the Finalizer", func() {
			Expect(err).To(Succeed())
			Expect(added).To(BeFalse())
			t.assertFinalizers()
		})
	})

	When("update fails", func() {
		BeforeEach(func() {
			fake.FailOnAction(&t.kubeClient.Fake, "pods", "update", nil, false)
		})

		It("should return an error", func() {
			Expect(err).To(HaveOccurred())
			Expect(added).To(BeFalse())
		})
	})
})

var _ = Describe("Remove", func() {
	var (
		t   *testDriver
		err error
	)

	BeforeEach(func() {
		t = newTestDriver()
		t.pod.Finalizers = []string{finalizerName}
	})

	JustBeforeEach(func() {
		t.justBeforeEach()
		err = finalizer.Remove(context.TODO(), t.client, t.pod, finalizerName)
	})

	When("the Finalizer is present", func() {
		It("should remove it", func() {
			Expect(err).To(Succeed())
			t.assertFinalizers()
		})

		Context("and update initially fails with a conflict error", func() {
			BeforeEach(func() {
				fake.ConflictOnUpdateReactor(&t.kubeClient.Fake, "pods")
			})

			It("should eventually succeed", func() {
				Expect(err).To(Succeed())
				t.assertFinalizers()
			})
		})
	})

	When("other Finalizers are also present", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"other1", finalizerName, "other2"}
		})

		It("should not remove the others", func() {
			Expect(err).To(Succeed())
			t.assertFinalizers("other1", "other2")
		})
	})

	When("the Finalizer is not present", func() {
		BeforeEach(func() {
			t.pod.Finalizers = []string{"other"}
		})

		It("should not try to remove it", func() {
			Expect(err).To(Succeed())
			test.EnsureNoActionsForResource(&t.kubeClient.Fake, "pods", "get", "update")
			t.assertFinalizers("other")
		})
	})

	When("update fails", func() {
		BeforeEach(func() {
			fake.FailOnAction(&t.kubeClient.Fake, "pods", "update", nil, false)
		})

		It("should return an error", func() {
			Expect(err).To(HaveOccurred())
		})
	})
})

type testDriver struct {
	pod        *corev1.Pod
	kubeClient *kubeFake.Clientset
	client     resource.Interface
}

func newTestDriver() *testDriver {
	t := &testDriver{
		pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-pod",
				Namespace:  "test-ns",
				Finalizers: []string{},
			},
		},
		kubeClient: kubeFake.NewSimpleClientset(),
	}

	return t
}

func (t *testDriver) justBeforeEach() {
	t.client = resource.ForPod(t.kubeClient, t.pod.Namespace)

	_, err := t.client.Create(context.TODO(), t.pod, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	t.kubeClient.Fake.ClearActions()
}

func (t *testDriver) assertFinalizers(exp ...string) {
	test.AssertFinalizers(t.client, t.pod.Name, exp...)
}
