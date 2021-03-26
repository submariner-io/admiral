/*
© 2020 Red Hat, Inc.

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
	"github.com/kelseyhightower/envconfig"
)

type brokerSpecification struct {
	APIServer       string
	APIServerToken  string
	RemoteNamespace string
	Insecure        bool `default:"false"`
	Ca              string
}

func getBrokerSpecification() (*brokerSpecification, error) {
	brokerSpec := brokerSpecification{}

	err := envconfig.Process("broker_k8s", &brokerSpec)
	if err != nil {
		return nil, err
	}

	return &brokerSpec, nil
}
