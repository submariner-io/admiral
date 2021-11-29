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
	"sync/atomic"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

type FailOnActionReactor struct {
	fail atomic.Value
}

func (r *FailOnActionReactor) Fail(v bool) {
	r.fail.Store(v)
}

func FailOnAction(f *testing.Fake, resource, verb string, customErr error, autoReset bool) *FailOnActionReactor {
	r := &FailOnActionReactor{}
	r.fail.Store(true)

	retErr := customErr
	if retErr == nil {
		retErr = errors.New("fake error")
	}

	chain := []testing.Reactor{&testing.SimpleReactor{
		Verb:     verb,
		Resource: resource,
		Reaction: func(action testing.Action) (bool, runtime.Object, error) {
			if r.fail.Load().(bool) {
				if autoReset {
					r.fail.Store(false)
				}

				return true, nil, retErr
			}

			return false, nil, nil
		},
	}}
	f.ReactionChain = append(chain, f.ReactionChain...)

	return r
}
