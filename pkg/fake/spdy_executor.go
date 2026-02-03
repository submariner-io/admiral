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

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type SPDYExecutor struct {
	Stdout     string
	Stderr     string
	Err        error
	URLMatcher types.GomegaMatcher
}

func SetSPDYExecutor(stdout, stderr string, err error) {
	SetSPDYExecutors(SPDYExecutor{Stdout: stdout, Stderr: stderr, Err: err})
}

func SetSPDYExecutors(executors ...SPDYExecutor) {
	resource.NewSPDYExecutor = func(_ *rest.Config, _ string, url *url.URL) (remotecommand.Executor, error) {
		for _, e := range executors {
			if e.URLMatcher == nil {
				return &e, nil
			}

			ok, err := e.URLMatcher.Match(url.String())
			Expect(err).ToNot(HaveOccurred())

			if ok {
				return &e, nil
			}
		}

		return &SPDYExecutor{}, nil
	}
}

func (f *SPDYExecutor) Stream(options remotecommand.StreamOptions) error {
	return f.StreamWithContext(context.TODO(), options)
}

func (f *SPDYExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	if f.Err != nil {
		return f.Err
	}

	if options.Stdout != nil {
		_, err := options.Stdout.Write([]byte(f.Stdout))
		if err != nil {
			return err
		}
	}

	if options.Stderr != nil {
		_, err := options.Stderr.Write([]byte(f.Stderr))
		if err != nil {
			return err
		}
	}

	return nil
}
