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
package syncer_test

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
)

var _ = Describe("Resource Syncer", func() {
	Describe("Local -> Remote", testLocalToRemote)

	Describe("Remote -> Local", func() {
		Context("with a local cluster ID", testRemoteToLocalWithLocalClusterID)
		Context("without a local cluster ID", testRemoteToLocalWithoutLocalClusterID)
	})

	Describe("With Transform Function", testTransformFunction)
	Describe("With OnSuccessfulSync Function", testOnSuccessfulSyncFunction)
	Describe("With ShouldProcess Function", testShouldProcessFunction)
	Describe("Sync Errors", testSyncErrors)
	Describe("Update Suppression", testUpdateSuppression)
	Describe("GetResource", testGetResource)
	Describe("ListResources", testListResources)
})

func testLocalToRemote() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	When("a resource without a cluster ID label is created in the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		d.verifyDistributeOnDeleteTest("")
	})

	When("a resource with a cluster ID label is created in the local datastore", func() {
		d.verifyNoDistributeOnCreateTest("remote")
	})

	When("a resource with a cluster ID label is updated in the local datastore", func() {
		d.verifyNoDistributeOnUpdateTest("remote")
	})

	When("a resource with a cluster ID label is deleted from the local datastore", func() {
		d.verifyNoDistributeOnDeleteTest("remote")
	})
}

func testRemoteToLocalWithLocalClusterID() {
	d := newTestDiver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	When("a resource with a non-local cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource with a non-local cluster ID label is updated in the remote datastore", func() {
		d.verifyDistributeOnUpdateTest("remote")
	})

	When("a resource with a non-local cluster ID label is deleted from the remote datastore", func() {
		d.verifyDistributeOnDeleteTest("remote")
	})

	When("a resource with a local cluster ID label is created in the remote datastore", func() {
		d.verifyNoDistributeOnCreateTest(d.config.LocalClusterID)
	})

	When("a resource with a local cluster ID label is updated in the remote datastore", func() {
		d.verifyNoDistributeOnUpdateTest(d.config.LocalClusterID)
	})

	When("a resource with a local cluster ID label is deleted from the remote datastore", func() {
		d.verifyNoDistributeOnDeleteTest(d.config.LocalClusterID)
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		d.verifyNoDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the remote datastore", func() {
		d.verifyNoDistributeOnUpdateTest("")
	})

	When("a resource without a local cluster ID label is deleted from the remote datastore", func() {
		d.verifyNoDistributeOnDeleteTest("")
	})
}

