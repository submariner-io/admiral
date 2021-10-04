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

// Package gomega provides Gomega matchers used in our tests.
package gomega

import (
	"fmt"
	"strings"

	"github.com/onsi/gomega/format"
	gomegaTypes "github.com/onsi/gomega/types"
)

type containErrorSubstring struct {
	expected error
}

// ContainErrorSubstring checks whether the actual error’s error message
// contains the expected error’s error message, not as an exact match but
// as a substring.
func ContainErrorSubstring(expected error) gomegaTypes.GomegaMatcher {
	return &containErrorSubstring{expected}
}

func (m *containErrorSubstring) Match(x interface{}) (bool, error) {
	actual, ok := x.(error)
	if !ok {
		return false, fmt.Errorf("containErrorSubstring matcher requires an error.  Got:\n%s", format.Object(x, 1))
	}

	return strings.Contains(actual.Error(), m.expected.Error()), nil
}

func (m *containErrorSubstring) FailureMessage(actual interface{}) string {
	return format.Message(actual, "to contain substring", m.expected.Error())
}

func (m *containErrorSubstring) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to contain substring", m.expected.Error())
}
