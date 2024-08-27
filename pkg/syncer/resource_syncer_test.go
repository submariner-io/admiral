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
package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	fakereactor "github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/federate/fake"
	. "github.com/submariner-io/admiral/pkg/gomega"
	resourceutils "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"
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
	Describe("ListResourcesBySelector", testListResourcesBySelector)
	Describe("RequeueResource", testRequeueResource)
	Describe("Reconcile", func() {
		Context(fmt.Sprintf("Direction: %s", syncer.LocalToRemote), testReconcileLocalToRemote)
		Context(fmt.Sprintf("Direction: %s", syncer.RemoteToLocal), testReconcileRemoteToLocal)
		Context(fmt.Sprintf("Direction: %s", syncer.None), testReconcileNoDirection)
	})
	Describe("Trim Resource Fields", testTrimResourceFields)
	Describe("With SharedInformer", testWithSharedInformer)
	Describe("With missing namespace", testWithMissingNamespace)
	Describe("Event Ordering", testEventOrdering)
})

func testReconcileLocalToRemote() {
	d := newTestDriver(metav1.NamespaceAll, "local", syncer.LocalToRemote)

	BeforeEach(func() {
		test.SetClusterIDLabel(d.resource, "local")
		d.resource.Labels[syncer.OrigNamespaceLabelKey] = test.LocalNamespace
	})

	JustBeforeEach(func() {
		d.resource.Namespace = test.RemoteNamespace
		obj := d.resource.DeepCopyObject()
		d.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile is local and does not exist", func() {
		It("should invoke delete", func() {
			d.resource.Namespace = test.LocalNamespace
			d.federator.VerifyDelete(test.SetClusterIDLabel(d.resource, ""))
		})
	})

	When("the resource to reconcile is local and does exist", func() {
		BeforeEach(func() {
			d.resource.Namespace = test.LocalNamespace
			d.addInitialResource(d.resource)
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})

	When("the resource to reconcile is not local", func() {
		BeforeEach(func() {
			test.SetClusterIDLabel(d.resource, "remote")
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})
}

func testReconcileRemoteToLocal() {
	d := newTestDriver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	BeforeEach(func() {
		d.resource.Namespace = test.RemoteNamespace
		test.SetClusterIDLabel(d.resource, "remote")
	})

	JustBeforeEach(func() {
		obj := d.resource.DeepCopyObject()
		d.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile is remote and does not exist", func() {
		It("should invoke delete", func() {
			d.federator.VerifyDelete(d.resource)
		})
	})

	When("the resource to reconcile is remote and does exist", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})

	When("the resource to reconcile is not remote", func() {
		BeforeEach(func() {
			test.SetClusterIDLabel(d.resource, "local")
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})
}

func testReconcileNoDirection() {
	d := newTestDriver(test.LocalNamespace, "local", syncer.None)

	var toReconcile runtime.Object

	BeforeEach(func() {
		toReconcile = d.resource
		d.resource.Namespace = test.LocalNamespace
	})

	JustBeforeEach(func() {
		obj := toReconcile.DeepCopyObject()

		d.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile does not exist", func() {
		It("should invoke delete", func() {
			d.federator.VerifyDelete(d.resource)
		})
	})

	When("the resource to reconcile does exist", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})

	When("the originating namespace of the resource to reconcile cannot be determined", func() {
		BeforeEach(func() {
			d.resource.Namespace = ""
		})

		It("should not invoke delete", func() {
			d.federator.VerifyNoDelete()
		})
	})

	When("the type of the resource to reconcile is invalid", func() {
		BeforeEach(func() {
			toReconcile = &corev1.Namespace{}
		})

		It("should ignore it", func() {
			d.federator.VerifyNoDelete()
		})
	})
}

func testLocalToRemote() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.config.SyncCounterOpts = &prometheus.GaugeOpts{
			Namespace: "ns",
			Name:      "test",
		}
	})

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
	d := newTestDriver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	BeforeEach(func() {
		d.config.SyncCounter = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{},
			[]string{
				syncer.DirectionLabel,
				syncer.OperationLabel,
				syncer.SyncerNameLabel,
			},
		)
	})

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
	d := newTestDriver(test.RemoteNamespace, "", syncer.RemoteToLocal)

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
	d := newTestDriver(test.LocalNamespace, "", syncer.RemoteToLocal)
	ctx := context.TODO()

	var transformed *corev1.Pod
	var expOperation chan syncer.Operation
	var invocationCount int32
	var requeue bool

	BeforeEach(func() {
		test.SetClusterIDLabel(d.resource, "remote")
		atomic.StoreInt32(&invocationCount, 0)

		expOperation = make(chan syncer.Operation, 20)
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
		requeue = false

		d.config.Transform = func(from runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
			defer GinkgoRecover()
			atomic.AddInt32(&invocationCount, 1)

			pod, ok := from.(*corev1.Pod)
			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", from)
			Expect(equality.Semantic.DeepDerivative(d.resource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, d.resource.Spec)
			expOperation <- op

			return transformed, requeue
		}
	})

	verifyDistribute := func() {
		d.federator.VerifyDistribute(test.SetClusterIDLabel(transformed.DeepCopy(), "remote"))
	}

	verifyDelete := func() {
		d.federator.VerifyDelete(test.SetClusterIDLabel(transformed.DeepCopy(), "remote"))
	}

	When("a resource is created in the datastore", func() {
		JustBeforeEach(func() {
			test.CreateResource(d.sourceClient, d.resource)
		})

		It("should distribute the transformed resource", func() {
			verifyDistribute()
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(func() int {
				return int(atomic.LoadInt32(&invocationCount))
			}).Should(Equal(1))
		})

		Context("and the transform function specifies to re-queue", func() {
			BeforeEach(func() {
				requeue = true
			})

			It("should eventually retry", func() {
				Eventually(func() int {
					return int(atomic.LoadInt32(&invocationCount))
				}, 3).Should(BeNumerically(">", 1))
			})
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		It("should distribute the transformed resource", func() {
			verifyDistribute()
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			d.resource = test.NewPodWithImage(d.config.SourceNamespace, "updated")
			test.UpdateResource(d.sourceClient, test.NewPodWithImage(d.config.SourceNamespace, "updated"))
			d.federator.VerifyDistribute(transformed)
			Eventually(expOperation).Should(Receive(Equal(syncer.Update)))
		})
	})

	When("a resource is deleted from the datastore", func() {
		BeforeEach(func() {
			d.addInitialResource(d.resource)
		})

		JustBeforeEach(func() {
			verifyDistribute()
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			atomic.StoreInt32(&invocationCount, 0)
		})

		It("should delete the transformed resource", func() {
			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			verifyDelete()
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			Consistently(func() int {
				return int(atomic.LoadInt32(&invocationCount))
			}).Should(Equal(1))
		})

		Context("and the transform function specifies to re-queue", func() {
			JustBeforeEach(func() {
				requeue = true
			})

			It("should eventually retry", func() {
				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Eventually(func() int {
					return int(atomic.LoadInt32(&invocationCount))
				}, 3).Should(BeNumerically(">", 1))
			})
		})
	})

	When("deletion of the transformed resource initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete(errors.New("fake error"))
			d.addInitialResource(d.resource)
		})

		It("should retry until it succeeds", func() {
			verifyDistribute()
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			verifyDelete()
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
		})
	})

	When("distribute for the transformed resource initially fails", func() {
		JustBeforeEach(func() {
			d.federator.FailOnDistribute(errors.New("fake error"))
		})

		It("retry until it succeeds", func() {
			test.CreateResource(d.sourceClient, d.resource)
			verifyDistribute()
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
		})
	})

	When("the transform function returns nil with no re-queue", func() {
		BeforeEach(func() {
			d.config.Transform = func(_ runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
				atomic.AddInt32(&invocationCount, 1)
				expOperation <- op

				return nil, false
			}
		})

		Context("and a resource is created in the datastore", func() {
			It("should not distribute the resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyNoDistribute()
				Expect(int(atomic.LoadInt32(&invocationCount))).To(Equal(1))
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		Context("and a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				d.addInitialResource(d.resource)
			})

			It("should not delete the resource", func() {
				d.federator.VerifyNoDistribute()
				atomic.StoreInt32(&invocationCount, 0)
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				d.federator.VerifyNoDelete()
				Expect(int(atomic.LoadInt32(&invocationCount))).To(Equal(1))
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

			d.config.Transform = func(_ runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
				var ret runtime.Object

				v := transformFuncRet.Load()
				if v != nilResource {
					ret, _ = v.(runtime.Object)
				}

				transformFuncRet.Store(transformed)
				expOperation <- op

				return ret, ret == nil
			}
		})

		Context("and a resource is created in the datastore", func() {
			It("should eventually distribute the transformed resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				verifyDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		Context("and a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				d.addInitialResource(d.resource)
			})

			It("should eventually delete the resource", func() {
				verifyDistribute()
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
				transformFuncRet.Store(nilResource)

				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				verifyDelete()
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})
}

func testOnSuccessfulSyncFunction() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var (
		expResource            *corev1.Pod
		expOperation           chan syncer.Operation
		onSuccessfulSyncReturn atomic.Bool
	)

	BeforeEach(func() {
		expOperation = make(chan syncer.Operation, 20)
		expResource = d.resource

		onSuccessfulSyncReturn.Store(false)

		d.config.OnSuccessfulSync = func(synced runtime.Object, op syncer.Operation) bool {
			defer GinkgoRecover()

			pod, ok := synced.(*corev1.Pod)

			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", synced)
			Expect(equality.Semantic.DeepDerivative(expResource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, expResource.Spec)
			expOperation <- op

			retry := onSuccessfulSyncReturn.Load()
			onSuccessfulSyncReturn.Store(false)

			return retry
		}
	})

	When("a resource is successfully created in the datastore", func() {
		It("should invoke the OnSuccessfulSync function", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(expOperation).ShouldNot(Receive())
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
			Consistently(expOperation).ShouldNot(Receive())
		})
	})

	Context("with transform function", func() {
		When("a resource is successfully created in the datastore", func() {
			BeforeEach(func() {
				expResource = test.NewPodWithImage(d.config.SourceNamespace, "transformed")
				d.config.Transform = func(_ runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
					return expResource, false
				}
			})

			It("should invoke the OnSuccessfulSync function with the transformed resource", func() {
				test.CreateResource(d.sourceClient, d.resource)
				d.federator.VerifyDistribute(expResource)
				Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("distribute fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDistribute(errors.New("fake error"))
			d.federator.ResetOnFailure.Store(false)
		})

		It("should not invoke the OnSuccessfulSync function", func() {
			test.CreateResource(d.sourceClient, d.resource)
			Consistently(expOperation, 300*time.Millisecond).ShouldNot(Receive())
		})
	})

	When("delete fails", func() {
		JustBeforeEach(func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
		})

		Context("with a general error", func() {
			BeforeEach(func() {
				d.federator.FailOnDelete(errors.New("fake error"))
				d.federator.ResetOnFailure.Store(false)
			})

			It("should not invoke the OnSuccessfulSync function", func() {
				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Consistently(expOperation, 300*time.Millisecond).ShouldNot(Receive())
			})
		})

		Context("with a NotFound error", func() {
			BeforeEach(func() {
				d.federator.FailOnDelete(apierrors.NewNotFound(schema.GroupResource{}, ""))
			})

			It("should invoke the OnSuccessfulSync function", func() {
				Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})

	When("the OnSuccessfulSync function returns true", func() {
		BeforeEach(func() {
			onSuccessfulSyncReturn.Store(true)
		})

		It("should retry", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(expOperation).ShouldNot(Receive())

			onSuccessfulSyncReturn.Store(true)
			Expect(d.sourceClient.Delete(ctx, d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			Eventually(expOperation).Should(Receive(Equal(syncer.Delete)))
			Consistently(expOperation).ShouldNot(Receive())
		})
	})
}

func testShouldProcessFunction() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)
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
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)
	ctx := context.TODO()

	var expectedErr error

	BeforeEach(func() {
		expectedErr = errors.New("fake error")
	})

	When("distribute initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDistribute(expectedErr)
		})

		It("should log the error and retry until it succeeds", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(d.handledError, 5).Should(Receive(ContainErrorSubstring(expectedErr)))
		})
	})

	When("delete initially fails", func() {
		BeforeEach(func() {
			d.federator.FailOnDelete(expectedErr)
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
			d.federator.FailOnDelete(apierrors.NewNotFound(schema.GroupResource{}, "not found"))
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
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.addInitialResource(d.resource)
	})

	JustBeforeEach(func() {
		d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
		test.UpdateResource(d.sourceClient, d.resource)
	})

	When("no equivalence function is specified", func() {
		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			})
		})

		Context("and the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t := metav1.Now()
				d.resource.ObjectMeta.DeletionTimestamp = &t
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			})
		})
	})

	When("the default equivalence function is specified", func() {
		BeforeEach(func() {
			d.config.ResourcesEquivalent = syncer.DefaultResourcesEquivalent
		})

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
			})
		})

		Context("and the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t := metav1.Now()
				d.resource.ObjectMeta.DeletionTimestamp = &t
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
			})
		})

		Context("and the resource's Labels are updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.SetLabels(map[string]string{"new-label": "value"})
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			})
		})

		Context("and the resource's Annotations are updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.SetAnnotations(map[string]string{"new-annotations": "value"})
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
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

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			})
		})
	})

	When("the Spec equivalence function is specified ", func() {
		BeforeEach(func() {
			d.config.ResourcesEquivalent = syncer.AreSpecsEquivalent
		})

		Context("and the resource's Spec is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Spec.Hostname = "newHost"
			})

			It("should distribute it", func() {
				d.federator.VerifyDistribute(test.GetResource(d.sourceClient, d.resource))
			})
		})

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				d.resource.Status.Phase = corev1.PodRunning
			})

			It("should not distribute it", func() {
				d.federator.VerifyNoDistribute()
			})
		})
	})
}