func testRemoteToLocalWithoutLocalClusterID() {
	d := newTestDiver(test.RemoteNamespace, "", syncer.RemoteToLocal)

	When("a resource with a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource with a cluster ID label is updated in the remote datastore", func() {
		d.verifyDistributeOnUpdateTest("remote")
	})

	When("a resource with a cluster ID label is deleted from the remote datastore", func() {
		d.verifyDistributeOnCreateTest("remote")
	})

	When("a resource without a cluster ID label is created in the remote datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})

	When("a resource without a cluster ID label is updated in the local datastore", func() {
		d.verifyDistributeOnUpdateTest("")
	})

	When("a resource without a cluster ID label is deleted from the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})
}

func testTransformFunction() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var transformed *corev1.Pod
	var expOperation chan syncer.Operation

	BeforeEach(func() {
		expOperation = make(chan syncer.Operation, 20)
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
		d.config.Transform = func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
			defer GinkgoRecover()
			pod, ok := from.(*corev1.Pod)
			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", from)
			Expect(equality.Semantic.DeepDerivative(d.resource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, d.resource.Spec)
			expOperation <- op
			return transformed, false
		}
	})

	When("a resource is created in the datastore", func() {
		It("should distribute the transformed resource", func() {
			test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should distribute the transformed resource", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			d.resource = test.NewPodWithImage(d.config.SourceNamespace, "updated")
			test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "updated"))
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Update)))
		})
	})

	When("a resource is deleted from the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should delete the transformed resource", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			d.federator.VerifyDelete(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
		})
	})

	When("deletion of the transformed resource initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = errors.New("fake error")
			d.addInitialResource(d.resource)
		})

		It("should retry until it succeeds", func() {
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			d.federator.VerifyDelete(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
		})
	})

	When("distribute for the transformed resource initially fails", func() {
		JustBeforeEach(func() {
			d.federator.FailOnDistribute = errors.New("fake error")
		})

		It("retry until it succeeds", func() {
			test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(test.ToUnstructured(transformed))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
		})
	})

	When("the transform function returns nil with no re-queue", func() {
		var count int32

		BeforeEach(func() {
			atomic.StoreInt32(&count, 0)
			d.config.Transform = func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
				atomic.AddInt32(&count, 1)
				expOperation <- op
				return nil, false
			}
		})

		When("a resource is created in the datastore", func() {
			It("should not distribute the resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyNoDistribute()
				Expect(int(atomic.LoadInt32(&count))).To(Equal(1))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		When("a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				d.addInitialResource(d.resource)
			})

			It("should not delete the resource", func() {
				d.federator.VerifyNoDistribute()
				atomic.StoreInt32(&count, 0)
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				d.federator.VerifyNoDelete()
				Expect(int(atomic.LoadInt32(&count))).To(Equal(1))
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})

	When("the transform function initially returns nil with re-queue", func() {
		nilResource := &corev1.Pod{}
		var transformFuncRet *atomic.Value

		BeforeEach(func() {
			transformFuncRet = &atomic.Value{}
			transformFuncRet.Store(nilResource)
			d.config.Transform = func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
				var ret runtime.Object
				v := transformFuncRet.Load()
				if v != nilResource {
					ret = v.(runtime.Object)
				}

				transformFuncRet.Store(transformed)
				expOperation <- op
				return ret, true
			}
		})

		When("a resource is created in the datastore", func() {
			It("should eventually distribute the transformed resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyDistribute(test.ToUnstructured(transformed))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		When("a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				d.addInitialResource(d.resource)
			})

			It("should eventually delete the resource", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(transformed))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
				transformFuncRet.Store(nilResource)

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				d.federator.VerifyDelete(test.ToUnstructured(transformed))
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})
}

func testOnSuccessfulSyncFunction() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var expResource *corev1.Pod
	var expOperation chan syncer.Operation

	BeforeEach(func() {
		expOperation = make(chan syncer.Operation, 20)
		expResource = d.resource
		d.config.OnSuccessfulSync = func(synced runtime.Object, op syncer.Operation) {
			defer GinkgoRecover()
			pod, ok := synced.(*corev1.Pod)
			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", synced)
			Expect(equality.Semantic.DeepDerivative(expResource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, expResource.Spec)
			expOperation <- op
		}
	})

	When("a resource is successfully created in the datastore", func() {
		It("should invoke the OnSuccessfulSync function", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
		})
	})

	When("a resource is successfully updated in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should invoke the OnSuccessfulSync function", func() {
			d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			expResource = test.NewPodWithImage(d.config.SourceNamespace, "apache")
			d.federator.VerifyDistribute(test.UpdateResource(d.sourceClient, expResource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Update)))
		})
	})

	When("a resource is successfully deleted from the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should invoke the OnSuccessfulSync function", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			d.federator.VerifyDelete(expected)
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
		})
	})

	Context("with transform function", func() {
		When("a resource is successfully created in the datastore", func() {
			BeforeEach(func() {
				expResource = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
				d.config.Transform = func(from runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
					return expResource, false
				}
			})

			It("should invoke the OnSuccessfulSync function with the transformed resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyDistribute(test.ToUnstructured(expResource))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("distribute fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDistribute = errors.New("fake error")
			d.federator.ResetOnFailure = false
		})

		It("should not invoke the OnSuccessfulSync function", func() {
			test.CreateResource(d.sourceClient, d.resource)
			Consistently(expOperation, 300*time.Millisecond).ShouldNot(Receive())
		})
	})

	When("delete fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = errors.New("fake error")
			d.federator.ResetOnFailure = false
		})

		It("should not invoke the OnSuccessfulSync function", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Consistently(expOperation, 300*time.Millisecond).ShouldNot(Receive())
		})
	})
}

func testShouldProcessFunction() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var (
		expResource   *corev1.Pod
		expOperation  chan syncer.Operation
		shouldProcess bool
	)

	BeforeEach(func() {
		expOperation = make(chan syncer.Operation, 20)
		expResource = d.resource
		shouldProcess = true
		d.config.ShouldProcess = func(obj *unstructured.Unstructured, op syncer.Operation) bool {
			defer GinkgoRecover()
			pod := &corev1.Pod{}
			Expect(d.config.Scheme.Convert(obj, pod, nil)).To(Succeed())
			Expect(equality.Semantic.DeepDerivative(expResource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, expResource.Spec)
			expOperation <- op
			return shouldProcess
		}
	})

	When("a resource is created in the datastore", func() {
		When("the ShouldProcess function returns true", func() {
			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				shouldProcess = false
			})

			It("should not distribute it", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyNoDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		When("the ShouldProcess function returns true", func() {
			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				expResource = test.NewPodWithImage(d.config.SourceNamespace, "apache")
				d.federator.VerifyDistribute(test.UpdateResource(d.sourceClient, expResource))
				Eventually(expOperation).Should(Receive(Equal(syncer.Update)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				shouldProcess = false
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				expResource = test.NewPodWithImage(d.config.SourceNamespace, "apache")
				test.UpdateResource(d.sourceClient, expResource)
				d.federator.VerifyNoDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Update)))
			})
		})
	})

	When("a resource is deleted from the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		When("the ShouldProcess function returns true", func() {
			It("should delete it", func() {
				expected := test.GetResource(d.sourceClient, d.resource)
				d.federator.VerifyDistribute(expected)
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				d.federator.VerifyDelete(expected)
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				shouldProcess = false
			})

			It("should not delete it", func() {
				d.federator.VerifyNoDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				d.federator.VerifyNoDelete()
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})
}

