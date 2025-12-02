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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func testShouldProcessFunction() {
	t := newShouldProcessFuncTestDriver()

	When("a resource is created in the datastore", func() {
		When("the ShouldProcess function returns true", func() {
			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.CreateResource(t.sourceClient, t.resource))
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				t.shouldProcess = false
			})

			It("should not distribute it", func() {
				test.CreateResource(t.sourceClient, t.resource)
				t.federator.VerifyNoDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))
			})
		})
	})

	When("a resource is updated in the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		When("the ShouldProcess function returns true", func() {
			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

				t.expResource = test.NewPodWithImage(t.config.SourceNamespace, "apache")
				t.federator.VerifyDistribute(test.UpdateResource(t.sourceClient, t.expResource))
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Update)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				t.shouldProcess = false
			})

			It("should not distribute it", func() {
				t.federator.VerifyNoDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

				t.expResource = test.NewPodWithImage(t.config.SourceNamespace, "apache")
				test.UpdateResource(t.sourceClient, t.expResource)
				t.federator.VerifyNoDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Update)))
			})
		})
	})

	When("a resource is deleted from the datastore", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		When("the ShouldProcess function returns true", func() {
			It("should delete it", func() {
				expected := test.GetResource(t.sourceClient, t.resource)
				t.federator.VerifyDistribute(expected)
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				t.federator.VerifyDelete(expected)
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})

		When("the ShouldProcess function returns false", func() {
			BeforeEach(func() {
				t.shouldProcess = false
			})

			It("should not delete it", func() {
				t.federator.VerifyNoDistribute()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Create)))

				Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				t.federator.VerifyNoDelete()
				Eventually(t.expOperation).Should(Receive(Equal(syncer.Delete)))
			})
		})
	})
}

type shouldProcessFuncTestDriver struct {
	*testDriver
	expResource   *corev1.Pod
	expOperation  chan syncer.Operation
	shouldProcess bool
}

func newShouldProcessFuncTestDriver() *shouldProcessFuncTestDriver {
	t := &shouldProcessFuncTestDriver{testDriver: newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)}

	BeforeEach(func() {
		t.expOperation = make(chan syncer.Operation, 20)
		t.expResource = t.resource
		t.shouldProcess = true
		t.config.ShouldProcess = func(obj *unstructured.Unstructured, op syncer.Operation) bool {
			defer GinkgoRecover()

			pod := &corev1.Pod{}

			Expect(t.config.Scheme.Convert(obj, pod, nil)).To(Succeed())
			Expect(equality.Semantic.DeepDerivative(t.expResource.Spec, pod.Spec)).To(BeTrue(),
				"Expected:\n%#v\n to be equivalent to: \n%#v", pod.Spec, t.expResource.Spec)
			t.expOperation <- op

			return t.shouldProcess
		}
	})

	return t
}
