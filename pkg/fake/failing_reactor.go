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
	mutex          sync.Mutex
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

	f.Lock()
	defer f.Unlock()

	f.PrependReactor("*", resource, r.react)

	return r
}

func (f *FailingReactor) react(action testing.Action) (bool, runtime.Object, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	var fail *error

	switch action.GetVerb() {
	case "get":
		fail = &f.failOnGet
	case "create":
		fail = &f.failOnCreate
	case "update":
		fail = &f.failOnUpdate
	case "delete":
		fail = &f.failOnDelete
	case "list":
		fail = &f.failOnList
	}

	if *fail != nil {
		err := *fail

		if f.resetOnFailure {
			*fail = nil
		}

		return true, nil, err
	}

	return false, nil, nil
}

func (f *FailingReactor) SetResetOnFailure(v bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.resetOnFailure = v
}

func (f *FailingReactor) SetFailOnCreate(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failOnCreate = err
}

func (f *FailingReactor) SetFailOnUpdate(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failOnUpdate = err
}

func (f *FailingReactor) SetFailOnDelete(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failOnDelete = err
}

func (f *FailingReactor) SetFailOnGet(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failOnGet = err
}

func (f *FailingReactor) SetFailOnList(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.failOnList = err
}
