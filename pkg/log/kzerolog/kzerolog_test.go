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

package kzerolog_test

import (
	"errors"
	"flag"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/global"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/log/kzerolog"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestZerolog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Zerolog Suite")
}

func init() {
	flags := flag.NewFlagSet("kzerolog", flag.ExitOnError)
	kzerolog.AddFlags(flags)

	_ = flags.Parse([]string{"-v=4"})

	kzerolog.InitK8sLogging()
}

// We don't explicitly verify anything in this test. The purpose is to invoke the various log levels and visually inspect the output
// for correctness.
var _ = When("zerolog is configured", func() {
	It("should output in the desired format", func() {
		logger := log.Logger{Logger: logf.Log.WithName("test")}

		logger.Info("Info log with keys and values.", "key1", "value1", "key2", "value2")
		logger.Infof("Infof log: arg: %s", "value")

		logger.Warning("Warning log with keys and values.", "key1", "value1", "key2", "value2")
		logger.Warningf("Warningf log: arg: %s", "value")

		logger.Error(errors.New("mock error"), "Error log with keys and values.", "key1", "value1", "key2", "value2")
		logger.Errorf(errors.New("mock error"), "Errorf log: arg: %s", "value")

		logger.V(log.DEBUG).Info("Debug log")
		logger.V(log.TRACE).Info("Trace log")

		logger.Logger.Error(nil, "Fatal log", log.FatalKey, "true")
	})

	Context("and max-verbosity is globally configured for a logger", func() {
		It("should limit the output to the desired verbosity level", func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					"log.TestModule.max-verbosity": strconv.Itoa(log.DEBUG),
				},
			})

			logger := log.Logger{Logger: logf.Log.WithName("TestModule")}

			// Should output at DEBUG level.
			logger.V(log.DEBUG).Info("DEBUG level")

			// Should not output at TRACE level.
			logger.V(log.TRACE).Info("TRACE level")
		})
	})
})
