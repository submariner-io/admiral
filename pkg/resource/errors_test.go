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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

var _ = Describe("IsNotFoundErr", func() {
	When("the error is NotFound", func() {
		It("should return true", func() {
			Expect(resource.IsNotFoundErr(apierrors.NewNotFound(schema.GroupResource{}, "foo"))).To(BeTrue())
		})
	})

	When("the error is NoMatchError", func() {
		It("should return true", func() {
			Expect(resource.IsNotFoundErr(&meta.NoKindMatchError{})).To(BeTrue())
		})
	})

	When("the error is ErrGroupDiscoveryFailed", func() {
		Context("and the underlying error is NotFound", func() {
			It("should return true", func() {
				Expect(resource.IsNotFoundErr(&discovery.ErrGroupDiscoveryFailed{
					Groups: map[schema.GroupVersion]error{{}: apierrors.NewNotFound(schema.GroupResource{}, "foo")},
				})).To(BeTrue())
			})
		})

		Context("and the underlying error does not indicate not found", func() {
			It("should return false", func() {
				Expect(resource.IsNotFoundErr(&discovery.ErrGroupDiscoveryFailed{
					Groups: map[schema.GroupVersion]error{{}: apierrors.NewServiceUnavailable("")},
				})).To(BeFalse())
			})
		})
	})

	When("the error does not indicate not found", func() {
		It("should return false", func() {
			Expect(resource.IsNotFoundErr(apierrors.NewForbidden(schema.GroupResource{}, "foo", errors.New("")))).To(BeFalse())
		})
	})
})
