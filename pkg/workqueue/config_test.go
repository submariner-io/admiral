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
	"github.com/submariner-io/admiral/pkg/workqueue"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var customConfig = workqueue.Config{
	ItemRateLimiterBaseDelay:     time.Millisecond * 10,
	ItemRateLimiterMaxDelay:      time.Minute,
	OverallRateLimiterMaxDelay:   time.Hour,
	BucketRateLimiterItemsPerSec: 99,
	BucketRateLimiterMaxBurst:    9999,
}

var _ = Describe("DefaultConfigIfNil", func() {
	Specify("should return the config when non-nil", func() {
		Expect(workqueue.DefaultConfigIfNil(&customConfig)).To(Equal(customConfig))
	})

	Specify("should return the default when passed nil", func() {
		Expect(workqueue.DefaultConfigIfNil(nil)).To(Equal(workqueue.DefaultConfig()))
	})
})

var _ = Describe("ConfigFromConfigMap", func() {
	const keyPrefix = "pods"

	var configMap *corev1.ConfigMap

	BeforeEach(func() {
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "workqueue-cm",
				Namespace: "my-ns",
			},
			Data: map[string]string{},
		}
	})

	mustConfigFromConfigMap := func(cm *corev1.ConfigMap, kp string, dc *workqueue.Config) workqueue.Config {
		c := workqueue.ConfigFromConfigMap(cm, kp, dc)
		Expect(c).ToNot(BeNil())

		return *c
	}

	When("the specified ConfigMap is nil", func() {
		Context("and the specified default Config is nil", func() {
			It("should return nil", func() {
				Expect(workqueue.ConfigFromConfigMap(nil, keyPrefix, nil)).To(BeNil())
			})
		})

		Context("and the specified default Config is non-nil", func() {
			It("should return the default Config", func() {
				Expect(mustConfigFromConfigMap(nil, keyPrefix, &customConfig)).To(Equal(customConfig))
			})
		})
	})

	When("the specified ConfigMap is non-nil", func() {
		It("should return a Config derived from the Data map", func() {
			configMap.Data = map[string]string{
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterBaseDelayKey):     "20ms",
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterMaxDelayKey):      "40s",
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.OverallRateLimiterMaxDelayKey):   "2h",
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterItemsPerSecKey): "99",
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterMaxBurstKey):    "999",
				workqueue.ToConfigMapDataKey(keyPrefix, workqueue.MaxVerbosityKey):                 "2",
				workqueue.ToConfigMapDataKey("other", workqueue.BucketRateLimiterMaxBurstKey):      "888",
			}

			Expect(mustConfigFromConfigMap(configMap, keyPrefix, nil)).To(Equal(workqueue.Config{
				ItemRateLimiterBaseDelay:     time.Millisecond * 20,
				ItemRateLimiterMaxDelay:      time.Second * 40,
				OverallRateLimiterMaxDelay:   time.Hour * 2,
				BucketRateLimiterItemsPerSec: 99,
				BucketRateLimiterMaxBurst:    999,
				MaxVerbosity:                 2,
			}))
		})

		Context("and contains only some settings in the Data map", func() {
			It("should return a Config with ConfigMap settings merged with the defaults", func() {
				configMap.Data = map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterMaxDelayKey):      "40s",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.OverallRateLimiterMaxDelayKey):   "2h",
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterItemsPerSecKey): "99",
				}

				Expect(mustConfigFromConfigMap(configMap, keyPrefix, &workqueue.Config{
					ItemRateLimiterBaseDelay:  time.Millisecond * 33,
					ItemRateLimiterMaxDelay:   time.Minute,
					BucketRateLimiterMaxBurst: 888,
				})).To(Equal(workqueue.Config{
					ItemRateLimiterBaseDelay:     time.Millisecond * 33,
					ItemRateLimiterMaxDelay:      time.Second * 40,
					OverallRateLimiterMaxDelay:   time.Hour * 2,
					BucketRateLimiterItemsPerSec: 99,
					BucketRateLimiterMaxBurst:    888,
				}))

				Expect(mustConfigFromConfigMap(configMap, keyPrefix, nil)).To(Equal(workqueue.Config{
					ItemRateLimiterBaseDelay:     workqueue.DefaultConfig().ItemRateLimiterBaseDelay,
					ItemRateLimiterMaxDelay:      time.Second * 40,
					OverallRateLimiterMaxDelay:   time.Hour * 2,
					BucketRateLimiterItemsPerSec: 99,
					BucketRateLimiterMaxBurst:    workqueue.DefaultConfig().BucketRateLimiterMaxBurst,
					MaxVerbosity:                 0,
				}))
			})
		})

		Context("and there's no settings in the Data map", func() {
			It("should return a Config with the defaults", func() {
				Expect(mustConfigFromConfigMap(configMap, keyPrefix, nil)).To(Equal(workqueue.DefaultConfig()))
			})
		})

		Context("and there's invalid values in the Data map", func() {
			It("should panic", func() {
				configMap.Data = map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.ItemRateLimiterBaseDelayKey): "2oms",
				}

				Expect(func() {
					mustConfigFromConfigMap(configMap, keyPrefix, nil)
				}).To(Panic())

				configMap.Data = map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.OverallRateLimiterMaxDelayKey): "2j",
				}

				Expect(func() {
					mustConfigFromConfigMap(configMap, keyPrefix, nil)
				}).To(Panic())

				configMap.Data = map[string]string{
					workqueue.ToConfigMapDataKey(keyPrefix, workqueue.BucketRateLimiterItemsPerSecKey): "invalid",
				}

				Expect(func() {
					mustConfigFromConfigMap(configMap, keyPrefix, nil)
				}).To(Panic())
			})
		})
	})
})
