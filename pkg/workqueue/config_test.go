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

package workqueue_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/global"
	"github.com/submariner-io/admiral/pkg/workqueue"
	corev1 "k8s.io/api/core/v1"
)

var customConfig = workqueue.Config{
	ItemRateLimiterBaseDelay:     time.Millisecond * 10,
	ItemRateLimiterMaxDelay:      time.Minute,
	OverallRateLimiterMaxDelay:   time.Hour,
	BucketRateLimiterItemsPerSec: 99,
	BucketRateLimiterMaxBurst:    9999,
	MaxVerbosity:                 2,
}

var _ = Describe("DefaultConfigIfNil", func() {
	Specify("should return the config when non-nil", func() {
		Expect(workqueue.DefaultConfigIfNil(&customConfig)).To(Equal(customConfig))
	})

	Specify("should return the default when passed nil", func() {
		Expect(workqueue.DefaultConfigIfNil(nil)).To(Equal(workqueue.DefaultConfig()))
	})
})

var _ = Describe("ConfigFromGlobal", func() {
	const keyPrefix = "pods"

	mustConfigFromGlobal := func(kp string, dc *workqueue.Config) workqueue.Config {
		c := workqueue.ConfigFromGlobal(kp, dc)
		Expect(c).ToNot(BeNil())

		return *c
	}

	When("there's no global config settings", func() {
		Context("and the specified custom default Config is nil", func() {
			It("should return a Config the custom settings", func() {
				Expect(mustConfigFromGlobal(keyPrefix, nil)).To(Equal(workqueue.DefaultConfig()))
			})
		})

		Context("and the specified custom default Config is non-nil", func() {
			It("should return a Config with the custom settings", func() {
				Expect(mustConfigFromGlobal(keyPrefix, &customConfig)).To(Equal(customConfig))
			})
		})
	})

	When("all settings are specified in the global config", func() {
		It("should return a Config with the global settings", func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterBaseDelayKey):     "20ms",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterMaxDelayKey):      "40s",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.OverallRateLimiterMaxDelayKey):   "2h",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterItemsPerSecKey): "99",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterMaxBurstKey):    "999",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.MaxVerbosityKey):                 "2",
				},
			})
			Expect(mustConfigFromGlobal(keyPrefix, nil)).To(Equal(workqueue.Config{
				ItemRateLimiterBaseDelay:     time.Millisecond * 20,
				ItemRateLimiterMaxDelay:      time.Second * 40,
				OverallRateLimiterMaxDelay:   time.Hour * 2,
				BucketRateLimiterItemsPerSec: 99,
				BucketRateLimiterMaxBurst:    999,
				MaxVerbosity:                 2,
			}))
		})
	})

	When("some settings are specified in the global config", func() {
		BeforeEach(func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterBaseDelayKey):     "20ms",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterItemsPerSecKey): "99",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.MaxVerbosityKey):                 "2",
				},
			})
		})

		Context("and the specified custom default Config is nil", func() {
			It("should return a Config with the default settings merged with the global settings", func() {
				Expect(mustConfigFromGlobal(keyPrefix, nil)).To(Equal(workqueue.Config{
					ItemRateLimiterBaseDelay:     time.Millisecond * 20,
					ItemRateLimiterMaxDelay:      workqueue.DefaultConfig().ItemRateLimiterMaxDelay,
					OverallRateLimiterMaxDelay:   workqueue.DefaultConfig().OverallRateLimiterMaxDelay,
					BucketRateLimiterItemsPerSec: 99,
					BucketRateLimiterMaxBurst:    workqueue.DefaultConfig().BucketRateLimiterMaxBurst,
					MaxVerbosity:                 2,
				}))
			})
		})

		Context("and the specified custom default Config is non-nil", func() {
			It("should return a Config with the custom settings merged with the global settings", func() {
				Expect(mustConfigFromGlobal(keyPrefix, &customConfig)).To(Equal(workqueue.Config{
					ItemRateLimiterBaseDelay:     time.Millisecond * 20,
					ItemRateLimiterMaxDelay:      customConfig.ItemRateLimiterMaxDelay,
					OverallRateLimiterMaxDelay:   customConfig.OverallRateLimiterMaxDelay,
					BucketRateLimiterItemsPerSec: 99,
					BucketRateLimiterMaxBurst:    customConfig.BucketRateLimiterMaxBurst,
					MaxVerbosity:                 2,
				}))
			})
		})
	})
})
