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

package mock

import (
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

func FormattingMatcher(expected interface{}, matcher gomock.Matcher) gomock.Matcher {
	return gomock.GotFormatterAdapter(gomock.GotFormatterFunc(FormatToYAML),
		gomock.WantFormatter(gomock.StringerFunc(func() string {
			return FormatToYAML(expected)
		}), matcher))
}

func Eq(expected interface{}) gomock.Matcher {
	return FormattingMatcher(expected, gomock.Eq(expected))
}

func FormatToYAML(o interface{}) string {
	b, err := yaml.Marshal(o)
	Expect(err).To(Succeed())

	return fmt.Sprintf("%T:\n%s", o, string(b))
}
