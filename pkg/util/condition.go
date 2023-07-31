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
