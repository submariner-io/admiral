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

package broker

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/pkg/errors"
)

type brokerSpecification struct {
	APIServer       string
	APIServerToken  string
	RemoteNamespace string
	Insecure        bool `default:"false"`
	Ca              string
	Secret          string
}

const brokerConfigPrefix = "broker_k8s"

func getBrokerSpecification() (*brokerSpecification, error) {
	brokerSpec := brokerSpecification{}

	err := envconfig.Process(brokerConfigPrefix, &brokerSpec)
	if err != nil {
		return nil, errors.Wrap(err, "error processing env configuration")
	}

	return &brokerSpec, nil
}

func EnvironmentVariable(setting string) string {
	// Check the setting is known (ignoring case)
	s := reflect.ValueOf(&brokerSpecification{})
	t := s.Elem().Type()

	for i := 0; i < t.NumField(); i++ {
		if strings.EqualFold(t.Field(i).Name, strings.ToLower(setting)) {
			return strings.ToUpper(fmt.Sprintf("%s_%s", brokerConfigPrefix, setting))
		}
	}

	panic(fmt.Sprintf("unknown Broker setting %s", setting))
}

func SecretPath(secretName string) string {
	return fmt.Sprintf("/run/secrets/submariner.io/%s", secretName)
}
