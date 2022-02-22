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

package fake

import (
	"fmt"

	"github.com/submariner-io/admiral/pkg/syncer/test"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

type filteringListReactor struct {
	reactors []testing.Reactor
}

func AddFilteringListReactor(f *testing.Fake) {
	r := &filteringListReactor{reactors: f.ReactionChain[0:]}
	f.PrependReactor("list", "*", r.react)
}

func (r *filteringListReactor) react(action testing.Action) (bool, runtime.Object, error) {
	switch listAction := action.(type) {
	case testing.ListActionImpl:
		obj, err := invokeReactors(listAction, r.reactors)
		if err != nil {
			return true, nil, err
		}

		list, err := test.ToUnstructured(obj).ToList()
		if err != nil {
			return true, nil, err
		}

		filtered := &unstructured.UnstructuredList{Object: list.Object}

		for i := range list.Items {
			item := &list.Items[i]
			fieldSet := fields.Set{"metadata.namespace": item.GetNamespace(), "metadata.name": item.GetName()}
			if listAction.ListRestrictions.Fields.Matches(fieldSet) {
				filtered.Items = append(filtered.Items, *item)
			}
		}

		return true, filtered, nil
	default:
		return false, nil, fmt.Errorf("invalid action: %#v", action)
	}
}
