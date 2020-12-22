/*
© 2020 Red Hat, Inc.

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
package stringset

import (
	"sync"
)

type Interface interface {
	Add(s string) bool
	AddAll(s ...string)
	Remove(s string) bool
	RemoveAll()
	Contains(s string) bool
	Size() int
	Elements() []string
	Difference(other Interface) []string
}

type setType struct {
	set map[string]bool
}

type synchronized struct {
	*setType
	syncMutex sync.Mutex
}

func New(s ...string) Interface {
	return newSetType(s...)
}

func NewSynchronized(s ...string) Interface {
	return &synchronized{setType: newSetType(s...)}
}

func newSetType(s ...string) *setType {
	set := &setType{set: make(map[string]bool, len(s))}
	set.AddAll(s...)

	return set
}

func (set *setType) Add(s string) bool {
	_, found := set.set[s]
	set.set[s] = true

	return !found
}

func (set *setType) AddAll(str ...string) {
	for _, s := range str {
		set.Add(s)
	}
}

func (set *setType) Contains(s string) bool {
	_, found := set.set[s]
	return found
}

func (set *setType) Size() int {
	return len(set.set)
}

func (set *setType) Remove(s string) bool {
	_, found := set.set[s]
	delete(set.set, s)

	return found
}

func (set *setType) RemoveAll() {
	for v := range set.set {
		delete(set.set, v)
	}
}

func (set *setType) Elements() []string {
	elements := make([]string, len(set.set))
	i := 0

	for v := range set.set {
		elements[i] = v
		i++
	}

	return elements
}

func (set *setType) Difference(other Interface) []string {
	if otherSet, ok := other.(*setType); ok {
		return diff(otherSet, set.Contains)
	}

	return diff2(other.Elements(), set.Contains)
}

func diff(set *setType, contains func(string) bool) []string {
	notFound := []string{}

	for item := range set.set {
		if !contains(item) {
			notFound = append(notFound, item)
		}
	}

	return notFound
}

func diff2(s []string, contains func(string) bool) []string {
	notFound := []string{}

	for _, item := range s {
		if !contains(item) {
			notFound = append(notFound, item)
		}
	}

	return notFound
}

func (set *synchronized) Add(s string) bool {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	return set.setType.Add(s)
}

func (set *synchronized) Contains(s string) bool {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	return set.containsSafe(s)
}

func (set *synchronized) containsSafe(s string) bool {
	return set.setType.Contains(s)
}

func (set *synchronized) Size() int {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	return set.setType.Size()
}

func (set *synchronized) Remove(s string) bool {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	return set.setType.Remove(s)
}

func (set *synchronized) RemoveAll() {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	set.setType.RemoveAll()
}

func (set *synchronized) Elements() []string {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	return set.setType.Elements()
}

func (set *synchronized) Difference(other Interface) []string {
	set.syncMutex.Lock()
	defer set.syncMutex.Unlock()

	if otherSet, ok := other.(*synchronized); ok {
		otherSet.syncMutex.Lock()
		defer otherSet.syncMutex.Unlock()

		return diff(otherSet.setType, set.containsSafe)
	}

	return diff2(other.Elements(), set.containsSafe)
}
