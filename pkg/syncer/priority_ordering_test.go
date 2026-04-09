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
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/workqueue"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

//nolint:gocognit // Ignore cognitive complexity for test cases
func testPriorityOrdering() {
	d := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		wqc := workqueue.DefaultConfig()
		wqc.ItemRateLimiterBaseDelay = 0
		d.config.WorkQueueConfig = &wqc

		for i := 1; i <= 100; i++ {
			d.addInitialResource(&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      strconv.Itoa(i),
					Namespace: d.config.SourceNamespace,
				},
			})
		}
	})

	Context("resources updated", func() {
		const numToUpdate = 5

		var (
			updatedResourcesQueuedCh chan []string
			firstTransform           atomic.Bool
			transformedCh            chan string
		)

		BeforeEach(func(ctx context.Context) {
			firstTransform.Store(true)

			transformedCh = make(chan string, 500)

			var (
				initialResourcesQueued atomic.Value
				updatedResourcesQueued atomic.Value
			)

			initialResourcesQueued.Store([]string{})
			updatedResourcesQueued.Store([]string{})

			initialResourcesQueuedCh := make(chan []string, 1)
			updatedResourcesQueuedCh = make(chan []string, 1)
			allUpdatedResourcesAreQueuedCh := make(chan chan struct{})

			d.config.ShouldProcess = func(obj *unstructured.Unstructured, op syncer.Operation) bool {
				if op == syncer.Create {
					q := append(initialResourcesQueued.Load().([]string), obj.GetName())
					initialResourcesQueued.Store(q)

					if len(q) == len(d.initialResources) {
						initialResourcesQueuedCh <- q
					}
				} else if op == syncer.Update {
					q := updatedResourcesQueued.Load().([]string)
					if len(q) == numToUpdate-1 {
						close(allUpdatedResourcesAreQueuedCh)
						updatedResourcesQueuedCh <- q
					} else {
						updatedResourcesQueued.Store(append(q, obj.GetName()))
					}
				}

				return true
			}

			d.config.Transform = func(from runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
				defer GinkgoRecover()

				if firstTransform.CompareAndSwap(true, false) {
					// We're processing the first initial resource. Block until all the initial resources have been
					// enqueued (via ShouldProcess which is called prior to enqueueing) before updating any resources.
					// This ensures that the updated resources should be dequeued and processed next.
					var initialQueue []string

					Eventually(initialResourcesQueuedCh).Should(Receive(&initialQueue))

					// Choose a few resources from the queue to update. We want to ensure we choose ones that won't
					// be at the front of the queue anyway. Logically we should pick from the back of the queue but
					// the priority queue algorithm will swap items of equal priority between the two ends so choose
					// resources from the middle.
					for i := range numToUpdate {
						name := initialQueue[len(initialQueue)/2+i]

						test.UpdateResource(ctx, d.sourceClient, &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      name,
								Namespace: d.config.SourceNamespace,
								Labels:    map[string]string{"foo": "bar"},
							},
						})
					}

					// Block until all the updated resources have been queued.
					Eventually(allUpdatedResourcesAreQueuedCh).Should(BeClosed())
				} else {
					transformedCh <- resource.MustToMeta(from).GetName()
				}

				return from, false
			}
		})

		Specify("should be prioritized before the initial resources", func() {
			var updatedQueue []string

			Eventually(updatedResourcesQueuedCh).WithTimeout(time.Second * 5).Should(Receive(&updatedQueue))

			for range updatedQueue {
				var name string

				Eventually(transformedCh).Should(Receive(&name))
				Expect(updatedQueue).To(ContainElement(name))
			}
		})
	})

	Context("new resources created", func() {
		var (
			initialResourcesCount atomic.Int32
			newResourcesCount     atomic.Int32
			firstTransform        atomic.Bool
			transformedCh         chan string
		)

		allInitialResourcesAreQueuedCh := make(chan struct{})
		allNewResourcesAreQueuedCh := make(chan chan struct{})

		BeforeEach(func(ctx context.Context) {
			firstTransform.Store(true)

			transformedCh = make(chan string, 500)

			d.config.ShouldProcess = func(obj *unstructured.Unstructured, _ syncer.Operation) bool {
				if strings.HasPrefix(obj.GetName(), "new") {
					if int(newResourcesCount.Add(1)) == 2 {
						close(allNewResourcesAreQueuedCh)
					}
				} else if int(initialResourcesCount.Add(1)) == len(d.initialResources) {
					close(allInitialResourcesAreQueuedCh)
				}

				return true
			}

			d.config.Transform = func(from runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
				defer GinkgoRecover()

				if firstTransform.CompareAndSwap(true, false) {
					// We're processing the first initial resource. Block until all the initial resources have been
					// enqueued (via ShouldProcess which is called prior to enqueueing) before creating the new
					// resources. This ensures that the newly created resources should be dequeued and processed next.
					Eventually(allInitialResourcesAreQueuedCh).Should(BeClosed())

					test.CreateResource(ctx, d.sourceClient, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "new1",
							Namespace: d.config.SourceNamespace,
						},
					})

					test.CreateResource(ctx, d.sourceClient, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "new2",
							Namespace: d.config.SourceNamespace,
						},
					})

					// Block until all the newly created resources have been queued.
					Eventually(allNewResourcesAreQueuedCh).Should(BeClosed())
				} else {
					transformedCh <- resource.MustToMeta(from).GetName()
				}

				return from, false
			}
		})

		Specify("should be prioritized before the initial resources", func() {
			Eventually(allNewResourcesAreQueuedCh).Should(BeClosed())

			var actualName string

			Eventually(transformedCh).Should(Receive(&actualName))
			Expect(actualName).To(HavePrefix("new"))
		})
	})
}