func testSyncErrors() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var expectedErr error

	BeforeEach(func() {
		expectedErr = errors.New("fake error")
	})

	When("distribute initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDistribute = expectedErr
		})

		It("should log the error and retry until it succeeds", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(d.handledError, 5).Should(Receive(ContainErrorSubstring(expectedErr)))
		})
	})

	When("delete initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = expectedErr
			d.addInitialResource(d.resource)
		})

		It("should log the error and retry until it succeeds", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Eventually(d.handledError, 5).Should(Receive(ContainErrorSubstring(expectedErr)))
			d.federator.VerifyDelete(expected)
		})
	})

	When("delete fails with not found", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete = apierrors.NewNotFound(schema.GroupResource{}, "not found")
			d.addInitialResource(d.resource)
		})

		It("should not log the error nor retry", func() {
			expected := test.GetResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(expected)

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Consistently(d.handledError, 300*time.Millisecond).ShouldNot(Receive(), "Error was unexpectedly logged")
		})
	})
}

func testUpdateSuppression() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.addInitialResource(d.resource)
	})

	JustBeforeEach(func() {
		d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
		test.UpdateResource(d.sourceClient, d.resource)
	})

	When("no equivalence function is specified", func() {
		When("the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
			})
		})

		When("the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t := metav1.Now()
				d.resource.ObjectMeta.DeletionTimestamp = &t
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
			})
		})
	})

	When("the default equivalence function is specified", func() {
		BeforeEach(func() {
			d.config.ResourcesEquivalent = syncer.DefaultResourcesEquivalent
		})

		When("the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
			})
		})

		When("the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t := metav1.Now()
				d.resource.ObjectMeta.DeletionTimestamp = &t
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
			})
		})

		When("the resource's Labels are updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.SetLabels(map[string]string{"new-label": "value"})
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
			})
		})

		When("the resource's Annotations are updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.SetAnnotations(map[string]string{"new-annotations": "value"})
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
			})
		})
	})

	When("a custom equivalence function is specified that compares Status", func() {
		BeforeEach(func() {
			d.config.ResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
				return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "status"),
					util.GetNestedField(obj2, "status"))
			}
		})

		When("the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.ToUnstructured(d.resource))
			})
		})
	})
}

func testGetResource() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	When("the requested resource exists", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should return the resource", func() {
			obj, exists, err := d.syncer.GetResource(d.resource.Name, d.resource.Namespace)
			Expect(err).To(Succeed())
			Expect(exists).To(BeTrue())

			pod, ok := obj.(*corev1.Pod)
			Expect(ok).To(BeTrue())
			Expect(pod.Name).To(Equal(d.resource.Name))
			Expect(pod.Spec).To(Equal(d.resource.Spec))
		})
	})

	When("the requested resource does not exist", func() {
		It("should return false", func() {
			_, exists, err := d.syncer.GetResource(d.resource.Name, d.resource.Namespace)
			Expect(err).To(Succeed())
			Expect(exists).To(BeFalse())
		})
	})
}

func testListResources() {
	d := newTestDiver(test.LocalNamespace, "", syncer.LocalToRemote)

	var resource2 *corev1.Pod

	BeforeEach(func() {
		resource2 = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "apache-pod",
				Namespace: d.resource.Namespace,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: "apache",
						Name:  "httpd",
					},
				},
			},
		}

		d.addInitialResource(d.resource)
		d.addInitialResource(resource2)
	})

	It("should return all the resources", func() {
		list, err := d.syncer.ListResources()
		Expect(err).To(Succeed())

		expected := map[string]*corev1.PodSpec{d.resource.Name: &d.resource.Spec, resource2.Name: &resource2.Spec}

		Expect(list).To(HaveLen(len(expected)))
		for _, actual := range list {
			meta, err := metaapi.Accessor(actual)
			Expect(err).To(Succeed())
			Expect(actual).To(BeAssignableToTypeOf(&corev1.Pod{}))
			Expect(&actual.(*corev1.Pod).Spec).To(Equal(expected[meta.GetName()]))
			delete(expected, meta.GetName())
		}
	})
}

