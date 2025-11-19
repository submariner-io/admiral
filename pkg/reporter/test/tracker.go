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

package test

import (
	"fmt"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/submariner-io/admiral/pkg/reporter"
)

type Tracker struct {
	reporter.Interface
	failures []string
	warnings []string
}

func (t *Tracker) Warning(message string, args ...any) {
	t.warnings = append(t.warnings, fmt.Sprintf(message, args...))
	t.Interface.Warning(message, args...)
}

func (t *Tracker) Failure(message string, args ...any) {
	t.failures = append(t.failures, fmt.Sprintf(message, args...))
	t.Interface.Failure(message, args...)
}

func (t *Tracker) Error(err error, message string, args ...any) error {
	return (&reporter.Adapter{Basic: t}).Error(err, message, args...)
}

func (t *Tracker) AssertHasFailure() {
	Expect(t.failures).NotTo(BeEmpty())
}

func (t *Tracker) AssertFailureCount(count int) {
	Expect(t.failures).To(HaveLen(count))
}

func (t *Tracker) AssertContainsFailure(matcher types.GomegaMatcher) {
	Expect(t.failures).To(ContainElement(matcher))
}

// AssertFailureContainsStrings checks if any failure message contains all the given strings. This a
// convenience function equivalent to
//
//	AssertContainsFailure(And(ContainSubstring("1"), ContainSubstring("2"), ...))
//
// If no string is provided, it checks if there are any failure messages.
func (t *Tracker) AssertFailureContainsStrings(s ...string) {
	if len(s) == 0 {
		t.AssertHasFailure()
	} else {
		matchers := make([]types.GomegaMatcher, len(s))
		for i := range s {
			matchers[i] = ContainSubstring(s[i])
		}

		t.AssertContainsFailure(And(matchers...))
	}
}

func (t *Tracker) AssertHasWarning() {
	Expect(t.warnings).NotTo(BeEmpty())
}

func (t *Tracker) AssertWarningCount(count int) {
	Expect(t.warnings).To(HaveLen(count))
}

func (t *Tracker) AssertContainsWarning(matcher types.GomegaMatcher) {
	Expect(t.warnings).To(ContainElement(matcher))
}

// AssertWarningContainsStrings checks if any warning message contains all the given strings. This a
// convenience function equivalent to
//
//	AssertContainsWarning(And(ContainSubstring("1"), ContainSubstring("2"), ...))
//
// If no string is provided, it checks if there are any warning messages.
func (t *Tracker) AssertWarningContainsStrings(s ...string) {
	if len(s) == 0 {
		t.AssertHasWarning()
	} else {
		matchers := make([]types.GomegaMatcher, len(s))
		for i := range s {
			matchers[i] = ContainSubstring(s[i])
		}

		t.AssertContainsWarning(And(matchers...))
	}
}
