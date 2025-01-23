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

package workqueue

import (
	"container/heap"
)

type priorityWorkQueue[T comparable] struct {
	priorityQueue *PriorityQueue
}

func (p *priorityWorkQueue[T]) Touch(item T) {
	p.priorityQueue.Adjust(item)
}

func (p *priorityWorkQueue[T]) Len() int {
	return p.priorityQueue.Len()
}

func (p *priorityWorkQueue[T]) Push(item T) {
	heap.Push(p.priorityQueue, item)
}

func (p *priorityWorkQueue[T]) Pop() T {
	return heap.Pop(p.priorityQueue).(T)
}
