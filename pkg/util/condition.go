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
)

// TryAppendCondition appends the given Condition if it's not equal to the last Condition. Returns a new array if appended,
// otherwise nil.
func TryAppendCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	newCondition.LastTransitionTime = metav1.Now()

	numCond := len(conditions)
	if numCond > 0 && conditionsEqual(&conditions[numCond-1], &newCondition) {
		return nil
	}

	return append(conditions, newCondition)
}

func conditionsEqual(c1, c2 *metav1.Condition) bool {
	return c1.Type == c2.Type && c1.Status == c2.Status && c1.Reason == c2.Reason && c1.Message == c2.Message
}
