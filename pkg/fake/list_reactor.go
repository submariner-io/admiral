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

	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/meta"
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

		objs, err := meta.ExtractList(obj)
		if err != nil {
			return true, nil, err
		}

		filtered := []runtime.Object{}

		for i := range objs {
			objMeta := resource.MustToMeta(objs[i])
			fieldSet := fields.Set{"metadata.namespace": objMeta.GetNamespace(), "metadata.name": objMeta.GetName()}
			if listAction.ListRestrictions.Fields.Matches(fieldSet) {
				filtered = append(filtered, objs[i])
			}
		}

		err = meta.SetList(obj, filtered)

		return true, obj, err
	default:
		return false, nil, fmt.Errorf("invalid action: %#v", action)
	}
}
