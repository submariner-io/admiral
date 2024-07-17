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

package syncer

import (
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type createOperation *unstructured.Unstructured

type deleteOperation *unstructured.Unstructured

type operationQueueMap struct {
	mutex  sync.Mutex
	queues map[string][]any
}

func (m *operationQueueMap) peek(key string) any {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	q := m.queues[key]
	if len(q) == 0 {
		return nil
	}

	return q[0]
}

func (m *operationQueueMap) remove(key string, op any) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	q := m.queues[key]
	if len(q) == 0 {
		return false
	}

	if op != nil && op == q[0] {
		m.queues[key] = q[1:]
	}

	newLen := len(m.queues[key])
	if newLen == 0 {
		delete(m.queues, key)
	}

	return newLen > 0
}

func (m *operationQueueMap) add(key string, v any) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.queues[key] = append(m.queues[key], v)
}