func testGetResource() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

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
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var resource2 *corev1.Pod

	BeforeEach(func() {
		resource2 = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "apache-pod",
				Namespace: d.resource.Namespace,
				Labels:    map[string]string{"foo": "bar"},
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
		list := d.syncer.ListResources()
		assertResourceList(list, d.resource, resource2)
	})
}

func testListResourcesBySelector() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	newPod := func(i int, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("pod%d", i),
				Namespace: fmt.Sprintf("namespace%d", i),
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Image: fmt.Sprintf("image%d", i),
						Name:  fmt.Sprintf("container%d", i),
					},
				},
			},
		}
	}

	var (
		pod1 = newPod(1, map[string]string{
			"foo":    "bar",
			"label1": "match",
			"label2": "no-match",
		})

		pod2 = newPod(2, map[string]string{
			"label1": "match",
		})

		pod3 = newPod(3, map[string]string{
			"label1": "no-match",
			"label2": "match",
		})

		pod4 = newPod(4, map[string]string{
			"label1": "no-match",
		})

		pod5 = newPod(5, map[string]string{
			"label1": "match",
			"label2": "match",
		})
	)

	BeforeEach(func() {
		d.addInitialResource(d.resource)
		d.addInitialResource(pod1)
		d.addInitialResource(pod2)
		d.addInitialResource(pod3)
		d.addInitialResource(pod4)
		d.addInitialResource(pod5)
	})

	It("should return correct resources for label1 selector", func() {
		list := d.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label1": "match"}).AsSelector())
		assertResourceList(list, pod1, pod2, pod5)
	})

	It("should return correct resources for label2 selector", func() {
		list := d.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label2": "match"}).AsSelector())
		assertResourceList(list, pod3, pod5)
	})

	It("should return correct resources for label1 and label2 selector", func() {
		list := d.syncer.ListResourcesBySelector(labels.Set(map[string]string{
			"label1": "match",
			"label2": "match",
		}).AsSelector())
		assertResourceList(list, pod5)
	})

	It("should return no resources for label3 selector", func() {
		list := d.syncer.ListResourcesBySelector(labels.Set(map[string]string{"label3": "match"}).AsSelector())
		assertResourceList(list)
	})
}

