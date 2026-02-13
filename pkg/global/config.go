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

package global

import (
	"strconv"
	"sync"
	"time"

	"github.com/submariner-io/admiral/pkg/log"
	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type ValueType interface {
	string | int | uint16 | uint32 | uint64 | bool | time.Duration
}

const (
	K8sClientQPS         = "k8s.client/qps"
	K8sClientBurst       = "k8s.client/burst"
	K8sBrokerClientQPS   = "k8s.broker.client/qps"
	K8sBrokerClientBurst = "k8s.broker.client/burst"
)

var (
	configMap sync.Map
	logger    = log.Logger{Logger: logf.Log.WithName("Global")}
)

func Init(fromConfigMaps ...*corev1.ConfigMap) {
	configMap.Clear()

	for _, cm := range fromConfigMaps {
		if cm == nil {
			continue
		}

		for k, v := range cm.Data {
			configMap.Store(k, v)
		}
	}
}

func Get[T ValueType](name string, defaultValue T) T {
	v, exists := configMap.Load(name)
	if !exists {
		return defaultValue
	}

	var (
		returnValue any
		err         error
	)

	existing := v.(string)

	switch any(defaultValue).(type) {
	case string:
		returnValue = v
	case int:
		returnValue, err = strconv.Atoi(existing)
	case uint16:
		var u64 uint64
		u64, err = strconv.ParseUint(existing, 10, 16)
		returnValue = uint16(u64)
	case uint32:
		var u64 uint64
		u64, err = strconv.ParseUint(existing, 10, 32)
		returnValue = uint32(u64)
	case uint64:
		returnValue, err = strconv.ParseUint(existing, 10, 64)
	case bool:
		returnValue, err = strconv.ParseBool(existing)
	case time.Duration:
		returnValue, err = time.ParseDuration(existing)
	}

	if err != nil {
		logger.Errorf(err, "Failed to parse config option %q value %q - using default value %v",
			name, existing, defaultValue)

		return defaultValue
	}

	return returnValue.(T)
}
