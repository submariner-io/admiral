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

package global_test

import (
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/global"
	corev1 "k8s.io/api/core/v1"
)

const (
	stringKey                   = "string"
	intKey                      = "int"
	uint16Key                   = "uint16"
	uint32Key                   = "uint32"
	uint64Key                   = "uint64"
	boolKey                     = "bool"
	durationKey                 = "duration"
	nonExistent                 = "non-existent"
	stringValue                 = "string-value"
	defaultStringValue          = "default"
	intValue                    = 10
	defaultIntValue             = 99
	uint16Value          uint16 = 11
	defaultUint16Value   uint16 = 98
	uint32Value          uint32 = 4294967295
	defaultUint32Value   uint32 = 4294967290
	uint64Value          uint64 = 18446744073709551615
	defaultUint64Value   uint64 = 18446744073709551610
	boolValue                   = true
	defaultBoolValue            = false
	durationValue               = time.Hour * 2
	defaultDurationValue        = time.Minute * 30
)

func runTestCase[T global.ValueType](name, key string, defaultValue, expectedValue T) {
	Specify(name, func() {
		v := global.Get(key, defaultValue)
		Expect(v).To(Equal(expectedValue))
	})
}

var _ = Describe("Global", func() {
	configMap := &corev1.ConfigMap{
		Data: map[string]string{
			stringKey:   stringValue,
			intKey:      strconv.Itoa(intValue),
			uint16Key:   strconv.FormatUint(uint64(uint16Value), 10),
			uint32Key:   strconv.FormatUint(uint64(uint32Value), 10),
			uint64Key:   strconv.FormatUint(uint64Value, 10),
			boolKey:     strconv.FormatBool(boolValue),
			durationKey: durationValue.String(),
		},
	}

	DescribeTableSubtree("",
		func(configMaps ...*corev1.ConfigMap) {
			BeforeEach(func() {
				global.Init(configMaps...)
			})

			Context("Get should return the value for an existing", func() {
				runTestCase(stringKey, stringKey, defaultStringValue, stringValue)
				runTestCase(intKey, intKey, defaultIntValue, intValue)
				runTestCase(uint16Key, uint16Key, defaultUint16Value, uint16Value)
				runTestCase(uint32Key, uint32Key, defaultUint32Value, uint32Value)
				runTestCase(uint64Key, uint64Key, defaultUint64Value, uint64Value)
				runTestCase(boolKey, boolKey, defaultBoolValue, boolValue)
				runTestCase(durationKey, durationKey, defaultDurationValue, durationValue)
			})

			Context("Get should return the default for a non-existent", func() {
				runTestCase(stringKey, nonExistent, defaultStringValue, defaultStringValue)
				runTestCase(intKey, nonExistent, defaultIntValue, defaultIntValue)
				runTestCase(uint16Key, nonExistent, defaultUint16Value, defaultUint16Value)
				runTestCase(uint32Key, nonExistent, defaultUint32Value, defaultUint32Value)
				runTestCase(uint64Key, nonExistent, defaultUint64Value, defaultUint64Value)
				runTestCase(boolKey, nonExistent, defaultBoolValue, defaultBoolValue)
				runTestCase(durationKey, nonExistent, defaultDurationValue, defaultDurationValue)
			})
		},
		Entry("initialized with first ConfigMap nil", nil, configMap),
		Entry("initialized with last ConfigMap nil", configMap, nil),
	)

	Context("Get should return the default for an invalid value", func() {
		BeforeEach(func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					intKey:      "invalid",
					uint16Key:   "invalid",
					uint32Key:   "invalid",
					uint64Key:   "invalid",
					boolKey:     "invalid",
					durationKey: "invalid",
				},
			})
		})

		runTestCase(intKey, intKey, defaultIntValue, defaultIntValue)
		runTestCase(uint16Key, uint16Key, defaultUint16Value, defaultUint16Value)
		runTestCase(uint32Key, uint32Key, defaultUint32Value, defaultUint32Value)
		runTestCase(uint64Key, uint64Key, defaultUint64Value, defaultUint64Value)
		runTestCase(boolKey, boolKey, defaultBoolValue, defaultBoolValue)
		runTestCase(durationKey, durationKey, defaultDurationValue, defaultDurationValue)
	})
})