func testRequeueResource() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var transformed *corev1.Pod

	BeforeEach(func() {
		transformed = test.NewPodWithImage(d.config.SourceNamespace, "transformed")

		d.config.Transform = func(_ runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
			return transformed, false
		}
	})

	When("the requested resource exists", func() {
		JustBeforeEach(func() {
			test.CreateResource(d.sourceClient, d.resource)
		})

		It("should requeue it", func() {
			d.federator.VerifyDistribute(transformed)

			d.syncer.RequeueResource(d.resource.Name, d.resource.Namespace)

			d.federator.VerifyDistribute(transformed)
		})
	})

	When("the requested resource does not exist", func() {
		It("should not requeue it", func() {
			d.syncer.RequeueResource(d.resource.Name, d.resource.Namespace)
			d.federator.VerifyNoDistribute()
		})
	})
}

func testTrimResourceFields() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.config.ResyncPeriod = time.Millisecond * 100

		d.resource.SetManagedFields([]metav1.ManagedFieldsEntry{
			{
				Manager:    "kubectl",
				Operation:  metav1.ManagedFieldsOperationApply,
				APIVersion: "v1",
				Time:       ptr.To(metav1.Now()),
				FieldsType: "FieldsV1",
				FieldsV1:   ptr.To(metav1.FieldsV1{}),
			},
		})

		d.addInitialResource(d.resource)
	})

	It("should remove ManagedFields from created resources", func() {
		obj, exists, err := d.syncer.GetResource(d.resource.Name, d.resource.Namespace)
		Expect(err).To(Succeed())
		Expect(exists).To(BeTrue())
		Expect(resourceutils.MustToMeta(obj).GetManagedFields()).Should(BeNil())

		// Sleep a little so a re-sync occurs and doesn't cause a data race.
		time.Sleep(200)
	})
}

