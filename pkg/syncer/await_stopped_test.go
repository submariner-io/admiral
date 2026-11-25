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
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"k8s.io/apimachinery/pkg/runtime"
)

func testAwaitStopped() {
	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		t.config.Federator = &blockingFederator{
			distributeContinue: make(chan any),
			distributeStarted:  make(chan any),
		}

		t.config.DrainWorkQueueTimeout = time.Millisecond * 80
	})

	It("should time out if the work queue is delayed stopping", func(ctx SpecContext) {
		defer func() {
			t.stopCh = nil
		}()

		test.CreateResource(ctx, t.sourceClient, t.resource)
		Eventually(t.config.Federator.(*blockingFederator).distributeStarted).Should(Receive())

		close(t.stopCh)

		timeoutContext, cancel := context.WithTimeout(ctx, time.Millisecond*300)
		defer cancel()

		Expect(t.syncer.AwaitStopped(timeoutContext)).NotTo(Succeed())

		t.config.Federator.(*blockingFederator).distributeContinue <- true

		timeoutContext, cancel = context.WithTimeout(ctx, time.Second)
		defer cancel()

		Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
		Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
	})

	When("the Federator is blocked on the processing context", func() {
		var federator *contextBlockingFederator

		BeforeEach(func() {
			federator = &contextBlockingFederator{distributeStarted: make(chan any, 1)}
			t.config.Federator = federator
		})

		It("should cancel the context if the work queue is delayed stopping", func(ctx SpecContext) {
			defer func() {
				t.stopCh = nil
			}()

			test.CreateResource(ctx, t.sourceClient, t.resource)
			Eventually(federator.distributeStarted).Should(Receive())

			close(t.stopCh)

			timeoutContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
		})
	})

	When("the Transform function is blocked on the processing context", func() {
		var (
			transformStarted  chan any
			transformContinue chan any
			transformErr      chan error
		)

		BeforeEach(func() {
			transformStarted = make(chan any, 1)
			transformContinue = make(chan any)
			transformErr = make(chan error, 1)

			t.config.Federator = t.federator

			t.config.Transform = func(ctx context.Context, from runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
				transformStarted <- true

				select {
				case <-ctx.Done():
				case <-transformContinue:
				}

				transformErr <- ctx.Err()

				return from, false
			}
		})

		It("should cancel the context if the work queue is delayed stopping", func(ctx SpecContext) {
			defer func() {
				t.stopCh = nil
			}()

			test.CreateResource(ctx, t.sourceClient, t.resource)
			Eventually(transformStarted).Should(Receive())
			Consistently(transformErr).ShouldNot(Receive())

			close(t.stopCh)

			timeoutContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
			Expect(transformErr).To(Receive(MatchError(context.Canceled)))
		})

		Context("and the work queue drains in time", func() {
			BeforeEach(func() {
				t.config.DrainWorkQueueTimeout = time.Second * 5
			})

			It("should not cancel the context", func(ctx SpecContext) {
				defer func() {
					t.stopCh = nil
				}()

				test.CreateResource(ctx, t.sourceClient, t.resource)
				Eventually(transformStarted).Should(Receive())

				close(t.stopCh)
				Consistently(transformErr, 300*time.Millisecond).ShouldNot(Receive())
				close(transformContinue)

				Eventually(transformErr).Should(Receive(Succeed()))

				timeoutContext, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()

				Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
			})
		})
	})

	When("items are still queued after the work queue drain times out", func() {
		var federator *slowFirstFederator

		BeforeEach(func() {
			federator = &slowFirstFederator{distributeStarted: make(chan any, 1), errs: make(chan error, 10)}
			t.config.Federator = federator
		})

		It("should process them with a context that isn't cancelled", func(ctx SpecContext) {
			defer func() {
				t.stopCh = nil
			}()

			test.CreateResource(ctx, t.sourceClient, t.resource)
			Eventually(federator.distributeStarted).Should(Receive())

			queued := t.resource.DeepCopy()
			queued.Name = "queued"
			test.CreateResource(ctx, t.sourceClient, queued)
			Consistently(federator.errs).ShouldNot(Receive())

			close(t.stopCh)

			timeoutContext, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			Expect(t.syncer.AwaitStopped(timeoutContext)).To(Succeed())
			Expect(federator.errs).To(Receive(MatchError(context.Canceled)))
			Expect(federator.errs).To(Receive(Succeed()))
		})
	})
}

type blockingFederator struct {
	distributeContinue chan any
	distributeStarted  chan any
	once               sync.Once
}

func (f *blockingFederator) Distribute(_ context.Context, _ runtime.Object) error {
	f.once.Do(func() {
		f.distributeStarted <- true
		<-f.distributeContinue
	})

	return nil
}

func (f *blockingFederator) Delete(_ context.Context, _ runtime.Object) error {
	return nil
}

type contextBlockingFederator struct {
	distributeStarted chan any
}

func (f *contextBlockingFederator) Distribute(ctx context.Context, _ runtime.Object) error {
	select {
	case f.distributeStarted <- true:
	default:
	}

	<-ctx.Done()

	return ctx.Err()
}

func (f *contextBlockingFederator) Delete(_ context.Context, _ runtime.Object) error {
	return nil
}

// slowFirstFederator blocks the first Distribute call until its context is cancelled and reports the context error of
// each call.
type slowFirstFederator struct {
	distributeStarted chan any
	errs              chan error
	once              sync.Once
}

func (f *slowFirstFederator) Distribute(ctx context.Context, _ runtime.Object) error {
	f.once.Do(func() {
		f.distributeStarted <- true

		<-ctx.Done()
	})

	f.errs <- ctx.Err()

	return nil
}

func (f *slowFirstFederator) Delete(_ context.Context, _ runtime.Object) error {
	return nil
}
