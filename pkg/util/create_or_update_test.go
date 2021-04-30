/*
© 2021 Red Hat, Inc.

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
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("", func() {
	var (
		pod         *corev1.Pod
		client      *fake.DynamicResourceClient
		origBackoff wait.Backoff
		expectedErr error
	)

	BeforeEach(func() {
		dynClient := fake.NewDynamicClient(scheme.Scheme)

		client = dynClient.Resource(schema.GroupVersionResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Version:  corev1.SchemeGroupVersion.Version,
			Resource: "pods",
		}).Namespace("test").(*fake.DynamicResourceClient)

		client.CheckResourceVersionOnUpdate = true

		pod = test.NewPod("")

		origBackoff = util.SetBackoff(wait.Backoff{
			Steps:    5,
			Duration: 30 * time.Millisecond,
		})
	})

	AfterEach(func() {
		util.SetBackoff(origBackoff)
	})

	createAnew := func() error {
		return util.CreateAnew(context.TODO(), resource.ForDynamic(client), pod, metav1.CreateOptions{}, metav1.DeleteOptions{})
	}

	Describe("CreateAnew function", func() {
		When("no existing resource exists", func() {
			It("should successfully create the resource", func() {
				Expect(createAnew()).To(Succeed())
				verifyPod(client, pod)
			})
		})

		When("an existing resource exists", func() {
			var existing *corev1.Pod

			BeforeEach(func() {
				test.CreateResource(client, pod)
				existing = test.GetPod(client, pod)
			})

			It("should delete the existing resource and create a new one", func() {
				Expect(createAnew()).To(Succeed())
				verifyPod(client, existing)
			})

			Context("and Delete returns not found", func() {
				BeforeEach(func() {
					client.FailOnDelete = apierrors.NewNotFound(schema.GroupResource{}, pod.Name)
				})

				It("should successfully create the resource", func() {
					Expect(createAnew()).To(Succeed())
					verifyPod(client, existing)
				})
			})

			Context("and Delete fails", func() {
				BeforeEach(func() {
					client.FailOnDelete = errors.New("delete failed")
					expectedErr = client.FailOnDelete
				})

				It("should return an error", func() {
					Expect(createAnew()).To(ContainErrorSubstring(expectedErr))
				})
			})

			Context("and deletion doesn't complete in time", func() {
				BeforeEach(func() {
					client.PersistentFailOnCreate.Store(apierrors.NewAlreadyExists(schema.GroupResource{}, pod.Name))
				})

				It("should return an error", func() {
					Expect(createAnew()).ToNot(Succeed())
				})
			})
		})

		When("Create fails", func() {
			BeforeEach(func() {
				client.FailOnCreate = errors.New("create failed")
				expectedErr = client.FailOnCreate
			})

			It("should return an error", func() {
				Expect(createAnew()).To(ContainErrorSubstring(expectedErr))
			})
		})
	})

	Describe("CreateOrUpdate function", func() {
		var mutateFn util.MutateFn

		BeforeEach(func() {
			mutateFn = func(existing runtime.Object) (runtime.Object, error) {
				obj := test.ToUnstructured(pod)
				obj.SetUID(resource.ToMeta(existing).GetUID())
				return util.Replace(obj)(nil)
			}
		})

		createOrUpdate := func() (util.OperationResult, error) {
			return util.CreateOrUpdate(context.TODO(), resource.ForDynamic(client), test.ToUnstructured(pod), mutateFn)
		}

		testCreateOrUpdateErr := func() {
			result, err := createOrUpdate()
			Expect(err).To(ContainErrorSubstring(expectedErr))
			Expect(result).To(Equal(util.OperationResultNone))
		}

		When("no existing resource exists", func() {
			It("should successfully create the resource", func() {
				Expect(createOrUpdate()).To(Equal(util.OperationResultCreated))
				verifyPod(client, pod)
			})

			Context("and Create fails", func() {
				JustBeforeEach(func() {
					client.FailOnCreate = apierrors.NewServiceUnavailable("fake")
					expectedErr = client.FailOnCreate
				})

				It("should return an error", func() {
					testCreateOrUpdateErr()
				})
			})

			Context("and Create initially returns AlreadyExists due to a simulated out-of-band create", func() {
				BeforeEach(func() {
					test.CreateResource(client, pod)
					pod = test.NewPodWithImage("", "apache")
				})

				JustBeforeEach(func() {
					client.FailOnGet = apierrors.NewNotFound(schema.GroupResource{}, pod.GetName())
					client.FailOnCreate = apierrors.NewAlreadyExists(schema.GroupResource{}, pod.GetName())
				})

				It("should eventually update the resource", func() {
					Expect(createOrUpdate()).To(Equal(util.OperationResultUpdated))
					verifyPod(client, pod)
				})
			})
		})

		When("an existing resource exists", func() {
			BeforeEach(func() {
				test.CreateResource(client, pod)
				pod = test.NewPodWithImage("", "apache")
			})

			It("should update the resource", func() {
				Expect(createOrUpdate()).To(Equal(util.OperationResultUpdated))
				verifyPod(client, pod)
			})

			Context("and Update initially fails due to conflict", func() {
				BeforeEach(func() {
					client.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("conflict"))
				})

				It("should eventually update the resource", func() {
					Expect(createOrUpdate()).To(Equal(util.OperationResultUpdated))
					verifyPod(client, pod)
				})
			})

			Context("and Update fails not due to conflict", func() {
				JustBeforeEach(func() {
					client.FailOnUpdate = apierrors.NewServiceUnavailable("fake")
					expectedErr = client.FailOnUpdate
				})

				It("should return an error", func() {
					testCreateOrUpdateErr()
				})
			})

			Context("and the resource to update is the same", func() {
				BeforeEach(func() {
					mutateFn = func(existing runtime.Object) (runtime.Object, error) {
						return existing, nil
					}
				})

				It("should not update the resource", func() {
					result, err := createOrUpdate()
					Expect(err).To(Succeed())
					Expect(result).To(Equal(util.OperationResultNone))
				})
			})

			Context("and the mutate function returns an error", func() {
				BeforeEach(func() {
					expectedErr = errors.New("mutate failure")
					mutateFn = func(existing runtime.Object) (runtime.Object, error) {
						return nil, expectedErr
					}
				})

				It("should return an error", func() {
					testCreateOrUpdateErr()
				})
			})
		})

		When("resource retrieval fails", func() {
			JustBeforeEach(func() {
				client.FailOnGet = apierrors.NewServiceUnavailable("fake")
				expectedErr = client.FailOnGet
			})

			It("should return an error", func() {
				testCreateOrUpdateErr()
			})
		})
	})
})

func verifyPod(client dynamic.ResourceInterface, expected *corev1.Pod) {
	actual := test.GetPod(client, expected)
	Expect(actual.GetUID()).NotTo(Equal(expected.GetUID()))
	Expect(actual.Spec).To(Equal(expected.Spec))
}