func testWithSharedInformer() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		d.useSharedInformer = true
	})

	When("a resource is created in the local datastore", func() {
		d.verifyDistributeOnCreateTest("")
	})
}

func testWithMissingNamespace() {
	const (
		transformedNamespace = "transformed-ns"
		noTransform          = "no-transform"
	)

	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	namespaceClient := func() dynamic.ResourceInterface {
		return d.config.SourceClient.Resource(corev1.SchemeGroupVersion.WithResource("namespaces")).Namespace(metav1.NamespaceNone)
	}

	createNamespace := func(name string) {
		test.CreateResource(namespaceClient(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		})
	}

	BeforeEach(func() {
		d.config.Transform = func(obj runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
			if resourceutils.MustToMeta(obj).GetName() == noTransform {
				return nil, false
			} else if resourceutils.MustToMeta(obj).GetNamespace() == test.LocalNamespace {
				obj = obj.DeepCopyObject()
				resourceutils.MustToMeta(obj).SetNamespace(transformedNamespace)
			}

			return obj, false
		}

		d.config.NamespaceInformer = cache.NewSharedInformer(&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return namespaceClient().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return namespaceClient().Watch(context.TODO(), options)
			},
		}, resourceutils.MustToUnstructured(&corev1.Namespace{}), 0)
	})

	JustBeforeEach(func() {
		d.federator.SetDelegator(federate.NewCreateFederator(d.config.SourceClient, d.config.RestMapper, transformedNamespace))

		createNamespace(test.LocalNamespace)

		fakereactor.AddVerifyNamespaceReactor(&d.config.SourceClient.(*fakeClient.FakeDynamicClient).Fake, "pods")

		if d.config.NamespaceInformer != nil {
			go d.config.NamespaceInformer.Run(d.stopCh)
		}
	})

	Specify("distribute should eventually succeed when the namespace is created", func() {
		resource := test.CreateResource(d.sourceClient, d.resource)
		d.federator.VerifyNoDistribute()

		By("Creating namespace")

		createNamespace(transformedNamespace)

		resource.SetNamespace(transformedNamespace)
		d.federator.VerifyDistribute(resource)
	})

	Context("and no namespace informer specified", func() {
		BeforeEach(func() {
			d.config.NamespaceInformer = nil
		})

		It("should not retry", func() {
			test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyNoDistribute()
		})
	})

	Context("after a namespace is created and distribute succeeds", func() {
		const otherNS = "other-ns"

		JustBeforeEach(func() {
			createNamespace(transformedNamespace)
			createNamespace(otherNS)
		})

		It("should eventually redistribute when the namespace is recreated", func() {
			resource := test.CreateResource(d.sourceClient, d.resource)
			resource.SetNamespace(transformedNamespace)
			d.federator.VerifyDistribute(resource)

			other := d.resource.DeepCopy()
			other.Name = noTransform
			test.CreateResource(d.sourceClient, other)

			other = d.resource.DeepCopy()
			other.Namespace = otherNS
			test.CreateResource(d.config.SourceClient.Resource(
				*test.GetGroupVersionResourceFor(d.config.RestMapper, other)).Namespace(otherNS), other)

			By("Deleting namespace")

			err := namespaceClient().Delete(context.TODO(), transformedNamespace, metav1.DeleteOptions{})
			Expect(err).To(Succeed())

			By("Recreating namespace")

			createNamespace(transformedNamespace)
			d.federator.VerifyDistribute(resource)
		})
	})
}

