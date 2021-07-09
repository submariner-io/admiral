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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

type FailingReactor struct {
	sync.Mutex
	failOnCreate   error
	failOnUpdate   error
	failOnDelete   error
	failOnGet      error
	failOnList     error
	resetOnFailure bool
}

func NewFailingReactor(f *testing.Fake) *FailingReactor {
	return NewFailingReactorForResource(f, "*")
}

func NewFailingReactorForResource(f *testing.Fake, resource string) *FailingReactor {
	r := &FailingReactor{}
	chain := []testing.Reactor{&testing.SimpleReactor{Verb: "*", Resource: resource, Reaction: r.react}}
	f.ReactionChain = append(chain, f.ReactionChain...)

	return r
}

func (f *FailingReactor) react(action testing.Action) (bool, runtime.Object, error) {
	f.Lock()
	defer f.Unlock()

	switch action.GetVerb() {
	case "get":
		return f.get()
	case "create":
		return f.create()
	case "update":
		return f.update()
	case "delete":
		return f.delete()
	case "list":
		return f.list()
	}

	return false, nil, nil
}

func (f *FailingReactor) get() (bool, runtime.Object, error) {
	err := f.failOnGet
	if err != nil {
		if f.resetOnFailure {
			f.failOnGet = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) create() (bool, runtime.Object, error) {
	err := f.failOnCreate
	if err != nil {
		if f.resetOnFailure {
			f.failOnCreate = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) update() (bool, runtime.Object, error) {
	err := f.failOnUpdate
	if err != nil {
		if f.resetOnFailure {
			f.failOnUpdate = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) delete() (bool, runtime.Object, error) {
	err := f.failOnDelete
	if err != nil {
		if f.resetOnFailure {
			f.failOnDelete = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) list() (bool, runtime.Object, error) {
	err := f.failOnList
	if err != nil {
		if f.resetOnFailure {
			f.failOnList = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) SetResetOnFailure(v bool) {
	f.Lock()
	defer f.Unlock()
	f.resetOnFailure = v
}

func (f *FailingReactor) SetFailOnCreate(err error) {
	f.Lock()
	defer f.Unlock()
	f.failOnCreate = err
}

func (f *FailingReactor) SetFailOnUpdate(err error) {
	f.Lock()
	defer f.Unlock()
	f.failOnUpdate = err
}

func (f *FailingReactor) SetFailOnDelete(err error) {
	f.Lock()
	defer f.Unlock()
	f.failOnDelete = err
}

func (f *FailingReactor) SetFailOnGet(err error) {
	f.Lock()
	defer f.Unlock()
	f.failOnGet = err
}

func (f *FailingReactor) SetFailOnList(err error) {
	f.Lock()
	defer f.Unlock()
	f.failOnList = err
}
