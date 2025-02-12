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

package log_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("Logger", func() {
	var logger log.Logger

	BeforeEach(func() {
		exited = false
		logger = log.Logger{Logger: logf.Log}
	})

	Specify("Infof should log a formatted info message", func() {
		logger.Infof("value is %d", 3)
		Expect(rootLogSink.infoCh).To(Receive(HaveExactElements(0, "value is 3")))
	})

	Specify("Info should log an info message with keys and values", func() {
		logger.Info("hello", "key", "value")
		Expect(rootLogSink.infoCh).To(Receive(HaveExactElements(0, "hello", "key", "value")))
	})

	Specify("Errorf should log the error with a formatted message", func() {
		err := errors.New("some error")
		logger.Errorf(err, "value is %d", 3)
		Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(err, "value is 3")))
	})

	Specify("Error should log the error with a message and keys and values", func() {
		err := errors.New("some error")
		logger.Error(err, "failed", "key", "value")
		Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(err, "failed", "key", "value")))
	})

	Specify("Warningf should log a formatted info message", func() {
		logger.Warningf("value is %d", 3)
		Expect(rootLogSink.infoCh).To(Receive(HaveExactElements(0, "value is 3", log.WarningKey, "true")))
	})

	Specify("Warning should log an info message with keys and values", func() {
		logger.Warning("hello", "key", "value")
		Expect(rootLogSink.infoCh).To(Receive(HaveExactElements(0, "hello", "key", "value", log.WarningKey, "true")))
	})

	Specify("Fatalf should log a formatted error message and then exit", func() {
		logger.Fatalf("value is %d", 3)
		Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(nil, "value is 3", log.FatalKey, "true")))
		Expect(exited).To(BeTrue())
	})

	Specify("Fatal should log an error message with keys and values and then exit", func() {
		logger.Fatal("failed", "key", "value")
		Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(nil, "failed", "key", "value", log.FatalKey, "true")))
		Expect(exited).To(BeTrue())
	})

	Context("FatalfOnError", func() {
		Specify("with an error specified should log the error and then exit", func() {
			err := errors.New("some error")
			logger.FatalfOnError(err, "value is %d", 3)
			Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(err, "value is 3", log.FatalKey, "true")))
			Expect(exited).To(BeTrue())
		})

		Specify("with no error specified should not exit", func() {
			logger.FatalfOnError(nil, "value is %d", 3)
			Expect(rootLogSink.errorCh).NotTo(Receive())
			Expect(exited).To(BeFalse())
		})
	})

	Specify("FatalOnError should log the error and then exit", func() {
		err := errors.New("some error")
		logger.FatalOnError(err, "failed", "key", "value")
		Expect(rootLogSink.errorCh).To(Receive(HaveExactElements(err, "failed", "key", "value", log.FatalKey, "true")))
		Expect(exited).To(BeTrue())
	})

	Specify("V should set the log level", func() {
		logger.V(2).Info("message")
		Expect(rootLogSink.infoCh).To(Receive(HaveExactElements(2, "message")))
	})
})
