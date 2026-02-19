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
	"time"

	"github.com/submariner-io/admiral/pkg/global"
)

const (
	ItemRateLimiterBaseDelayKey     = "item-rate-limiter-base-delay"
	ItemRateLimiterMaxDelayKey      = "item-rate-limiter-max-delay"
	OverallRateLimiterMaxDelayKey   = "overall-rate-limiter-max-delay"
	BucketRateLimiterItemsPerSecKey = "bucket-rate-limiter-items-per-sec"
	BucketRateLimiterMaxBurstKey    = "bucket-rate-limiter-max-burst"
	MaxVerbosityKey                 = "max-verbosity"
	NumWorkersKey                   = "num-workers"
)

type Config struct {
	ItemRateLimiterBaseDelay     time.Duration
	ItemRateLimiterMaxDelay      time.Duration
	OverallRateLimiterMaxDelay   time.Duration
	BucketRateLimiterItemsPerSec int
	BucketRateLimiterMaxBurst    int
	MaxVerbosity                 int
	NumWorkers                   int
}

func DefaultConfig() Config {
	return Config{
		ItemRateLimiterBaseDelay:     time.Millisecond,
		ItemRateLimiterMaxDelay:      30 * time.Second,
		OverallRateLimiterMaxDelay:   5 * time.Minute,
		BucketRateLimiterItemsPerSec: 10,
		BucketRateLimiterMaxBurst:    500,
		NumWorkers:                   1,
	}
}

func DefaultConfigIfNil(c *Config) Config {
	if c == nil {
		return DefaultConfig()
	}

	return *c
}

func ConfigFromGlobal(keyPrefix string, defaultConfig *Config) *Config {
	config := DefaultConfigIfNil(defaultConfig)
	config.ItemRateLimiterBaseDelay = global.Get(ToConfigMapDataKey(keyPrefix, ItemRateLimiterBaseDelayKey),
		config.ItemRateLimiterBaseDelay)
	config.ItemRateLimiterMaxDelay = global.Get(ToConfigMapDataKey(keyPrefix, ItemRateLimiterMaxDelayKey),
		config.ItemRateLimiterMaxDelay)
	config.OverallRateLimiterMaxDelay = global.Get(ToConfigMapDataKey(keyPrefix, OverallRateLimiterMaxDelayKey),
		config.OverallRateLimiterMaxDelay)
	config.BucketRateLimiterItemsPerSec = global.Get(ToConfigMapDataKey(keyPrefix, BucketRateLimiterItemsPerSecKey),
		config.BucketRateLimiterItemsPerSec)
	config.BucketRateLimiterMaxBurst = global.Get(ToConfigMapDataKey(keyPrefix, BucketRateLimiterMaxBurstKey),
		config.BucketRateLimiterMaxBurst)
	config.MaxVerbosity = global.Get(ToConfigMapDataKey(keyPrefix, MaxVerbosityKey), config.MaxVerbosity)
	config.NumWorkers = global.Get(ToConfigMapDataKey(keyPrefix, NumWorkersKey), config.NumWorkers)

	return &config
}

func ToConfigMapDataKey(prefix, name string) string {
	return fmt.Sprintf("%s.workqueue/%s", prefix, name)
}
