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

package slices_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/slices"
	"k8s.io/apimachinery/pkg/util/sets"
)

var _ = Describe("Intersect", func() {
	When("called with non-empty slices", func() {
		It("should return the correct intersection", func() {
			testIntersect := func(s1, s2 []string, exp ...string) {
				actual := slices.Intersect(s1, s2, key)
				Expect(sets.New(actual...).Equal(sets.New(exp...))).To(BeTrue(), "Expected: %s. Actual: %s", exp, actual)
			}

			testIntersect([]string{"1", "2"}, []string{"1", "2"}, "1", "2")
			testIntersect([]string{"1", "2"}, []string{"2", "1"}, "1", "2")
			testIntersect([]string{"1", "2"}, []string{"1"}, "1")
			testIntersect([]string{"3"}, []string{"1", "2", "3"}, "3")
			testIntersect([]string{"1", "2", "3", "4"}, []string{"2", "5", "6", "3"}, "2", "3")
			testIntersect([]string{"1", "2", "3", "4"}, []string{"5", "6", "7", "8"})
		})
	})

	When("called with empty slices", func() {
		It("should return an empty slice", func() {
			Expect(slices.Intersect([]string{"1"}, []string{}, key)).To(BeEmpty())
			Expect(slices.Intersect([]string{"1"}, nil, key)).To(BeEmpty())
			Expect(slices.Intersect([]string{}, []string{"1"}, key)).To(BeEmpty())
			Expect(slices.Intersect(nil, []string{"1"}, key)).To(BeEmpty())
		})
	})
})

func key(e string) string {
	return e
}
