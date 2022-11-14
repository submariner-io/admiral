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

package resource_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
)

var _ = Describe("EnsureValidName", func() {
	When("the string is valid", func() {
		It("should not convert it", func() {
			Expect(resource.EnsureValidName("digits-1234567890")).To(Equal("digits-1234567890"))
			Expect(resource.EnsureValidName("example.com")).To(Equal("example.com"))
		})
	})

	When("the string has upper case letters", func() {
		It("should convert to lower", func() {
			Expect(resource.EnsureValidName("No-UPPER-caSe-aLLoweD")).To(Equal("no-upper-case-allowed"))
		})
	})

	When("the string has non-alphanumeric letters", func() {
		It("should convert them approriately", func() {
			Expect(resource.EnsureValidName("no-!@*()#$-chars")).To(Equal("no---------chars"))
		})
	})
})
