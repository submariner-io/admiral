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
	"container/heap"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/workqueue"
)

var _ = Describe("PriorityQueue", func() {
	var pq *workqueue.PriorityQueue

	BeforeEach(func() {
		pq = workqueue.NewPriorityQueue("test")
	})

	JustBeforeEach(func() {
		heap.Init(pq)
	})

	push := func(i string, p int) {
		pq.SetPriority(i, p)
		heap.Push(pq, i)
	}

	It("should retrieve in priority order", func() {
		count := 10

		for i := range count {
			push(strconv.Itoa(i), i)
		}

		Expect(pq.Len()).To(Equal(count))

		for i := count - 1; i >= 0; i-- {
			item := heap.Pop(pq)
			Expect(item).To(Equal(strconv.Itoa(i)))
		}
	})

	It("should adjust ordering when a priority is changed", func() {
		push("first", 3)
		push("second", 2)
		push("third", 1)

		pq.SetPriority("second", 20)
		pq.SetPriority("second", 20) // should be a no-op
		pq.Adjust("second")
		pq.Adjust("second") // should be a no-op
		Expect(heap.Pop(pq)).To(Equal("second"))
		pq.Adjust("second") // should be a no-op

		pq.SetPriority("third", 10)
		pq.Adjust("third")
		Expect(heap.Pop(pq)).To(Equal("third"))

		push("fourth", 40)
		pq.SetPriority("fourth", 0)
		pq.Adjust("fourth")
		Expect(heap.Pop(pq)).To(Equal("first"))
	})
})
