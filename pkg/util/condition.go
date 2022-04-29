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

package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// TryAppendCondition appends the given Condition if it's not equal to the last Condition.
func TryAppendCondition(conditions []metav1.Condition, newCondition *metav1.Condition) []metav1.Condition {
	if newCondition == nil {
		logger.Warning("TryAppendCondition call with nil newCondition")
		return conditions
	}

	newCondition.LastTransitionTime = metav1.Now()

	numCond := len(conditions)
	if numCond > 0 && conditionsEqual(&conditions[numCond-1], newCondition) {
		return conditions
	}

	return append(conditions, *newCondition)
}

func conditionsEqual(c1, c2 *metav1.Condition) bool {
	return c1.Type == c2.Type && c1.Status == c2.Status && c1.Reason == c2.Reason && c1.Message == c2.Message
}

func ConditionsFromUnstructured(from *unstructured.Unstructured, fields ...string) []metav1.Condition {
	rawConditions, _, _ := unstructured.NestedSlice(from.Object, fields...)

	conditions := make([]metav1.Condition, len(rawConditions))

	for i := range rawConditions {
		c := &metav1.Condition{}
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(rawConditions[i].(map[string]interface{}), c)
		conditions[i] = *c
	}

	return conditions
}

func ConditionsToUnstructured(conditions []metav1.Condition, to *unstructured.Unstructured, fields ...string) {
	newConditions := make([]interface{}, len(conditions))
	for i := range conditions {
		newConditions[i], _ = runtime.DefaultUnstructuredConverter.ToUnstructured(&conditions[i])
	}

	_ = unstructured.SetNestedSlice(to.Object, newConditions, fields...)
}
