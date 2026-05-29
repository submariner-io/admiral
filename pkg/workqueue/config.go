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

package workqueue

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

const (
	ItemRateLimiterBaseDelayKey     = "item-rate-limiter-base-delay"
	ItemRateLimiterMaxDelayKey      = "item-rate-limiter-max-delay"
	OverallRateLimiterMaxDelayKey   = "overall-rate-limiter-max-delay"
	BucketRateLimiterItemsPerSecKey = "bucket-rate-limiter-items-per-sec"
	BucketRateLimiterMaxBurstKey    = "bucket-rate-limiter-max-burst"
	MaxVerbosityKey                 = "max-verbosity"
)

type Config struct {
	ItemRateLimiterBaseDelay     time.Duration
	ItemRateLimiterMaxDelay      time.Duration
	OverallRateLimiterMaxDelay   time.Duration
	BucketRateLimiterItemsPerSec int
	BucketRateLimiterMaxBurst    int
	MaxVerbosity                 int
}

func DefaultConfig() Config {
	return Config{
		ItemRateLimiterBaseDelay:     time.Millisecond,
		ItemRateLimiterMaxDelay:      30 * time.Second,
		OverallRateLimiterMaxDelay:   5 * time.Minute,
		BucketRateLimiterItemsPerSec: 10,
		BucketRateLimiterMaxBurst:    500,
	}
}

func DefaultConfigIfNil(c *Config) Config {
	if c == nil {
		return DefaultConfig()
	}

	return *c
}

func ConfigFromConfigMap(configMap *corev1.ConfigMap, keyPrefix string, defaultConfig *Config) *Config {
	if configMap == nil {
		return defaultConfig
	}

	config := DefaultConfigIfNil(defaultConfig)

	keyPrefix = ToConfigMapDataKey(keyPrefix, "")

	var err error

	for k, v := range configMap.Data {
		if !strings.HasPrefix(k, keyPrefix) {
			continue
		}

		switch strings.Split(k, keyPrefix)[1] {
		case ItemRateLimiterBaseDelayKey:
			config.ItemRateLimiterBaseDelay, err = time.ParseDuration(v)
			utilruntime.Must(err)
		case ItemRateLimiterMaxDelayKey:
			config.ItemRateLimiterMaxDelay, err = time.ParseDuration(v)
			utilruntime.Must(err)
		case OverallRateLimiterMaxDelayKey:
			config.OverallRateLimiterMaxDelay, err = time.ParseDuration(v)
			utilruntime.Must(err)
		case BucketRateLimiterItemsPerSecKey:
			config.BucketRateLimiterItemsPerSec, err = strconv.Atoi(v)
			utilruntime.Must(err)
		case BucketRateLimiterMaxBurstKey:
			config.BucketRateLimiterMaxBurst, err = strconv.Atoi(v)
			utilruntime.Must(err)
		case MaxVerbosityKey:
			config.MaxVerbosity, err = strconv.Atoi(v)
			utilruntime.Must(err)
		}
	}

	return &config
}

func ToConfigMapDataKey(prefix, name string) string {
	return fmt.Sprintf("%s.workqueue.%s", prefix, name)
}
