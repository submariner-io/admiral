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

package workqueue_test

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/workqueue"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/set"
)

var _ = Describe("Work Queue", func() {
	var (
		wq        workqueue.Interface
		processFn workqueue.ProcessFunc
		itemCh    chan string
		itemCount int
		config    *workqueue.Config
	)

	BeforeEach(func() {
		config = nil
		itemCount = 100

		processFn = func(key, name, namespace string) (bool, error) {
			defer GinkgoRecover()

			actualNS, actualName, err := cache.SplitMetaNamespaceKey(key)
			Expect(err).To(Succeed())
			Expect(actualNS).To(Equal(namespace))
			Expect(actualName).To(Equal(name))

			itemCh <- key

			return false, nil
		}
	})

	JustBeforeEach(func() {
		if config != nil {
			wq = workqueue.NewWithConfig("test", *config)
		} else {
			wq = workqueue.New("test")
		}

		itemCh = make(chan string, itemCount)
		wq.Run(processFn)

		DeferCleanup(func() {
			wq.ShutDown()
		})
	})

	It("should notify of enqueued items", func() {
		expKeys := set.Set[string]{}

		for i := 1; i <= 10; i++ {
			k := cache.ObjectName{Namespace: "ns", Name: strconv.Itoa(i)}.String()
			expKeys.Insert(k)
			wq.Enqueue(cache.ExplicitKey(k))
		}

		count := expKeys.Len()
		for i := 1; i <= count; i++ {
			var received string

			Eventually(itemCh).Should(Receive(&received))
			Expect(expKeys.Has(received)).To(BeTrue(), "Received unexpected %q", received)
			expKeys.Delete(received)
		}

		Expect(expKeys.Len()).To(BeZero(), "Did not receive %v", expKeys.UnsortedList())
		Consistently(itemCh).ShouldNot(Receive())
	})

	When("a DeletedFinalStateUnknown object is enqueued", func() {
		It("should notify of the key", func() {
			key := cache.ObjectName{Namespace: "ns", Name: "deleted"}.String()
			wq.Enqueue(cache.DeletedFinalStateUnknown{Key: key})
			Eventually(itemCh).Should(Receive(Equal(key)))
		})
	})

	When("the processing function returns an error", func() {
		var handledError chan error

		BeforeEach(func() {
			savedErrorHandlers := utilruntime.ErrorHandlers
			DeferCleanup(func() {
				utilruntime.ErrorHandlers = savedErrorHandlers
			})

			handledError = make(chan error, 50)

			utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers,
				func(_ context.Context, err error, _ string, _ ...any) {
					handledError <- err
				})

			processFn = func(_, _, _ string) (bool, error) {
				return false, errors.New("processing error")
			}
		})

		It("should log the error", func() {
			key := cache.ObjectName{Name: "foo"}.String()
			wq.Enqueue(cache.ExplicitKey(key))

			Eventually(handledError).Should(Receive())
			Consistently(handledError).ShouldNot(Receive())
		})
	})

	When("a requeue is requested from the processing function", func() {
		BeforeEach(func() {
			var once sync.Once
			processFn = func(key, _, _ string) (bool, error) {
				itemCh <- key

				requeue := false
				once.Do(func() {
					requeue = true
				})

				return requeue, nil
			}
		})

		It("should notify of the item again", func() {
			key := cache.ObjectName{Namespace: "foo", Name: "bar"}.String()
			wq.Enqueue(cache.ExplicitKey(key))

			Eventually(itemCh).Should(Receive(Equal(key)))
			Eventually(itemCh).Should(Receive(Equal(key)))

			Consistently(itemCh).ShouldNot(Receive())
		})
	})

	Context("items enqueued with low priority", func() {
		firstKey := cache.ObjectName{Name: "first"}.String()
		lowKey1 := cache.ObjectName{Name: "lowKey1"}.String()
		lowKey2 := cache.ObjectName{Name: "lowKey2"}.String()
		normalKey1 := cache.ObjectName{Name: "normalKey1"}.String()
		normalKey2 := cache.ObjectName{Name: "normalKey2"}.String()

		BeforeEach(func() {
			c := workqueue.DefaultConfig()
			config = &c
			config.ItemRateLimiterBaseDelay = 0

			processFn = func(key, _, _ string) (bool, error) {
				itemCh <- key

				if key == firstKey {
					wq.EnqueueWithOpts(cache.ExplicitKey(lowKey1), workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
					wq.Enqueue(cache.ExplicitKey(normalKey1))
					wq.EnqueueWithOpts(cache.ExplicitKey(lowKey2), workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
					wq.Enqueue(cache.ExplicitKey(normalKey2))
				}

				return false, nil
			}
		})

		Specify("should be processed after normal priority items", func() {
			wq.Enqueue(cache.ExplicitKey(firstKey))
			Eventually(itemCh).Should(Receive(Equal(firstKey)))

			Eventually(itemCh).Should(Receive(HavePrefix("normal")))
			Eventually(itemCh).Should(Receive(HavePrefix("normal")))
			Eventually(itemCh).Should(Receive(HavePrefix("low")))
			Eventually(itemCh).Should(Receive(HavePrefix("low")))
		})
	})

	When("the priority for an item already in the queue is increased to normal", func() {
		firstKey := cache.ObjectName{Name: "first"}.String()
		adjustedKey := cache.ObjectName{Name: "adjusted"}.String()

		BeforeEach(func() {
			c := workqueue.DefaultConfig()
			config = &c
			config.ItemRateLimiterBaseDelay = 0

			processFn = func(key, _, _ string) (bool, error) {
				itemCh <- key

				if key == firstKey {
					for i := 1; i <= 50; i++ {
						wq.EnqueueWithOpts(cache.ExplicitKey("low"+strconv.Itoa(i)), workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
					}

					wq.EnqueueWithOpts(cache.ExplicitKey(adjustedKey), workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
					// Should cause it to be adjusted to the front of the queue
					wq.Enqueue(cache.ExplicitKey(adjustedKey))
				}

				return false, nil
			}
		})

		It("should adjust its position to the front of the queue", func() {
			wq.Enqueue(cache.ExplicitKey(firstKey))
			Eventually(itemCh).Should(Receive(Equal(firstKey)))

			Eventually(itemCh).Should(Receive(Equal(adjustedKey)))
		})
	})

	Context("", func() {
		BeforeEach(func() {
			itemCount = 1000
			c := workqueue.DefaultConfig()
			config = &c
		})

		JustBeforeEach(func() {
			for i := 1; i <= itemCount; i++ {
				wq.Enqueue(cache.ExplicitKey("item" + strconv.Itoa(i)))
			}

			done := make(chan struct{})

			go func() {
				for i := 1; i <= itemCount; i++ {
					Eventually(itemCh).Should(Receive())
				}

				done <- struct{}{}
			}()

			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
				Fail("Did not receive all the items in time")
			}
		})

		When("the BucketRateLimiterItemsPerSec config is set to 0", func() {
			BeforeEach(func() {
				config.BucketRateLimiterItemsPerSec = 0
			})

			It("should disable the bucket rate limiter", func() {
			})
		})

		When("the BucketRateLimiterMaxBurst config is set to 0", func() {
			BeforeEach(func() {
				config.BucketRateLimiterMaxBurst = 0
			})

			It("should disable the bucket rate limiter", func() {
			})
		})

		Context("with no rate limiter configured", func() {
			BeforeEach(func() {
				config = &workqueue.Config{}
			})

			It("should not affect functionality", func() {
			})
		})
	})

	Context("ShutDownWithDrain", func() {
		var (
			processContinue chan any
			processStart    chan any
			processed       sync.Map
			once            sync.Once
		)

		BeforeEach(func() {
			processContinue = make(chan any)
			processStart = make(chan any)
			processed = sync.Map{}
			once = sync.Once{}

			processFn = func(key, _, _ string) (bool, error) {
				once.Do(func() {
					processStart <- true
					<-processContinue
				})

				processed.Store(key, true)

				// Delay a bit to ensure the majority of the items are processed after ShutDownWithDrain has started.
				time.Sleep(time.Millisecond * 20)

				return false, nil
			}
		})

		It("should process all previously queued items", func(ctx SpecContext) {
			count := 10

			var keys []string
			for i := 1; i <= count; i++ {
				keys = append(keys, strconv.Itoa(i))
			}

			for _, key := range keys {
				wq.EnqueueWithOpts(cache.ExplicitKey(key),
					workqueue.EnqueueOpts{Priority: workqueue.NormalPriority, RateLimited: false})
			}

			Eventually(processStart).Should(Receive())

			processContinue <- true
			Expect(wq.ShutDownWithDrain(ctx)).To(Succeed())

			for _, key := range keys {
				_, ok := processed.Load(key)
				Expect(ok).To(BeTrue(), "%q was not processed", key)
			}
		})

		It("should time out if the current item processing is delayed", func(ctx SpecContext) {
			itemKey := "item"

			wq.EnqueueWithOpts(cache.ExplicitKey(itemKey),
				workqueue.EnqueueOpts{Priority: workqueue.NormalPriority, RateLimited: false})

			Eventually(processStart).Should(Receive())

			timeoutContext, cancel := context.WithTimeout(ctx, time.Millisecond*100)
			defer cancel()

			Expect(wq.ShutDownWithDrain(timeoutContext)).NotTo(Succeed())

			processContinue <- true

			timeoutContext, cancel = context.WithTimeout(ctx, time.Second*3)
			defer cancel()

			Expect(wq.ShutDownWithDrain(timeoutContext)).To(Succeed())

			_, ok := processed.Load(itemKey)
			Expect(ok).To(BeTrue(), "Item was not processed")
		})
	})
})
