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

package slices

import "reflect"

// Intersect returns the intersection of two slices using the given key function.
// The returned intersection preserves the order of elements in s2.
func Intersect[E any, K comparable](s1, s2 []E, key func(E) K) []E {
	var r []E

	m := mapFrom(s1, key)
	for i := range s2 {
		if _, ok := m[key(s2[i])]; ok {
			r = append(r, s2[i])
		}
	}

	return r
}

// IndexOf returns the index of the first occurrence of element e in s using the given key function, or -1 if not present.
func IndexOf[E any, K comparable](s []E, e K, key func(E) K) int {
	for i := range s {
		if key(s[i]) == e {
			return i
		}
	}

	return -1
}

// AppendIfNotPresent appends element e to s if not already present using the given key function. Returns the resultant
// slice and a bool indicating if the element was added.
func AppendIfNotPresent[E any, K comparable](s []E, e E, key func(E) K) ([]E, bool) {
	i := IndexOf(s, key(e), key)
	if i >= 0 {
		return s, false
	}

	return append(s, e), true
}

// Remove the first instance of element e from s using the given key function. Returns the resultant slice and a bool
// indicating if the element was removed.
func Remove[E any, K comparable](s []E, e E, key func(E) K) ([]E, bool) {
	i := IndexOf(s, key(e), key)
	if i < 0 {
		return s, false
	}

	copy(s[i:], s[i+1:])

	return s[:len(s)-1], true
}

// Union returns the union of two slices using the given key function.
func Union[E any, K comparable](s1, s2 []E, key func(E) K) []E {
	m := mapFrom(s1, key)
	for i := range s2 {
		if _, ok := m[key(s2[i])]; !ok {
			s1 = append(s1, s2[i])
		}
	}

	return s1
}

// Equivalent reports whether two slices contain the same elements regardless of order and uniqueness.
func Equivalent[E any, K comparable](s1, s2 []E, key func(E) K) bool {
	m1 := mapFrom(s1, key)
	m2 := mapFrom(s2, key)

	return reflect.DeepEqual(m1, m2)
}

func mapFrom[E any, K comparable](s []E, key func(E) K) map[K]bool {
	m := make(map[K]bool)
	for _, v := range s {
		m[key(v)] = true
	}

	return m
}

func Key[K comparable](k K) K {
	return k
}
