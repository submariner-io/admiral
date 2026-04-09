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
	"errors"
	"maps"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	tests "github.com/submariner-io/admiral/pkg/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/testing"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("CreateAnew function", func() {
	t := newCreateOrUpdateTestDiver()

	createAnew := func(ctx context.Context) (runtime.Object, error) {
		return util.CreateAnew(t.ctx(ctx), resource.ForDynamic(t.client),
			resource.MustToUnstructured(t.pod), metav1.CreateOptions{}, metav1.DeleteOptions{})
	}

	createAnewSuccess := func(ctx context.Context) *corev1.Pod {
		o, err := createAnew(ctx)
		Expect(err).To(Succeed())
		Expect(o).ToNot(BeNil())

		actual := &corev1.Pod{}
		Expect(scheme.Scheme.Convert(o, actual, nil)).To(Succeed())

		return actual
	}

	createAnewError := func(ctx context.Context) error {
		_, err := createAnew(ctx)
		return err
	}

	When("the resource doesn't exist", func() {
		It("should successfully create the resource", func(ctx SpecContext) {
			t.compareWithPod(createAnewSuccess(ctx))
		})
	})

	When("the resource already exists", func() {
		BeforeEach(func(ctx context.Context) {
			t.createPod(ctx)
		})

		Context("and the new resource spec differs", func() {
			BeforeEach(func() {
				t.pod.Spec.Containers[0].Image = "updated"
			})

			It("should delete the existing resource and create a new one", func(ctx SpecContext) {
				t.compareWithPod(createAnewSuccess(ctx))
			})

			Context("and Delete returns not found", func() {
				BeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods", "delete", apierrors.NewNotFound(schema.GroupResource{}, t.pod.Name), true)
				})

				It("should successfully create the resource", func(ctx SpecContext) {
					t.compareWithPod(createAnewSuccess(ctx))
				})
			})

			Context("and Delete fails", func() {
				BeforeEach(func() {
					t.expectedErr = errors.New("delete failed")
					fake.FailOnAction(t.testingFake, "pods", "delete", t.expectedErr, false)
				})

				It("should return an error", func(ctx SpecContext) {
					Expect(createAnewError(ctx)).To(ContainErrorSubstring(t.expectedErr))
				})
			})

			Context("and deletion doesn't complete in time", func() {
				BeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods", "create", apierrors.NewAlreadyExists(schema.GroupResource{},
						t.pod.Name), false)
				})

				It("should return an error", func(ctx SpecContext) {
					Expect(createAnewError(ctx)).ToNot(Succeed())
				})
			})
		})

		Context("and the new resource spec does not differ", func() {
			BeforeEach(func() {
				t.pod.Status.Phase = corev1.PodRunning
			})

			It("should not recreate it", func(ctx SpecContext) {
				createAnewSuccess(ctx)
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "delete")
			})
		})
	})

	When("Create fails", func() {
		BeforeEach(func() {
			t.expectedErr = errors.New("create failed")
			fake.FailOnAction(t.testingFake, "pods", "create", t.expectedErr, false)
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(createAnewError(ctx)).To(ContainErrorSubstring(t.expectedErr))
		})
	})
})

