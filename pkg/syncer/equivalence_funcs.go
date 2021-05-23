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
package syncer

import (
	"reflect"

	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func DefaultResourcesEquivalent(obj1, obj2 *unstructured.Unstructured) bool {
	return reflect.DeepEqual(obj1.GetLabels(), obj2.GetLabels()) &&
		reflect.DeepEqual(obj1.GetAnnotations(), obj2.GetAnnotations()) &&
		AreSpecsEquivalent(obj1, obj2)
}

func ResourcesNotEquivalent(obj1, obj2 *unstructured.Unstructured) bool {
	return false
}

func AreSpecsEquivalent(obj1, obj2 *unstructured.Unstructured) bool {
	return equality.Semantic.DeepEqual(util.GetSpec(obj1), util.GetSpec(obj2))
}
