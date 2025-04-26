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
	"sync"

	"github.com/submariner-io/admiral/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type itemType struct {
	value    any
	priority int
}

type priorityType struct {
	priority int
	dirty    bool
}

// PriorityQueue implements heap.Interface and orders items by descending priority such that
// items with higher priority values are de-queued first.
type PriorityQueue struct {
	items      []itemType
	indices    map[any]int
	priorities sync.Map
	name       string
	logger     log.Logger
}

func NewPriorityQueue(name string) *PriorityQueue {
	return &PriorityQueue{
		name:    name,
		items:   []itemType{},
		indices: map[any]int{},
		logger:  log.Logger{Logger: logf.Log.WithName("PriorityQueue")},
	}
}

func (p *PriorityQueue) Len() int {
	return len(p.items)
}

func (p *PriorityQueue) Less(i, j int) bool {
	return p.items[i].priority > p.items[j].priority
}

func (p *PriorityQueue) Swap(i, j int) {
	p.items[i], p.items[j] = p.items[j], p.items[i]
	p.indices[p.items[i].value] = i
	p.indices[p.items[j].value] = j
}

func (p *PriorityQueue) Push(item any) {
	index := len(p.items)
	priority := p.getPriority(item)

	p.logger.V(log.DEBUG).Infof("%s: Push \"%v\" at index %d, priority %d", p.name, item, index, priority)

	p.indices[item] = index
	p.items = append(p.items, itemType{value: item, priority: priority})
}

func (p *PriorityQueue) Pop() any {
	old := p.items
	n := len(old)
	item := old[n-1]
	old[n-1].value = nil
	p.items = old[0 : n-1]

	delete(p.indices, item.value)
	p.priorities.Delete(item.value)

	p.logger.V(log.DEBUG).Infof("%s: Pop \"%v\", size %d", p.name, item.value, len(p.items))

	return item.value
}

// SetPriority for an item. This must be called prior to pushing the item.
func (p *PriorityQueue) SetPriority(item any, priority int) {
	var priorityItem priorityType
	priorityItem.priority = priority

	v, found := p.priorities.Load(item)
	if !found {
		p.priorities.LoadOrStore(item, priorityItem)

		return
	}

	existing := v.(priorityType)
	priorityItem.dirty = existing.dirty

	if existing.priority != priority {
		priorityItem.dirty = true
	}

	p.priorities.CompareAndSwap(item, existing, priorityItem)
}

func (p *PriorityQueue) getPriority(item any) int {
	v, found := p.priorities.Load(item)
	if !found {
		return 0
	}

	return v.(priorityType).priority
}

// Adjust the order of an existing item in the queue after it's priority has been changed.
func (p *PriorityQueue) Adjust(item any) {
	index, found := p.indices[item]
	if !found {
		return
	}

	v, found := p.priorities.Load(item)
	if !found {
		return
	}

	existing := v.(priorityType)
	if existing.dirty {
		p.priorities.CompareAndSwap(item, existing, priorityType{priority: existing.priority})
		p.items[p.indices[item]].priority = existing.priority
		heap.Fix(p, index)
	}
}