var _ = Describe("CreateOrUpdate function", func() {
	t := newCreateOrUpdateTestDiver()

	createOrUpdate := func(ctx context.Context, expResult util.OperationResult) error {
		options := util.CreateOrUpdateOptions[*unstructured.Unstructured]{
			Client:         resource.ForDynamic(t.client),
			MutateOnUpdate: t.mutateFn,
		}

		if t.pod.GenerateName != "" {
			options.IdentifyingLabels = map[string]string{}
			maps.Copy(options.IdentifyingLabels, t.pod.Labels)

			t.pod.Labels["new-label"] = "new-value"
		}

		options.Obj = resource.MustToUnstructured(t.pod)

		result, retObj, err := util.CreateOrUpdateWithOptions(t.ctx(ctx), options)
		if err != nil && expResult != util.OperationResultNone {
			return err
		}

		Expect(result).To(Equal(expResult))

		if result != util.OperationResultNone {
			Expect(retObj).NotTo(BeNil())
			Expect(retObj).To(Equal(test.GetResource(ctx, t.client, retObj)))
		}

		return err
	}

	When("the resource doesn't exist", func() {
		It("should successfully create the resource", func(ctx SpecContext) {
			Expect(createOrUpdate(ctx, util.OperationResultCreated)).To(Succeed())
			t.verifyPod(ctx)
			tests.EnsureNoActionsForResource(t.testingFake, "pods/status", "update")
		})

		Context("and a mutation function specified", func() {
			It("should invoke the function on create", func(ctx SpecContext) {
				result, created, err := util.CreateOrUpdateWithOptions(t.ctx(ctx),
					util.CreateOrUpdateOptions[*unstructured.Unstructured]{
						Client: resource.ForDynamic(t.client),
						Obj:    resource.MustToUnstructured(t.pod),
						MutateOnCreate: func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
							existing.SetAnnotations(map[string]string{"on-create-invoked": "true"})
							return existing, nil
						},
					})
				Expect(err).To(Succeed())
				Expect(result).To(Equal(util.OperationResultCreated))

				actual := t.verifyPod(ctx)
				Expect(actual.Annotations).To(HaveKeyWithValue("on-create-invoked", "true"))

				Expect(resource.MustFromUnstructured(created, &corev1.Pod{})).To(Equal(actual))
			})

			Context("which returns an error", func() {
				It("should return an error", func(ctx SpecContext) {
					_, _, err := util.CreateOrUpdateWithOptions(t.ctx(ctx),
						util.CreateOrUpdateOptions[*unstructured.Unstructured]{
							Client: resource.ForDynamic(t.client),
							Obj:    resource.MustToUnstructured(t.pod),
							MutateOnCreate: func(_ *unstructured.Unstructured) (*unstructured.Unstructured, error) {
								return nil, errors.New("mutate failure")
							},
						})
					Expect(err).To(HaveOccurred())
				})
			})
		})

		Context("and GenerateName is set", func() {
			BeforeEach(func() {
				t.pod.Name = ""
				t.pod.GenerateName = "name-prefix-"
				t.pod.Labels = map[string]string{"label1": "value1", "label2": "value2"}
			})

			It("should successfully create the resource", func(ctx SpecContext) {
				Expect(createOrUpdate(ctx, util.OperationResultCreated)).To(Succeed())

				actual := t.verifyPod(ctx)
				Expect(actual.Name).To(HavePrefix("name-prefix-"))
			})
		})

		Context("and the status field is specified", func() {
			BeforeEach(func() {
				t.pod.Status = corev1.PodStatus{Phase: corev1.PodRunning}
			})

			It("should create the resource with the status field set via UpdateStatus", func(ctx SpecContext) {
				Expect(createOrUpdate(ctx, util.OperationResultCreated)).To(Succeed())
				t.verifyPod(ctx)
				tests.EnsureActionsForResource(t.testingFake, "pods/status", "update")
			})

			Context("but the status resource isn't defined", func() {
				BeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods/status", "update", apierrors.NewNotFound(schema.GroupResource{}, ""), false)
				})

				It("should successfully create the resource", func(ctx SpecContext) {
					Expect(createOrUpdate(ctx, util.OperationResultCreated)).To(Succeed())
					t.verifyPod(ctx)
				})
			})

			Context("and UpdateStatus fails", func() {
				JustBeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods/status", "update", apierrors.NewServiceUnavailable("fake"), false)
				})

				It("should return an error", func(ctx SpecContext) {
					Expect(createOrUpdate(ctx, util.OperationResultNone)).ToNot(Succeed())
				})
			})
		})

		Context("and Create fails", func() {
			JustBeforeEach(func() {
				t.expectedErr = apierrors.NewServiceUnavailable("fake")
				fake.FailOnAction(t.testingFake, "pods", "create", t.expectedErr, false)
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(createOrUpdate(ctx, util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
			})
		})

		Context("and Create initially returns AlreadyExists due to a simulated out-of-band create", func() {
			BeforeEach(func(ctx context.Context) {
				t.createPod(ctx)
				t.pod = test.NewPodWithImage("", "apache")
			})

			JustBeforeEach(func(ctx context.Context) {
				fake.FailOnAction(t.testingFake, "pods", "get", apierrors.NewNotFound(schema.GroupResource{},
					t.pod.GetName()), true)
				fake.FailOnAction(t.testingFake, "pods", "create", apierrors.NewAlreadyExists(schema.GroupResource{},
					t.pod.GetName()), true)
			})

			It("should eventually update the resource", func(ctx SpecContext) {
				Expect(createOrUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
				t.verifyPod(ctx)
			})
		})
	})

	t.testUpdate(createOrUpdate)

	t.testGetFailure(createOrUpdate)
})

