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

	It("should time out if the work queue is delayed stopping", func() {
		defer func() {
			t.stopCh = nil
		}()

		test.CreateResource(t.sourceClient, t.resource)
		Eventually(t.config.Federator.(*blockingFederator).distributeStarted).Should(Receive())

		close(t.stopCh)

		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*300)
		defer cancel()

		Expect(t.syncer.AwaitStopped(ctx)).NotTo(Succeed())

		t.config.Federator.(*blockingFederator).distributeContinue <- true

		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		Expect(t.syncer.AwaitStopped(ctx)).To(Succeed())
		Expect(t.syncer.AwaitStopped(ctx)).To(Succeed())
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
