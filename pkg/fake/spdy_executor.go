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
	"context"
	"net/url"

	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type fakeExecutor struct {
	stdout string
	stderr string
	err    error
}

func SetSPDYExecutor(stdout, stderr string, err error) {
	resource.NewSPDYExecutor = func(_ *rest.Config, _ string, _ *url.URL) (remotecommand.Executor, error) {
		return &fakeExecutor{stdout: stdout, stderr: stderr, err: err}, nil
	}
}

func (f *fakeExecutor) Stream(options remotecommand.StreamOptions) error {
	return f.StreamWithContext(context.TODO(), options)
}

func (f *fakeExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	if f.err != nil {
		return f.err
	}

	if options.Stdout != nil {
		_, err := options.Stdout.Write([]byte(f.stdout))
		if err != nil {
			return err
		}
	}

	if options.Stderr != nil {
		_, err := options.Stderr.Write([]byte(f.stderr))
		if err != nil {
			return err
		}
	}

	return nil
}
