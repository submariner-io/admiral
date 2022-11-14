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
package stringset_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/stringset"
)

func TestStringSet(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StringSet Suite")
}

var _ = Describe("Unsynchronized set", func() {
	test(stringset.New, stringset.NewSynchronized)
})

var _ = Describe("Synchronized set", func() {
	test(stringset.NewSynchronized, stringset.New)
})

func test(creator, creator2 func(s ...string) stringset.Interface) {
	var set stringset.Interface

	BeforeEach(func() {
		set = creator("one", "two")
	})

	It("should return the correct size", func() {
		Expect(set.Size()).To(Equal(2))
	})

	When("it contains a specified string", func() {
		It("should return true", func() {
			Expect(set.Contains("one")).To(BeTrue())
			Expect(set.Contains("two")).To(BeTrue())
		})
	})

	When("it does not contain a specified string", func() {
		It("should return false", func() {
			Expect(set.Contains("three")).To(BeFalse())
		})
	})

	When("adding a string that does not exist", func() {
		It("should add it and return true", func() {
			Expect(set.Add("three")).To(BeTrue())
			Expect(set.Size()).To(Equal(3))
			Expect(set.Contains("three")).To(BeTrue())
		})
	})

	When("adding a string that already exists", func() {
		It("should not add it again", func() {
			Expect(set.Add("one")).To(BeFalse())
			Expect(set.Size()).To(Equal(2))
			Expect(set.Contains("one")).To(BeTrue())
		})
	})

	It("should add multiple strings", func() {
		set.AddAll("three", "four", "five")
		Expect(set.Size()).To(Equal(5))
		Expect(set.Contains("three")).To(BeTrue())
		Expect(set.Contains("four")).To(BeTrue())
		Expect(set.Contains("five")).To(BeTrue())
	})

	When("an existing string is removed", func() {
		It("should return true and no longer be observed in the set", func() {
			Expect(set.Remove("two")).To(BeTrue())
			Expect(set.Contains("two")).To(BeFalse())
			Expect(set.Size()).To(Equal(1))
		})
	})

	When("a non-existent string is removed", func() {
		It("should return false", func() {
			Expect(set.Remove("non-existent")).To(BeFalse())
		})
	})

	When("all strings are removed", func() {
		It("should be empty", func() {
			set.RemoveAll()
			Expect(set.Contains("one")).To(BeFalse())
			Expect(set.Contains("two")).To(BeFalse())
			Expect(set.Size()).To(Equal(0))
		})
	})

	When("a string is re-added", func() {
		It("should be observed in the set", func() {
			set.Remove("two")
			set.Add("two")
			Expect(set.Contains("two")).To(BeTrue())
			Expect(set.Size()).To(Equal(2))
		})
	})

	It("should return the correct elements", func() {
		containsElements(set.Elements(), "one", "two")

		set.Add("three")
		containsElements(set.Elements(), "one", "two", "three")

		set.Remove("one")
		set.Remove("three")
		containsElements(set.Elements(), "two")

		containsElements(creator().Elements())
	})

	It("should calculate the difference correctly", func() {
		set2 := creator("one", "three")
		set3 := creator2("one", "three")

		containsElements(set.Difference(set2), "three")
		containsElements(set.Difference(set3), "three")

		containsElements(set2.Difference(set), "two")
		containsElements(set3.Difference(set), "two")
	})
}

func containsElements(actual []string, exp ...string) {
	for _, s := range exp {
		Expect(actual).To(ContainElement(s))
	}

	Expect(actual).To(HaveLen(len(exp)))
}
