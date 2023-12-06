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

package util_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("DeeplyEmpty", func() {
	Specify("with an empty map should return true", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{})).To(BeTrue())
	})

	Specify("with nested empty maps should return true", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested": map[string]interface{}{},
		})).To(BeTrue())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{},
			"nested2": map[string]interface{}{},
		})).To(BeTrue())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{
				"nested2": map[string]interface{}{},
			},
		})).To(BeTrue())
	})

	Specify("with a non-empty map should return false", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{"foo": "bar"})).To(BeFalse())
	})

	Specify("with nested non-empty maps should return false", func() {
		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested": map[string]interface{}{},
			"foo":    "bar",
		})).To(BeFalse())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"foo":    "bar",
			"nested": map[string]interface{}{},
		})).To(BeFalse())

		Expect(util.DeeplyEmpty(map[string]interface{}{
			"nested1": map[string]interface{}{
				"nested2": map[string]interface{}{},
				"foo":     "bar",
			},
		})).To(BeFalse())
	})

	Specify("an empty Service status should return true", func() {
		Expect(util.DeeplyEmpty(util.GetNestedField(resource.MustToUnstructuredUsingDefaultConverter(&corev1.Service{}),
			util.StatusField).(map[string]interface{}))).To(BeTrue())
	})
})
