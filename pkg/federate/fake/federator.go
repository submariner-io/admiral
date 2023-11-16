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

// Package fake provides a fake Federator for use in tests.
package fake

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type Federator struct {
	distribute       chan *unstructured.Unstructured
	delete           chan *unstructured.Unstructured
	FailOnDistribute error
	FailOnDelete     error
	ResetOnFailure   bool
}

func New() *Federator {
	return &Federator{
		distribute:     make(chan *unstructured.Unstructured, 100),
		delete:         make(chan *unstructured.Unstructured, 100),
		ResetOnFailure: true,
	}
}

func (f *Federator) Distribute(_ context.Context, obj runtime.Object) error {
	err := f.FailOnDistribute
	if err != nil {
		if f.ResetOnFailure {
			f.FailOnDistribute = nil
		}

		return err
	}

	f.distribute <- resource.MustToUnstructured(obj)

	return nil
}

func (f *Federator) Delete(_ context.Context, obj runtime.Object) error {
	err := f.FailOnDelete
	if err != nil {
		if f.ResetOnFailure {
			f.FailOnDelete = nil
		}

		return err
	}

	f.delete <- resource.MustToUnstructured(obj)

	return nil
}

func (f *Federator) VerifyDistribute(expected runtime.Object) {
	Eventually(f.distribute, 5).Should(Receive(Equal(resource.MustToUnstructured(expected))), "Distribute was not called")
}

func (f *Federator) VerifyNoDistribute() {
	Consistently(f.distribute, 300*time.Millisecond).ShouldNot(Receive(), "Distribute was unexpectedly called")
}

func (f *Federator) VerifyDelete(expected runtime.Object) {
	Eventually(f.delete, 5).Should(Receive(Equal(resource.MustToUnstructured(expected))), "Delete was not called")
}

func (f *Federator) VerifyNoDelete() {
	Consistently(f.delete, 300*time.Millisecond).ShouldNot(Receive(), "Delete was unexpectedly called")
}
