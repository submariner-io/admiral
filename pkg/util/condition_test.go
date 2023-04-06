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

var _ = Describe("TryAppendCondition", func() {
	var (
		newCondition  metav1.Condition
		inConditions  []metav1.Condition
		outConditions []metav1.Condition
	)

	BeforeEach(func() {
		newCondition = metav1.Condition{
			Type:    "Valid",
			Status:  metav1.ConditionFalse,
			Reason:  "BadValue",
			Message: "Unable to parse",
		}

		inConditions = nil
	})

	JustBeforeEach(func() {
		outConditions = util.TryAppendCondition(inConditions, &newCondition)
	})

	compareLast := func(expLen int) {
		Expect(outConditions).To(HaveLen(expLen))
		newCondition.LastTransitionTime = outConditions[len(outConditions)-1].LastTransitionTime
		Expect(outConditions[len(outConditions)-1]).To(Equal(newCondition))
	}

	When("the input array is nil", func() {
		It("should append", func() {
			compareLast(1)
		})
	})

	When("the last element of the input array does not match", func() {
		BeforeEach(func() {
			inConditions = []metav1.Condition{newCondition}
		})

		Context("by Type", func() {
			BeforeEach(func() {
				inConditions[0].Type = "Conflict"
			})

			It("should append", func() {
				compareLast(2)
			})
		})

		Context("by Status", func() {
			BeforeEach(func() {
				inConditions[0].Status = metav1.ConditionTrue
			})

			It("should append", func() {
				compareLast(2)
			})
		})

		Context("by Reason", func() {
			BeforeEach(func() {
				inConditions[0].Reason = "Unavailable"
			})

			It("should append", func() {
				compareLast(2)
			})
		})

		Context("by Message", func() {
			BeforeEach(func() {
				inConditions[0].Message = "Expected something else"
			})

			It("should append", func() {
				compareLast(2)
			})
		})
	})

	When("the last element of the input array does match", func() {
		BeforeEach(func() {
			inConditions = []metav1.Condition{newCondition}
		})

		It("should not append", func() {
			compareLast(1)
		})
	})
})

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