var _ = Describe("Update function", func() {
	t := newCreateOrUpdateTestDiver()

	update := func(ctx context.Context) error {
		return util.Update(t.ctx(ctx), resource.ForDynamic(t.client), resource.MustToUnstructured(t.pod),
			t.mutateFn)
	}

	When("the resource doesn't exist", func() {
		It("shouldn't return an error", func(ctx SpecContext) {
			Expect(update(ctx)).To(Succeed())
		})
	})

	t.testUpdate(func(ctx context.Context, _ util.OperationResult) error {
		return update(ctx)
	})

	t.testGetFailure(func(ctx context.Context, _ util.OperationResult) error {
		return update(ctx)
	})
})

var _ = Describe("MustUpdate function", func() {
	t := newCreateOrUpdateTestDiver()

	mustUpdate := func(ctx context.Context) error {
		return util.MustUpdate(t.ctx(ctx), resource.ForDynamic(t.client),
			resource.MustToUnstructured(t.pod), t.mutateFn)
	}

	When("the resource doesn't exist", func() {
		It("should return an error", func(ctx SpecContext) {
			Expect(mustUpdate(ctx)).ToNot(Succeed())
		})
	})

	t.testUpdate(func(ctx context.Context, _ util.OperationResult) error {
		return mustUpdate(ctx)
	})

	t.testGetFailure(func(ctx context.Context, _ util.OperationResult) error {
		return mustUpdate(ctx)
	})
})

type createOrUpdateTestDriver struct {
	pod         *corev1.Pod
	testingFake *testing.Fake
	client      dynamic.ResourceInterface
	origBackoff wait.Backoff
	expectedErr error
	mutateFn    util.MutateFn[*unstructured.Unstructured]
}

func newCreateOrUpdateTestDiver() *createOrUpdateTestDriver {
	t := &createOrUpdateTestDriver{}

	BeforeEach(func() {
		dynClient := dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
		t.testingFake = &dynClient.Fake
		fake.AddBasicReactors(t.testingFake)

		t.client = dynClient.Resource(schema.GroupVersionResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Version:  corev1.SchemeGroupVersion.Version,
			Resource: "pods",
		}).Namespace("test")

		t.pod = test.NewPod("")

		t.origBackoff = util.SetBackoff(wait.Backoff{
			Steps:    5,
			Duration: 30 * time.Millisecond,
		})

		t.mutateFn = func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
			obj := resource.MustToUnstructured(t.pod)
			obj.SetUID(resource.MustToMeta(existing).GetUID())

			return util.Replace(obj)(nil)
		}
	})

	AfterEach(func() {
		util.SetBackoff(t.origBackoff)
	})

	return t
}

func (t *createOrUpdateTestDriver) ctx(ctx context.Context) context.Context {
	return logr.NewContext(ctx, logf.Log)
}

