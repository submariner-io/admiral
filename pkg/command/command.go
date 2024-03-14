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

package command

import (
	"io"
	"os/exec"
)

type Interface interface {
	Run() error
	Start() error
	Wait() error
	StdoutPipe() (io.ReadCloser, error)
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
}

var New = func(cmd *exec.Cmd) Interface {
	return cmd
}