func testEventOrdering() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	var opChan chan syncer.Operation

	BeforeEach(func() {
		opChan = make(chan syncer.Operation, 20)

		d.config.Transform = func(from runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
			opChan <- op
			return from, false
		}
	})

	When("a create occurs immediately following a delete", func() {
		It("should process both events in order", func() {
			first := test.CreateResource(d.sourceClient, d.resource)
			d.federator.VerifyDistribute(first)
			Eventually(opChan).Should(Receive(Equal(syncer.Create)))

			d.resource = test.NewPodWithImage(d.config.SourceNamespace, "apache")
			Expect(d.sourceClient.Delete(context.TODO(), first.GetName(), metav1.DeleteOptions{})).To(Succeed())
			second := test.CreateResource(d.sourceClient, d.resource)

			Eventually(opChan).Should(Receive(Equal(syncer.Delete)))
			Eventually(opChan).Should(Receive(Equal(syncer.Create)))
			Consistently(opChan).ShouldNot(Receive())

			d.federator.VerifyDelete(first)
			d.federator.VerifyDistribute(second)
		})
	})

	When("a delete occurs immediately following a create", func() {
		It("should process both events in order", func() {
			r := test.CreateResource(d.sourceClient, d.resource)
			Expect(d.sourceClient.Delete(context.TODO(), r.GetName(), metav1.DeleteOptions{})).To(Succeed())

			Eventually(opChan).Should(Receive(Equal(syncer.Create)))
			Eventually(opChan).Should(Receive(Equal(syncer.Delete)))
			Consistently(opChan).ShouldNot(Receive())

			d.federator.VerifyDelete(r)
			d.federator.VerifyDistribute(r)
		})
	})

	When("a delete occurs immediately following an update", func() {
		It("should process both events in order", func() {
			d.federator.VerifyDistribute(test.CreateResource(d.sourceClient, d.resource))
			Eventually(opChan).Should(Receive(Equal(syncer.Create)))

			d.federator.VerifyDistribute(test.UpdateResource(d.sourceClient,
				test.NewPodWithImage(d.config.SourceNamespace, "apache")))
			Expect(d.sourceClient.Delete(context.TODO(), d.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())

			Eventually(opChan).Should(Receive(Equal(syncer.Update)))
			Eventually(opChan).Should(Receive(Equal(syncer.Delete)))
			Consistently(opChan).ShouldNot(Receive())
		})
	})
}

