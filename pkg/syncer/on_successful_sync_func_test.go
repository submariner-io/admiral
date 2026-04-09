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
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func testOnSuccessfulSyncFunction() {
	t := newOnSuccessfulSyncFuncTestDriver()

	When("a resource is successfully created in the datastore", func() {
		It("should invoke the OnSuccessfulSync function", func(ctx context.Context) {
			t.federator.VerifyDistribute(test.CreateResource(ctx, t.sourceClient, t.resource))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(t.expOperation).ShouldNot(Receive())
		})
	})

	When("a resource is successfully updated in the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should invoke the OnSuccessfulSync function", func(ctx context.Context) {
			t.federator.VerifyDistribute(test.GetResource(ctx, t.sourceClient, t.resource))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

			t.expResource = test.NewPodWithImage(t.config.SourceNamespace, "apache")
			t.federator.VerifyDistribute(test.UpdateResource(ctx, t.sourceClient, t.expResource))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Update)))
		})
	})

	When("a resource is successfully deleted from the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should invoke the OnSuccessfulSync function", func(ctx SpecContext) {
			expected := test.GetResource(ctx, t.sourceClient, t.resource)
			t.federator.VerifyDistribute(expected)
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

			Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			t.federator.VerifyDelete(expected)
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			Consistently(t.expOperation).ShouldNot(Receive())
		})
	})

	Context("with transform function", func() {
		When("a resource is successfully created in the datastore", func() {
			BeforeEach(func() {
				t.expResource = test.NewPodWithImage(t.config.SourceNamespace, "transformed")
				t.config.Transform = func(_ runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
					return t.expResource, false
				}
			})

			It("should invoke the OnSuccessfulSync function with the transformed resource", func(ctx context.Context) {
				test.CreateResource(ctx, t.sourceClient, t.resource)
				t.federator.VerifyDistribute(t.expResource)
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("distribute fails", func() {
		BeforeEach(func() {
			t.federator.FailOnDistribute(errors.New("fake error"))
			t.federator.ResetOnFailure.Store(false)
		})

		It("should not invoke the OnSuccessfulSync function", func(ctx context.Context) {
			test.CreateResource(ctx, t.sourceClient, t.resource)
			Consistently(t.expOperation, 300*time.Millisecond).ShouldNot(Receive())
		})
	})

	When("delete fails", func() {
		JustBeforeEach(func(ctx context.Context) {
			t.federator.VerifyDistribute(test.CreateResource(ctx, t.sourceClient, t.resource))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
		})

		Context("with a general error", func() {
			BeforeEach(func() {
				t.federator.FailOnDelete(errors.New("fake error"))
				t.federator.ResetOnFailure.Store(false)
			})

			It("should not invoke the OnSuccessfulSync function", func(ctx SpecContext) {
				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Consistently(t.expOperation, 300*time.Millisecond).ShouldNot(Receive())
			})
		})

		Context("with a NotFound error", func() {
			BeforeEach(func() {
				t.federator.FailOnDelete(apierrors.NewNotFound(schema.GroupResource{}, ""))
			})

			It("should invoke the OnSuccessfulSync function", func(ctx SpecContext) {
				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})

	When("the OnSuccessfulSync function returns true", func() {
		BeforeEach(func(ctx context.Context) {
			t.onSuccessfulSyncReturn.Store(true)
		})

		It("should retry", func(ctx context.Context) {
			t.federator.VerifyDistribute(test.CreateResource(ctx, t.sourceClient, t.resource))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			Consistently(t.expOperation).ShouldNot(Receive())

			t.onSuccessfulSyncReturn.Store(true)
			Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			Consistently(t.expOperation).ShouldNot(Receive())
		})
	})
}

type onSuccessfulSyncFuncTestDriver struct {
	*testDriver
	expResource            *corev1.Pod
	expOperation           chan syncer.Operation
	onSuccessfulSyncReturn atomic.Bool
}

func newOnSuccessfulSyncFuncTestDriver() *onSuccessfulSyncFuncTestDriver {
	t := &onSuccessfulSyncFuncTestDriver{testDriver: newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)}

	BeforeEach(func(ctx context.Context) {
		t.expOperation = make(chan syncer.Operation, 20)
		t.expResource = t.resource

		t.onSuccessfulSyncReturn.Store(false)

		t.config.OnSuccessfulSync = func(synced runtime.Object, op syncer.Operation) bool {
			defer GinkgoRecover()

			pod, ok := synced.(*corev1.Pod)

			Expect(ok).To(BeTrue(), "Expected a Pod object: %#v", synced)
			Expect(equality.Semantic.DeepDerivative(t.expResource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, t.expResource.Spec)
			t.expOperation <- op

			retry := t.onSuccessfulSyncReturn.Load()
			t.onSuccessfulSyncReturn.Store(false)

			return retry
		}
	})

	return t
}
