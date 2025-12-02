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
	"errors"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func testTransformFunction() {
	t := newTransformFunctionTestDriver()

	When("a resource is created in the datastore", func() {
		JustBeforeEach(func() {
			test.CreateResource(t.sourceClient, t.resource)
		})

		It("should distribute the transformed resource", func() {
			t.verifyDistribute()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(func() int {
				return int(atomic.LoadInt32(&t.invocationCount))
			}).Should(Equal(1))
		})

		Context("and the transform function specifies to re-queue", func() {
			BeforeEach(func() {
				t.requeueOnOp = ptr.To(syncer.Create)
			})

			It("should eventually retry", func() {
				Eventually(func() int {
					return int(atomic.LoadInt32(&t.invocationCount))
				}, 3).Should(BeNumerically(">", 1))
			})
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should distribute the transformed resource", func() {
			t.verifyDistribute()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

			t.resource = test.NewPodWithImage(t.config.SourceNamespace, "updated")
			test.UpdateResource(t.sourceClient, test.NewPodWithImage(t.config.SourceNamespace, "updated"))
			t.federator.VerifyDistribute(t.transformed)
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Update)))
		})
	})

	When("a resource is deleted from the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		JustBeforeEach(func() {
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			atomic.StoreInt32(&t.invocationCount, 0)
		})

		It("should delete the transformed resource", func() {
			Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			t.verifyDelete()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			Consistently(func() int {
				return int(atomic.LoadInt32(&t.invocationCount))
			}).Should(Equal(1))
		})

		Context("and the transform function specifies to re-queue", func() {
			BeforeEach(func() {
				t.requeueOnOp = ptr.To(syncer.Delete)
			})

			It("should eventually retry", func() {
				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Eventually(func() int {
					return int(atomic.LoadInt32(&t.invocationCount))
				}, 3).Should(BeNumerically(">", 1))
			})
		})

		Context("after the create operation is re-queued", func() {
			BeforeEach(func() {
				t.requeueOnOp = ptr.To(syncer.Create)
			})

			It("should not retry the create operation", func() {
				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
				Consistently(t.expOperation).ShouldNot(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("deletion of the transformed resource initially fails", func() {
		BeforeEach(func() {
			t.federator.FailOnDelete(errors.New("fake error"))
			t.addInitialResource(t.resource)
		})

		It("should retry until it succeeds", func() {
			t.verifyDistribute()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			t.verifyDelete()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
		})
	})

	When("distribute for the transformed resource initially fails", func() {
		JustBeforeEach(func() {
			t.federator.FailOnDistribute(errors.New("fake error"))
		})

		It("should retry until it succeeds", func() {
			test.CreateResource(t.sourceClient, t.resource)
			t.verifyDistribute()
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
		})
	})

	When("the transform function returns nil with no re-queue", func() {
		BeforeEach(func() {
			t.config.Transform = func(_ runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
				atomic.AddInt32(&t.invocationCount, 1)
				t.expOperation <- op

				return nil, false
			}
		})

		Context("and a resource is created in the datastore", func() {
			It("should not distribute the resource", func() {
				test.CreateResource(t.sourceClient, t.resource)
				t.federator.VerifyNoDistribute()
				Expect(int(atomic.LoadInt32(&t.invocationCount))).To(Equal(1))
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		Context("and a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				t.addInitialResource(t.resource)
			})

			It("should not delete the resource", func() {
				t.federator.VerifyNoDistribute()
				atomic.StoreInt32(&t.invocationCount, 0)
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				t.federator.VerifyNoDelete()
				Expect(int(atomic.LoadInt32(&t.invocationCount))).To(Equal(1))
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})

	When("the transform function initially returns nil with re-queue", func() {
		var returnNil atomic.Bool

		BeforeEach(func() {
			returnNil.Store(true)

			t.config.Transform = func(_ runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
				t.expOperation <- op

				if returnNil.Load() {
					returnNil.Store(false)
					return nil, true
				}

				return t.transformed, false
			}
		})

		Context("and a resource is created in the datastore", func() {
			It("should eventually distribute the transformed resource", func() {
				test.CreateResource(t.sourceClient, t.resource)
				t.verifyDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		Context("and a resource is deleted in the datastore", func() {
			BeforeEach(func() {
				t.addInitialResource(t.resource)
			})

			It("should eventually delete the resource", func() {
				t.verifyDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
				returnNil.Store(true)

				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				t.verifyDelete()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})
}

type transformFuncTestDriver struct {
	*testDriver
	transformed     *corev1.Pod
	expOperation    chan syncer.Operation
	invocationCount int32
	requeueOnOp     *syncer.Operation
}

func newTransformFunctionTestDriver() *transformFuncTestDriver {
	t := &transformFuncTestDriver{testDriver: newTestDriver(test.LocalNamespace, "", syncer.RemoteToLocal)}

	BeforeEach(func() {
		test.SetClusterIDLabel(t.resource, "remote")
		atomic.StoreInt32(&t.invocationCount, 0)

		t.expOperation = make(chan syncer.Operation, 20)
		t.transformed = test.NewPodWithImage(t.config.SourceNamespace, "transformed")
		t.requeueOnOp = nil

		t.config.Transform = func(from runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
			defer GinkgoRecover()
			atomic.AddInt32(&t.invocationCount, 1)

			pod, ok := from.(*corev1.Pod)
			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", from)
			Expect(equality.Semantic.DeepDerivative(t.resource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, t.resource.Spec)
			t.expOperation <- op

			requeue := false

			if t.requeueOnOp != nil {
				requeue = *t.requeueOnOp == op
			}

			retObj := t.transformed
			if requeue {
				retObj = nil
			}

			return retObj, requeue
		}
	})

	return t
}

func (t *transformFuncTestDriver) verifyDistribute() {
	t.federator.VerifyDistribute(test.SetClusterIDLabel(t.transformed.DeepCopy(), "remote"))
}

func (t *transformFuncTestDriver) verifyDelete() {
	t.federator.VerifyDelete(test.SetClusterIDLabel(t.transformed.DeepCopy(), "remote"))
}
