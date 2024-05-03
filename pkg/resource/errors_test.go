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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("IsMissingNamespaceErr", func() {
	When("the error isn't NotFound", func() {
		It("should return false", func() {
			ok, _ := resource.IsMissingNamespaceErr(apierrors.NewBadRequest(""))
			Expect(ok).To(BeFalse())
		})
	})

	When("the error details specify a namespace", func() {
		It("should return true and the name", func() {
			ok, name := resource.IsMissingNamespaceErr(apierrors.NewNotFound(schema.GroupResource{
				Resource: "namespaces",
			}, "missing-ns"))
			Expect(ok).To(BeTrue())
			Expect(name).To(Equal("missing-ns"))
		})
	})

	When("the error details does not specify a namespace", func() {
		It("should return false", func() {
			ok, _ := resource.IsMissingNamespaceErr(apierrors.NewNotFound(schema.GroupResource{
				Resource: "pods",
			}, "missing"))
			Expect(ok).To(BeFalse())
		})
	})
})
