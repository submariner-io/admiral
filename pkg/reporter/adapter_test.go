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

package reporter_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/reporter"
)

type basicImpl struct {
	reporter.Basic
	endCalled chan struct{}
}

func (b *basicImpl) End() {
	close(b.endCalled)
}

var _ = Describe("Adapter", func() {
	stdoutCapture := newStdoutCapture()

	var adapter reporter.Adapter

	BeforeEach(func() {
		adapter = reporter.Adapter{Basic: &basicImpl{Basic: reporter.Stdout(), endCalled: make(chan struct{})}}
		stdoutCapture.start()
	})

	Context("Error", func() {
		It("should log the error", func() {
			err := errors.New("some error")
			Expect(adapter.Error(err, "")).To(Equal(err))
			Expect(stdoutCapture.read()).To(ContainSubstring("Some error"))
			Expect(adapter.Basic.(*basicImpl).endCalled).To(BeClosed())
		})

		When("the error is nil", func() {
			It("should not log anything", func() {
				Expect(adapter.Error(nil, "failed")).To(Succeed())
				Expect(stdoutCapture.read()).To(BeEmpty())
			})
		})

		When("a message is specified", func() {
			It("should wrap the error with the message", func() {
				err := errors.New("some error")
				actual := adapter.Error(err, "failed")

				Expect(actual.Error()).To(HavePrefix("failed"))

				out := stdoutCapture.read()
				Expect(out).To(ContainSubstring("Failed"))
				Expect(out).To(ContainSubstring(err.Error()))
			})
		})
	})
})
