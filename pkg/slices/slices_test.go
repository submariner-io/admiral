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
	"k8s.io/utils/set"
)

type Status struct {
	Name string
}

var _ = Describe("Intersect", func() {
	Specify("with non-empty slices", func() {
		testIntersect := func(s1, s2 []string, exp ...string) {
			actual := slices.Intersect(s1, s2, slices.Key[string])
			Expect(set.New(actual...).Equal(set.New(exp...))).To(BeTrue(), "Expected: %s. Actual: %s", exp, actual)
		}

		testIntersect([]string{"1", "2"}, []string{"1", "2"}, "1", "2")
		testIntersect([]string{"1", "2"}, []string{"2", "1"}, "1", "2")
		testIntersect([]string{"1", "2"}, []string{"1"}, "1")
		testIntersect([]string{"3"}, []string{"1", "2", "3"}, "3")
		testIntersect([]string{"1", "2", "3", "4"}, []string{"2", "5", "6", "3"}, "2", "3")
		testIntersect([]string{"1", "2", "3", "4"}, []string{"5", "6", "7", "8"})
	})

	Specify("with empty an slice", func() {
		Expect(slices.Intersect([]string{"1"}, []string{}, slices.Key[string])).To(BeEmpty())
		Expect(slices.Intersect([]string{"1"}, nil, slices.Key[string])).To(BeEmpty())
		Expect(slices.Intersect([]string{}, []string{"1"}, slices.Key[string])).To(BeEmpty())
		Expect(slices.Intersect(nil, []string{"1"}, slices.Key[string])).To(BeEmpty())
	})
})

var _ = Specify("Union", func() {
	testUnion := func(s1, s2 []string, exp ...string) {
		actual := slices.Union(s1, s2, slices.Key[string])
		Expect(actual).To(Equal(exp))
	}

	testUnion([]string{"1"}, []string{"2"}, "1", "2")
	testUnion([]string{"1"}, []string{"1"}, "1")
	testUnion([]string{"1", "2"}, []string{"3"}, "1", "2", "3")
	testUnion([]string{"1"}, []string{"2", "3"}, "1", "2", "3")
	testUnion([]string{"1", "2"}, []string{"2", "3"}, "1", "2", "3")
	testUnion([]string{"1", "2", "3"}, []string{"3", "4", "1"}, "1", "2", "3", "4")

	testUnion([]string{"1"}, []string{}, "1")
	testUnion([]string{"1"}, nil, "1")
	testUnion([]string{}, []string{"1"}, "1")
	testUnion(nil, []string{"1"}, "1")
})

var _ = Describe("IndexOf", func() {
	Specify("with existent element", func() {
		s := []Status{{Name: "1"}, {Name: "2"}, {Name: "3"}}

		Expect(slices.IndexOf(s, "1", statusKey)).To(Equal(0))
		Expect(slices.IndexOf(s, "2", statusKey)).To(Equal(1))
		Expect(slices.IndexOf(s, "3", statusKey)).To(Equal(2))
	})

	Specify("with non-existent element", func() {
		Expect(slices.IndexOf([]Status{{Name: "1"}, {Name: "2"}}, "3", statusKey)).To(Equal(-1))
		Expect(slices.IndexOf([]Status{}, "2", statusKey)).To(Equal(-1))
		Expect(slices.IndexOf(nil, "2", statusKey)).To(Equal(-1))
	})
})

var _ = Describe("AppendIfNotPresent", func() {
	Specify("with existent element", func() {
		s := []Status{{Name: "1"}, {Name: "2"}, {Name: "3"}}

		s1, added := slices.AppendIfNotPresent(s, Status{Name: "2"}, statusKey)
		Expect(s1).To(Equal(s))
		Expect(added).To(BeFalse())
	})

	Specify("with non-existent element", func() {
		s1, added := slices.AppendIfNotPresent(nil, Status{Name: "1"}, statusKey)
		Expect(s1).To(Equal([]Status{{Name: "1"}}))
		Expect(added).To(BeTrue())

		s2, added := slices.AppendIfNotPresent(s1, Status{Name: "2"}, statusKey)
		Expect(s2).To(Equal([]Status{{Name: "1"}, {Name: "2"}}))
		Expect(added).To(BeTrue())
	})
})

