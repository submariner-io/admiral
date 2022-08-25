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
package util_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	tests "github.com/submariner-io/admiral/pkg/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/testing"
)

var _ = Describe("CreateAnew function", func() {
	t := newCreateOrUpdateTestDiver()

	createAnew := func() (runtime.Object, error) {
		return util.CreateAnew(context.TODO(), resource.ForDynamic(t.client), t.pod, metav1.CreateOptions{}, metav1.DeleteOptions{})
	}

	createAnewSuccess := func() *corev1.Pod {
		o, err := createAnew()
		Expect(err).To(Succeed())
		Expect(o).ToNot(BeNil())

		actual := &corev1.Pod{}
		Expect(scheme.Scheme.Convert(o, actual, nil)).To(Succeed())

		return actual
	}

	createAnewError := func() error {
		_, err := createAnew()
		return err
	}

	When("the resource doesn't exist", func() {
		It("should successfully create the resource", func() {
			t.compareWithPod(createAnewSuccess())
		})
	})

	When("the resource already exists", func() {
		BeforeEach(func() {
			t.createPod()
		})

		Context("and the new resource spec differs", func() {
			BeforeEach(func() {
				t.pod.Spec.Containers[0].Image = "updated"
			})

			It("should delete the existing resource and create a new one", func() {
				t.compareWithPod(createAnewSuccess())
			})

			Context("and Delete returns not found", func() {
				BeforeEach(func() {
					t.client.FailOnDelete = apierrors.NewNotFound(schema.GroupResource{}, t.pod.Name)
				})

				It("should successfully create the resource", func() {
					t.compareWithPod(createAnewSuccess())
				})
			})

			Context("and Delete fails", func() {
				BeforeEach(func() {
					t.client.FailOnDelete = errors.New("delete failed")
					t.expectedErr = t.client.FailOnDelete
				})

				It("should return an error", func() {
					Expect(createAnewError()).To(ContainErrorSubstring(t.expectedErr))
				})
			})

			Context("and deletion doesn't complete in time", func() {
				BeforeEach(func() {
					t.client.PersistentFailOnCreate.Store(apierrors.NewAlreadyExists(schema.GroupResource{}, t.pod.Name))
				})

				It("should return an error", func() {
					Expect(createAnewError()).ToNot(Succeed())
				})
			})
		})

		Context("and the new resource spec does not differ", func() {
			BeforeEach(func() {
				t.pod.Status.Phase = corev1.PodRunning
			})

			It("should not recreate it", func() {
				createAnewSuccess()
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "delete")
			})
		})
	})

	When("Create fails", func() {
		BeforeEach(func() {
			t.client.FailOnCreate = errors.New("create failed")
			t.expectedErr = t.client.FailOnCreate
		})

		It("should return an error", func() {
			Expect(createAnewError()).To(ContainErrorSubstring(t.expectedErr))
		})
	})
})