func (t *createOrUpdateTestDriver) testGetFailure(doOper func(context.Context, util.OperationResult) error) {
	When("resource retrieval fails", func() {
		JustBeforeEach(func() {
			t.expectedErr = apierrors.NewServiceUnavailable("fake")
			fake.FailOnAction(t.testingFake, "pods", "get", t.expectedErr, false)
		})

		It("should return an error", func(ctx SpecContext) {
			Expect(doOper(ctx, util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
		})
	})
}

func (t *createOrUpdateTestDriver) testUpdate(doUpdate func(context.Context, util.OperationResult) error) {
	When("the resource already exists", func() {
		JustBeforeEach(func(ctx context.Context) {
			labels := t.pod.Labels
			generateName := t.pod.GenerateName
			t.createPod(ctx)

			t.pod = test.NewPodWithImage("", "apache")
			t.pod.GenerateName = generateName
			t.pod.Labels = labels
		})

		It("should update the resource", func(ctx SpecContext) {
			Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
			t.verifyPod(ctx)
			tests.EnsureNoActionsForResource(t.testingFake, "pods/status", "update")
		})

		Context("and GenerateName is set", func() {
			BeforeEach(func() {
				t.pod.Name = ""
				t.pod.GenerateName = "name-prefix"
				t.pod.Labels = map[string]string{"label1": "value1", "label2": "value2"}
			})

			JustBeforeEach(func() {
				t.pod.Name = ""
			})

			It("should update the resource", func(ctx SpecContext) {
				test.CreateResource(ctx, t.client, &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "other-prefix",
						Labels:       t.pod.Labels,
					},
				})

				Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
				t.verifyPod(ctx)
			})

			Context("but more than one matching resources exist", func() {
				It("should return an error", func(ctx SpecContext) {
					test.CreateResource(ctx, t.client, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							GenerateName: t.pod.GenerateName,
							Labels:       t.pod.Labels,
						},
					})
					Expect(doUpdate(ctx, util.OperationResultNone)).ToNot(Succeed())
				})
			})
		})

		Context("and the status is also modified", func() {
			JustBeforeEach(func() {
				t.pod.Status = corev1.PodStatus{Phase: corev1.PodRunning}
			})

			It("should update the resource", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
				t.verifyPod(ctx)
			})

			Context("and UpdateStatus fails", func() {
				JustBeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods/status", "update", apierrors.NewServiceUnavailable("fake"), false)
				})

				It("should return an error", func(ctx SpecContext) {
					Expect(doUpdate(ctx, util.OperationResultNone)).ToNot(Succeed())
				})
			})
		})

		Context("and only the status is modified", func() {
			BeforeEach(func() {
				t.pod.Status = corev1.PodStatus{Phase: corev1.PodPending}
			})

			JustBeforeEach(func(ctx context.Context) {
				t.pod = test.GetResource(ctx, t.client, t.pod)
				t.pod.Status = corev1.PodStatus{Phase: corev1.PodRunning}
			})

			It("should only update the status", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
				Expect(test.GetResource(ctx, t.client, t.pod).Status).To(Equal(t.pod.Status))
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "update")
			})

			Context("and UpdateStatus returns NotFound", func() {
				JustBeforeEach(func() {
					fake.FailOnAction(t.testingFake, "pods/status", "update", apierrors.NewNotFound(schema.GroupResource{}, ""), false)
				})

				It("should update the status", func(ctx SpecContext) {
					Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
					Expect(test.GetResource(ctx, t.client, t.pod).Status).To(Equal(t.pod.Status))
				})
			})
		})

		Context("and the existing resource has a status but the status on update is empty", func() {
			BeforeEach(func() {
				t.pod.Status = corev1.PodStatus{Phase: corev1.PodPending}
			})

			JustBeforeEach(func(ctx context.Context) {
				t.pod = test.GetResource(ctx, t.client, t.pod)
				t.pod.Status = corev1.PodStatus{}
			})

			It("should not update the resource", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultNone)).To(Succeed())
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "update")
				tests.EnsureNoActionsForResource(t.testingFake, "pods/status", "update")
			})
		})

		Context("and Update initially fails due to conflict", func() {
			BeforeEach(func() {
				fake.FailOnAction(t.testingFake, "pods", "update", apierrors.NewConflict(schema.GroupResource{}, "",
					errors.New("conflict")), true)
			})

			It("should eventually update the resource", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultUpdated)).To(Succeed())
				t.verifyPod(ctx)
			})
		})

		Context("and Update fails not due to conflict", func() {
			JustBeforeEach(func() {
				fake.FailOnAction(t.testingFake, "pods", "update", t.expectedErr, false)
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultNone)).To(ContainErrorSubstring(t.expectedErr))
			})
		})

		Context("and the resource to update is the same", func() {
			BeforeEach(func() {
				t.mutateFn = func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
					return existing, nil
				}
			})

			It("should not update the resource", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultNone)).To(Succeed())
				tests.EnsureNoActionsForResource(t.testingFake, "pods", "update")
				tests.EnsureNoActionsForResource(t.testingFake, "pods/status", "update")
			})
		})

		Context("and the mutate function returns an error", func() {
			BeforeEach(func() {
				t.expectedErr = errors.New("mutate failure")
				t.mutateFn = func(_ *unstructured.Unstructured) (*unstructured.Unstructured, error) {
					return nil, t.expectedErr
				}
			})

			It("should return an error", func(ctx SpecContext) {
				Expect(doUpdate(ctx, util.OperationResultNone)).ToNot(Succeed())
			})
		})
	})
}

func (t *createOrUpdateTestDriver) createPod(ctx context.Context) {
	test.CreateResource(ctx, t.client, t.pod)
}

func (t *createOrUpdateTestDriver) verifyPod(ctx context.Context) *corev1.Pod {
	pod := t.pod.DeepCopy()

	if pod.Name == "" {
		list, err := t.client.List(t.ctx(ctx), metav1.ListOptions{})
		Expect(err).To(Succeed())
		Expect(scheme.Scheme.Convert(&list.Items[0], pod, nil)).To(Succeed())
	}

	actual := test.GetResource(ctx, t.client, pod)
	t.compareWithPod(actual)

	return actual
}

func (t *createOrUpdateTestDriver) compareWithPod(actual *corev1.Pod) {
	Expect(actual.Spec).To(Equal(t.pod.Spec))
	Expect(actual.Status).To(Equal(t.pod.Status))
}
