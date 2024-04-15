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
	"bytes"
	"io"
	"os/exec"
	"slices"
	"sync"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/command"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Executor struct {
	mutex          sync.Mutex
	commands       []*exec.Cmd
	commandOutputs []commandOutputInfo
}

type commandImpl struct {
	cmd  *exec.Cmd
	exec *Executor
}

type pipeReader struct {
	buffer bytes.Buffer
}

type commandOutputInfo struct {
	pathMatcher  interface{}
	expectedArgs []string
	output       string
	err          error
}

func New() *Executor {
	e := &Executor{}
	command.New = e.newCommand

	return e
}

func (e *Executor) newCommand(cmd *exec.Cmd) command.Interface {
	return &commandImpl{cmd: cmd, exec: e}
}

func (c *commandImpl) Run() error {
	return c.Start()
}

func (c *commandImpl) Start() error {
	c.exec.mutex.Lock()
	defer c.exec.mutex.Unlock()

	c.exec.commands = append(c.exec.commands, c.cmd)

	return nil
}

func (c *commandImpl) Wait() error {
	return nil
}

func cmdMatches(cmd *exec.Cmd, pathMatcher interface{}, args []string) bool {
	if pathMatcher != nil {
		matches, err := ContainElement(pathMatcher).Match([]string{cmd.Path})
		Expect(err).To(Succeed())

		if !matches {
			return false
		}
	}

	return sets.New(cmd.Args...).HasAll(args...)
}

func (c *commandImpl) StdoutPipe() (io.ReadCloser, error) {
	c.exec.mutex.Lock()
	defer c.exec.mutex.Unlock()

	r := &pipeReader{}

	for i := range c.exec.commandOutputs {
		if cmdMatches(c.cmd, c.exec.commandOutputs[i].pathMatcher, c.exec.commandOutputs[i].expectedArgs) {
			r.buffer.WriteString(c.exec.commandOutputs[i].output)
			break
		}
	}

	return r, nil
}

func (c *commandImpl) Output() ([]byte, error) {
	c.exec.mutex.Lock()
	defer c.exec.mutex.Unlock()

	c.exec.commands = append(c.exec.commands, c.cmd)

	for i := range c.exec.commandOutputs {
		if cmdMatches(c.cmd, c.exec.commandOutputs[i].pathMatcher, c.exec.commandOutputs[i].expectedArgs) {
			co := c.exec.commandOutputs[i]
			c.exec.commandOutputs = slices.Delete(c.exec.commandOutputs, i, i+1)

			return []byte(co.output), co.err
		}
	}

	return []byte{}, nil
}

func (c *commandImpl) CombinedOutput() ([]byte, error) {
	return c.Output()
}

func (r *pipeReader) Read(p []byte) (n int, err error) {
	return r.buffer.Read(p)
}

func (r *pipeReader) Close() error {
	return nil
}

func (e *Executor) getCommands() []*exec.Cmd {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	c := make([]*exec.Cmd, len(e.commands))
	copy(c, e.commands)

	return c
}

func (e *Executor) findCommand(pathMatcher interface{}, args []string) *exec.Cmd {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	for _, c := range e.commands {
		if cmdMatches(c, pathMatcher, args) {
			return c
		}
	}

	return nil
}

func (e *Executor) Clear() {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.commands = nil
}

func (e *Executor) AwaitCommand(pathMatcher interface{}, args ...string) {
	Eventually(func() bool {
		return e.findCommand(pathMatcher, args) != nil
	}, 1).Should(BeTrue(), "Command with args %v not found. Actual: %v", args, e.getCommands())
}

func (e *Executor) EnsureNoCommand(pathMatcher interface{}, args ...string) {
	Consistently(func() bool {
		return e.findCommand(pathMatcher, args) == nil
	}).Should(BeTrue(), "Found unexpected command with args %v", args)
}

func (e *Executor) SetupCommandStdOut(output string, pathMatcher interface{}, expectedArgs ...string) {
	e.SetupCommandOutputWithError(output, nil, pathMatcher, expectedArgs...)
}

func (e *Executor) SetupCommandOutputWithError(output string, err error, pathMatcher interface{}, expectedArgs ...string) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	e.commandOutputs = append([]commandOutputInfo{{
		pathMatcher:  pathMatcher,
		expectedArgs: expectedArgs,
		output:       output,
		err:          err,
	}}, e.commandOutputs...)
}