var _ = Describe("CreateOrUpdate function", func() {
	t := newCreateOrUpdateTestDiver()

	createOrUpdate := func(expResult util.OperationResult) error {
		result, err := util.CreateOrUpdate(context.TODO(), resource.ForDynamic(t.client), test.ToUnstructured(t.pod), t.mutateFn)
		Expect(result).To(Equal(expResult))

		return err
	}

	When("the resource doesn't exist", func() {
		It("should successfully create the resource", func() {
			Expect(createOrUpdate(util.OperationResultCreated)).To(Succeed())
			t.verifyPod()
		})

		Context("and Create fails", func() {
			JustBeforeEach(func() {
				t.client.FailOnCreate = apierrors.NewServiceUnavailable("fake")
				t.expectedErr = t.client.FailOnCreate
			})

			It("should return an error", func() {
				Expect(createOrUpdate(util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
			})
		})

		Context("and Create initially returns AlreadyExists due to a simulated out-of-band create", func() {
			BeforeEach(func() {
				t.createPod()
				t.pod = test.NewPodWithImage("", "apache")
			})

			JustBeforeEach(func() {
				t.client.FailOnGet = apierrors.NewNotFound(schema.GroupResource{}, t.pod.GetName())
				t.client.FailOnCreate = apierrors.NewAlreadyExists(schema.GroupResource{}, t.pod.GetName())
			})

			It("should eventually update the resource", func() {
				Expect(createOrUpdate(util.OperationResultUpdated)).To(Succeed())
				t.verifyPod()
			})
		})
	})

	t.testUpdate(createOrUpdate)

	t.testGetFailure(createOrUpdate)
})

var _ = Describe("Update function", func() {
	t := newCreateOrUpdateTestDiver()

	update := func() error {
		return util.Update(context.TODO(), resource.ForDynamic(t.client), test.ToUnstructured(t.pod), t.mutateFn)
	}

	When("the resource doesn't exist", func() {
		It("shouldn't return an error", func() {
			Expect(update()).To(Succeed())
		})
	})

	t.testUpdate(func(_ util.OperationResult) error {
		return update()
	})

	t.testGetFailure(func(_ util.OperationResult) error {
		return update()
	})
})

var _ = Describe("MustUpdate function", func() {
	t := newCreateOrUpdateTestDiver()

	mustUpdate := func() error {
		return util.MustUpdate(context.TODO(), resource.ForDynamic(t.client), test.ToUnstructured(t.pod), t.mutateFn)
	}

	When("the resource doesn't exist", func() {
		It("should return an error", func() {
			Expect(mustUpdate()).ToNot(Succeed())
		})
	})

	t.testUpdate(func(_ util.OperationResult) error {
		return mustUpdate()
	})

	t.testGetFailure(func(_ util.OperationResult) error {
		return mustUpdate()
	})
})

type createOrUpdateTestDriver struct {
	pod         *corev1.Pod
	testingFake *testing.Fake
	client      *fake.DynamicResourceClient
	origBackoff wait.Backoff
	expectedErr error
	mutateFn    util.MutateFn
}

func newCreateOrUpdateTestDiver() *createOrUpdateTestDriver {
	t := &createOrUpdateTestDriver{}

	BeforeEach(func() {
		dynClient := fake.NewDynamicClient(scheme.Scheme)
		t.testingFake = &dynClient.Fake

		t.client, _ = dynClient.Resource(schema.GroupVersionResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Version:  corev1.SchemeGroupVersion.Version,
			Resource: "pods",
		}).Namespace("test").(*fake.DynamicResourceClient)

		t.client.CheckResourceVersionOnUpdate = true

		t.pod = test.NewPod("")

		t.origBackoff = util.SetBackoff(wait.Backoff{
			Steps:    5,
			Duration: 30 * time.Millisecond,
		})

		t.mutateFn = func(existing runtime.Object) (runtime.Object, error) {
			obj := test.ToUnstructured(t.pod)
			obj.SetUID(resource.ToMeta(existing).GetUID())
			return util.Replace(obj)(nil)
		}
	})

	AfterEach(func() {
		util.SetBackoff(t.origBackoff)
	})

	return t
}

func (t *createOrUpdateTestDriver) testGetFailure(doOper func(util.OperationResult) error) {
	When("resource retrieval fails", func() {
		JustBeforeEach(func() {
			t.client.FailOnGet = apierrors.NewServiceUnavailable("fake")
			t.expectedErr = t.client.FailOnGet
		})

		It("should return an error", func() {
			Expect(doOper(util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
		})
	})
}

func (t *createOrUpdateTestDriver) testUpdate(doUpdate func(util.OperationResult) error) {
	When("the resource already exists", func() {
		BeforeEach(func() {
			t.createPod()
			t.pod = test.NewPodWithImage("", "apache")
		})

		It("should update the resource", func() {
			Expect(doUpdate(util.OperationResultUpdated)).To(Succeed())
			t.verifyPod()
		})

		Context("and Update initially fails due to conflict", func() {
			BeforeEach(func() {
				t.client.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("conflict"))
			})

			It("should eventually update the resource", func() {
				Expect(doUpdate(util.OperationResultUpdated)).To(Succeed())
				t.verifyPod()
			})
		})

		Context("and Update fails not due to conflict", func() {
			JustBeforeEach(func() {
				t.client.FailOnUpdate = apierrors.NewServiceUnavailable("fake")
				t.expectedErr = t.client.FailOnUpdate
			})

			It("should return an error", func() {
				Expect(doUpdate(util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
			})
		})

		Context("and the resource to update is the same", func() {
			BeforeEach(func() {
				t.mutateFn = func(existing runtime.Object) (runtime.Object, error) {
					return existing, nil
				}
			})

			It("should not update the resource", func() {
				Expect(doUpdate(util.OperationResultNone)).To(Succeed())
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "update")
			})
		})

		Context("and the mutate function returns an error", func() {
			BeforeEach(func() {
				t.expectedErr = errors.New("mutate failure")
				t.mutateFn = func(existing runtime.Object) (runtime.Object, error) {
					return nil, t.expectedErr
				}
			})

			It("should return an error", func() {
				Expect(doUpdate(util.OperationResultNone)).ToNot(Succeed())
			})
		})
	})
}

func (t *createOrUpdateTestDriver) createPod() {
	test.CreateResource(t.client, t.pod)
}

func (t *createOrUpdateTestDriver) verifyPod() {
	t.compareWithPod(test.GetPod(t.client, t.pod))
}

func (t *createOrUpdateTestDriver) compareWithPod(actual *corev1.Pod) {
	Expect(actual.GetUID()).NotTo(Equal(t.pod.GetUID()))
	Expect(actual.Spec).To(Equal(t.pod.Spec))
}
