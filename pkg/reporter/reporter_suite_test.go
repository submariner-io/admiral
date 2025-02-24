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
	"bytes"
	"io"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReporter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reporter Suite")
}

type StdoutCapture struct {
	oldStdout *os.File
	readFile  *os.File
	writeFile *os.File
}

func newStdoutCapture() *StdoutCapture {
	s := &StdoutCapture{oldStdout: os.Stdout}

	AfterEach(func() {
		s.done()
	})

	return s
}

func (s *StdoutCapture) start() {
	var err error

	s.readFile, s.writeFile, err = os.Pipe()
	Expect(err).To(Succeed())

	os.Stdout = s.writeFile
}

func (s *StdoutCapture) read() string {
	s.writeFile.Close()
	os.Stdout = s.oldStdout

	var buf bytes.Buffer
	_, err := io.Copy(&buf, s.readFile)
	Expect(err).To(Succeed())

	return buf.String()
}

func (s *StdoutCapture) done() {
	os.Stdout = s.oldStdout
}
