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
	"github.com/submariner-io/admiral/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Unstructured Conditions conversion", func() {
	It("should correctly convert to and from Unstructured", func() {
		conditions := []metav1.Condition{
			{
				Type:    "Valid",
				Status:  metav1.ConditionFalse,
				Reason:  "BadValue",
				Message: "Unable to parse",
			},
			{
				Type:   "Valid",
				Status: metav1.ConditionTrue,
				Reason: "Success",
			},
		}

		obj := &unstructured.Unstructured{Object: map[string]interface{}{}}
		util.ConditionsToUnstructured(conditions, obj, "status", "conditions")

		newConditions := util.ConditionsFromUnstructured(obj, "status", "conditions")
		Expect(newConditions).To(Equal(conditions))

		obj = &unstructured.Unstructured{Object: map[string]interface{}{}}
		newConditions = util.ConditionsFromUnstructured(obj, "status", "conditions")
		Expect(newConditions).To(BeEmpty())
	})
})
