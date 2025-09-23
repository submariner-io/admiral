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
	stringKey            = "string-key"
	intKey               = "int-key"
	boolKey              = "bool-key"
	durationKey          = "duration-key"
	nonExistent          = "non-existent"
	stringValue          = "string-value"
	defaultStringValue   = "default"
	intValue             = 10
	defaultIntValue      = 99
	durationValue        = time.Hour * 2
	defaultDurationValue = time.Minute * 30
)

var _ = Describe("Global", func() {
	configMap := &corev1.ConfigMap{
		Data: map[string]string{
			stringKey:   stringValue,
			intKey:      strconv.Itoa(intValue),
			boolKey:     strconv.FormatBool(true),
			durationKey: durationValue.String(),
		},
	}

	DescribeTableSubtree("",
		func(configMaps ...*corev1.ConfigMap) {
			BeforeEach(func() {
				global.Init(configMaps...)
			})

			Context("Get should return the value for an existing", func() {
				Specify("string", func() {
					v := global.Get(stringKey, defaultStringValue)
					Expect(v).To(Equal(stringValue))
				})

				Specify("int", func() {
					v := global.Get(intKey, defaultIntValue)
					Expect(v).To(Equal(intValue))
				})

				Specify("bool", func() {
					v := global.Get(boolKey, false)
					Expect(v).To(BeTrue())
				})

				Specify("time.Duration", func() {
					v := global.Get(durationKey, defaultDurationValue)
					Expect(v).To(Equal(durationValue))
				})
			})

			Context("Get should return the default for a non-existent", func() {
				Specify("string", func() {
					v := global.Get(nonExistent, defaultStringValue)
					Expect(v).To(Equal(defaultStringValue))
				})

				Specify("int", func() {
					v := global.Get(nonExistent, defaultIntValue)
					Expect(v).To(Equal(defaultIntValue))
				})

				Specify("bool", func() {
					v := global.Get(nonExistent, true)
					Expect(v).To(BeTrue())
				})

				Specify("time.Duration", func() {
					v := global.Get(nonExistent, defaultDurationValue)
					Expect(v).To(Equal(defaultDurationValue))
				})
			})
		},
		Entry("initialized with first ConfigMap nil", nil, configMap),
		Entry("initialized with last ConfigMap nil", configMap, nil),
	)

	Context("Get should return the default for an invalid", func() {
		BeforeEach(func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					intKey:      "invalid",
					boolKey:     "invalid",
					durationKey: "invalid",
				},
			})
		})

		Specify("int", func() {
			v := global.Get(intKey, defaultIntValue)
			Expect(v).To(Equal(defaultIntValue))
		})

		Specify("bool", func() {
			v := global.Get(boolKey, true)
			Expect(v).To(BeTrue())
		})

		Specify("time.Duration", func() {
			v := global.Get(durationKey, defaultDurationValue)
			Expect(v).To(Equal(defaultDurationValue))
		})
	})
})
