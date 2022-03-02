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
	"sync"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/testing"
)

func ConflictOnUpdateReactor(f *testing.Fake, resource string) {
	reactors := f.ReactionChain[0:]
	resourceVersion := "100"
	state := sync.Map{}

	chain := []testing.Reactor{&testing.SimpleReactor{
		Verb:     "get",
		Resource: resource,
		Reaction: func(action testing.Action) (bool, runtime.Object, error) {
			obj, err := propagate(action, reactors)
			if obj != nil {
				m, _ := meta.Accessor(obj)
				_, ok := state.Load(m.GetName())
				if ok {
					m.SetResourceVersion(resourceVersion)
				}
			}

			return true, obj, err
		},
	}, &testing.SimpleReactor{
		Verb:     "update",
		Resource: resource,
		Reaction: func(action testing.Action) (bool, runtime.Object, error) {
			updateAction := action.(testing.UpdateActionImpl)
			m, _ := meta.Accessor(updateAction.Object)

			_, ok := state.Load(m.GetName())
			if !ok {
				state.Store(m.GetName(), true)

				return true, nil, apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake conflict"))
			}

			if m.GetResourceVersion() != resourceVersion {
				return true, nil, apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake conflict"))
			}

			state.Delete(m.GetName())

			return false, nil, nil
		},
	}}
	f.ReactionChain = append(chain, f.ReactionChain...)
}

func propagate(action testing.Action, toReactors []testing.Reactor) (runtime.Object, error) {
	for _, reactor := range toReactors {
		if !reactor.Handles(action) {
			continue
		}

		handled, ret, err := reactor.React(action)
		if !handled {
			continue
		}

		return ret, err
	}

	return nil, errors.New("action not handled")
}
