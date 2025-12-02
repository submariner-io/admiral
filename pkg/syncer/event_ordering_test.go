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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func testEventOrdering() {
	t := newEventOrderingTestDriver()

	When("a create occurs immediately following a delete", func() {
		It("should process both events in order", func() {
			first := test.CreateResource(t.sourceClient, t.resource)
			t.federator.VerifyDistribute(first)
			Eventually(t.opChan).Should(Receive(Equal(syncer.Create)))

			t.resource = test.NewPodWithImage(t.config.SourceNamespace, "apache")
			Expect(t.sourceClient.Delete(ctx, first.GetName(), metav1.DeleteOptions{})).To(Succeed())
			second := test.CreateResource(t.sourceClient, t.resource)

			Eventually(t.opChan).Should(Receive(Equal(syncer.Delete)))
			Eventually(t.opChan).Should(Receive(Equal(syncer.Create)))
			Consistently(t.opChan).ShouldNot(Receive())

			t.federator.VerifyDelete(first)
			t.federator.VerifyDistribute(second)
		})
	})

	When("a delete occurs immediately following a create", func() {
		It("should process both events in order", func() {
			r := test.CreateResource(t.sourceClient, t.resource)
			Expect(t.sourceClient.Delete(ctx, r.GetName(), metav1.DeleteOptions{})).To(Succeed())

			Eventually(t.opChan).Should(Receive(Equal(syncer.Create)))
			Eventually(t.opChan).Should(Receive(Equal(syncer.Delete)))
			Consistently(t.opChan).ShouldNot(Receive())

			t.federator.VerifyDelete(r)
			t.federator.VerifyDistribute(r)
		})
	})

	When("a delete occurs immediately following an update", func() {
		It("should process both events in order", func() {
			t.federator.VerifyDistribute(test.CreateResource(t.sourceClient, t.resource))
			Eventually(t.opChan).Should(Receive(Equal(syncer.Create)))

			t.federator.VerifyDistribute(test.UpdateResource(t.sourceClient,
				test.NewPodWithImage(t.config.SourceNamespace, "apache")))
			Expect(t.sourceClient.Delete(ctx, t.resource.GetName(), metav1.DeleteOptions{})).To(Succeed())

			Eventually(t.opChan).Should(Receive(Equal(syncer.Update)))
			Eventually(t.opChan).Should(Receive(Equal(syncer.Delete)))
			Consistently(t.opChan).ShouldNot(Receive())
		})
	})
}

type eventOrderingTestDriver struct {
	*testDriver
	opChan chan syncer.Operation
}

func newEventOrderingTestDriver() *eventOrderingTestDriver {
	t := &eventOrderingTestDriver{testDriver: newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)}

	BeforeEach(func() {
		t.opChan = make(chan syncer.Operation, 20)

		t.config.Transform = func(from runtime.Object, _ int, op syncer.Operation) (runtime.Object, bool) {
			t.opChan <- op
			return from, false
		}
	})

	return t
}
