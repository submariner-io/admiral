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
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestLog(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Log Suite")
}

var (
	rootLogSink = newLogSinkImpl()
	exited      bool
)

var _ = BeforeSuite(func() {
	logf.SetLogger(logr.New(rootLogSink))

	log.Exit = func(_ int) {
		exited = true
	}
})

type logSinkImpl struct {
	infoCh  chan []any
	errorCh chan []any
}

func newLogSinkImpl() *logSinkImpl {
	return &logSinkImpl{infoCh: make(chan []any, 10), errorCh: make(chan []any, 10)}
}

func (l *logSinkImpl) Init(_ logr.RuntimeInfo) {
}

func (l *logSinkImpl) Enabled(_ int) bool {
	return true
}

func (l *logSinkImpl) Info(level int, msg string, keysAndValues ...any) {
	l.infoCh <- append([]any{level, msg}, keysAndValues...)
}

func (l *logSinkImpl) Error(err error, msg string, keysAndValues ...any) {
	l.errorCh <- append([]any{err, msg}, keysAndValues...)
}

func (l *logSinkImpl) WithValues(_ ...any) logr.LogSink {
	return newLogSinkImpl()
}

func (l *logSinkImpl) WithName(_ string) logr.LogSink {
	return newLogSinkImpl()
}