type testDriver struct {
	config             syncer.ResourceSyncerConfig
	syncer             syncer.Interface
	sourceClient       dynamic.ResourceInterface
	federator          *fake.Federator
	initialResources   []runtime.Object
	stopCh             chan struct{}
	resource           *corev1.Pod
	savedErrorHandlers []func(error)
	handledError       chan error
}

func newTestDiver(sourceNamespace, localClusterID string, syncDirection syncer.SyncDirection) *testDriver {
	resourceType := &corev1.Pod{}
	d := &testDriver{
		config: syncer.ResourceSyncerConfig{
			Name:            "test",
			SourceNamespace: sourceNamespace,
			LocalClusterID:  localClusterID,
			ResourceType:    resourceType,
			Direction:       syncDirection,
		},
	}

	BeforeEach(func() {
		d.federator = fake.New()
		d.config.Federator = d.federator
		d.initialResources = nil
		d.resource = test.NewPod(sourceNamespace)
		d.stopCh = make(chan struct{})
		d.savedErrorHandlers = utilruntime.ErrorHandlers
		d.handledError = make(chan error, 1000)
		d.config.Scheme = runtime.NewScheme()
		d.config.Transform = nil
		d.config.OnSuccessfulSync = nil
		d.config.ResourcesEquivalent = nil

		err := corev1.AddToScheme(d.config.Scheme)
		Expect(err).To(Succeed())
	})

	JustBeforeEach(func() {
		initObjs := test.PrepInitialClientObjs(d.config.SourceNamespace, "", d.initialResources...)

		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(d.config.ResourceType)

		d.config.RestMapper = restMapper
		d.config.SourceClient = fakeClient.NewSimpleDynamicClient(d.config.Scheme, initObjs...)

		d.sourceClient = d.config.SourceClient.Resource(*gvr).Namespace(d.config.SourceNamespace)

		var err error
		d.syncer, err = syncer.NewResourceSyncer(&d.config)
		Expect(err).To(Succeed())

		utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
			d.handledError <- err
		})

		Expect(d.syncer.Start(d.stopCh)).To(Succeed())
	})

	JustAfterEach(func() {
		close(d.stopCh)
		d.syncer.AwaitStopped()
		utilruntime.ErrorHandlers = d.savedErrorHandlers
	})

	return d
}

func (t *testDriver) addInitialResource(obj runtime.Object) {
	t.initialResources = append(t.initialResources, test.ToUnstructured(obj))
}

func (t *testDriver) verifyDistributeOnCreateTest(clusterID string) {
	It("should distribute it", func() {
		t.federator.VerifyDistribute(test.CreateResource(t.sourceClient, test.SetClusterIDLabel(t.resource, clusterID)))
	})
}

func (t *testDriver) verifyNoDistributeOnCreateTest(clusterID string) {
	It("should not distribute it", func() {
		test.CreateResource(t.sourceClient, test.SetClusterIDLabel(t.resource, clusterID))
		t.federator.VerifyNoDistribute()
	})
}

func (t *testDriver) verifyDistributeOnUpdateTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should distribute it", func() {
		t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
		t.federator.VerifyDistribute(test.UpdateResource(t.sourceClient, test.SetClusterIDLabel(
			test.NewPodWithImage(t.config.SourceNamespace, "apache"), clusterID)))
	})
}

func (t *testDriver) verifyNoDistributeOnUpdateTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should not distribute it", func() {
		t.federator.VerifyNoDistribute()

		test.UpdateResource(t.sourceClient, test.SetClusterIDLabel(
			test.NewPodWithImage(t.config.SourceNamespace, "apache"), clusterID))
		t.federator.VerifyNoDistribute()
	})
}

func (t *testDriver) verifyDistributeOnDeleteTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should delete it", func() {
		expected := test.GetResource(t.sourceClient, t.resource)
		t.federator.VerifyDistribute(expected)

		Expect(t.sourceClient.Delete(context.TODO(), t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
		t.federator.VerifyDelete(expected)
	})
}

func (t *testDriver) verifyNoDistributeOnDeleteTest(clusterID string) {
	BeforeEach(func() {
		t.addInitialResource(test.SetClusterIDLabel(t.resource, clusterID))
	})

	It("should not delete it", func() {
		t.federator.VerifyNoDistribute()

		Expect(t.sourceClient.Delete(context.TODO(), t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
		t.federator.VerifyNoDelete()
	})
}
