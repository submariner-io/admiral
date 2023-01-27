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

func Intersect[E any, K comparable](s1, s2 []E, key func(E) K) []E {
	m := make(map[K]bool)

	for _, v := range s1 {
		m[key(v)] = true
	}

	var r []E

	for _, v := range s2 {
		if _, ok := m[key(v)]; ok {
			r = append(r, v)
		}
	}

	return r
}