var _ = Describe("Remove", func() {
	Specify("with existent element", func() {
		s := []Status{{Name: "1"}, {Name: "2"}, {Name: "3"}}

		s1, removed := slices.Remove(s, Status{Name: "2"}, statusKey)
		Expect(s1).To(Equal([]Status{{Name: "1"}, {Name: "3"}}))
		Expect(removed).To(BeTrue())

		s2, removed := slices.Remove(s1, Status{Name: "1"}, statusKey)
		Expect(s2).To(Equal([]Status{{Name: "3"}}))
		Expect(removed).To(BeTrue())

		s3, removed := slices.Remove(s2, Status{Name: "3"}, statusKey)
		Expect(s3).To(Equal([]Status{}))
		Expect(removed).To(BeTrue())
	})

	Specify("with non-existent element", func() {
		s, removed := slices.Remove([]Status{{Name: "1"}}, Status{Name: "2"}, statusKey)
		Expect(s).To(Equal([]Status{{Name: "1"}}))
		Expect(removed).To(BeFalse())

		s, removed = slices.Remove([]Status{}, Status{Name: "2"}, statusKey)
		Expect(s).To(Equal([]Status{}))
		Expect(removed).To(BeFalse())

		s, removed = slices.Remove(nil, Status{Name: "2"}, statusKey)
		Expect(s).To(BeNil())
		Expect(removed).To(BeFalse())
	})
})

var _ = Describe("Equivalent", func() {
	Specify("with unique elements", func() {
		Expect(slices.Equivalent([]Status{{Name: "1"}}, []Status{{Name: "1"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}}, []Status{{Name: "1"}, {Name: "2"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{{Name: "2"}, {Name: "1"}}, []Status{{Name: "1"}, {Name: "2"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}}, []Status{{Name: "2"}, {Name: "1"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{}, []Status{}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent(nil, []Status{}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{}, nil, statusKey)).To(BeTrue())

		Expect(slices.Equivalent([]Status{{Name: "1"}}, []Status{{Name: "2"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}}, []Status{{Name: "1"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}}, []Status{{Name: "1"}, {Name: "2"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}}, []Status{{Name: "1"}, {Name: "3"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}}, []Status{{Name: "3"}, {Name: "2"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}}, []Status{}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{{Name: "1"}}, nil, statusKey)).To(BeFalse())
		Expect(slices.Equivalent([]Status{}, []Status{{Name: "1"}}, statusKey)).To(BeFalse())
		Expect(slices.Equivalent(nil, []Status{{Name: "1"}}, statusKey)).To(BeFalse())
	})

	Specify("with duplicate elements", func() {
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}, {Name: "2"}, {Name: "3"}},
			[]Status{{Name: "2"}, {Name: "3"}, {Name: "1"}, {Name: "2"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "3"}, {Name: "2"}, {Name: "2"}, {Name: "3"}},
			[]Status{{Name: "2"}, {Name: "3"}, {Name: "1"}, {Name: "3"}, {Name: "2"}}, statusKey)).To(BeTrue())

		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "2"}, {Name: "2"}, {Name: "3"}},
			[]Status{{Name: "1"}, {Name: "3"}, {Name: "1"}, {Name: "2"}}, statusKey)).To(BeTrue())
		Expect(slices.Equivalent([]Status{{Name: "1"}, {Name: "3"}, {Name: "2"}, {Name: "2"}, {Name: "3"}},
			[]Status{{Name: "2"}, {Name: "3"}, {Name: "1"}, {Name: "1"}, {Name: "2"}}, statusKey)).To(BeTrue())
	})
})

func statusKey(s Status) string {
	return s.Name
}