func assertResourceList(actual []runtime.Object, expected ...*corev1.Pod) {
	expSpecs := map[string]*corev1.PodSpec{}
	for i := range expected {
		expSpecs[expected[i].Name] = &expected[i].Spec
	}

	Expect(actual).To(HaveLen(len(expSpecs)))

	for _, obj := range actual {
		meta, err := metaapi.Accessor(obj)
		Expect(err).To(Succeed())
		Expect(obj).To(BeAssignableToTypeOf(&corev1.Pod{}))
		Expect(&obj.(*corev1.Pod).Spec).To(Equal(expSpecs[meta.GetName()]))
		delete(expSpecs, meta.GetName())
	}
}

type testDriver struct {
	config             syncer.ResourceSyncerConfig
	useSharedInformer  bool
	syncer             syncer.Interface
	sourceClient       dynamic.ResourceInterface
	federator          *fake.Federator
	initialResources   []runtime.Object
	stopCh             chan struct{}
	resource           *corev1.Pod
	savedErrorHandlers []utilruntime.ErrorHandler
	handledError       chan error
}

func newTestDriver(sourceNamespace, localClusterID string, syncDirection syncer.SyncDirection) *testDriver {
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
		d.config.ResyncPeriod = 0
		d.useSharedInformer = false

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

		if d.useSharedInformer {
			var sharedInformer cache.SharedInformer

			sharedInformer, err = syncer.NewSharedInformer(&d.config)
			Expect(err).To(Succeed())

			d.syncer, err = syncer.NewResourceSyncerWithSharedInformer(&d.config, sharedInformer)

			go func() {
				sharedInformer.Run(d.stopCh)
			}()
		} else {
			d.syncer, err = syncer.NewResourceSyncer(&d.config)
		}

		Expect(err).To(Succeed())

		utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers,
			func(_ context.Context, err error, _ string, _ ...interface{}) {
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
	t.initialResources = append(t.initialResources, resourceutils.MustToUnstructured(obj))
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
