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
	"encoding/base64"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"k8s.io/client-go/rest"
)

type brokerSpecification struct {
	APIServer       string
	APIServerToken  string
	RemoteNamespace string
	Insecure        bool `default:"false"`
	Ca              string
}

func BuildBrokerConfigFromEnv(useTokenCAForEndpoint bool) (*rest.Config, string, error) {
	brokerSpec := brokerSpecification{}
	err := envconfig.Process("broker_k8s", &brokerSpec)
	if err != nil {
		return nil, "", err
	}

	tlsClientConfig := rest.TLSClientConfig{}
	if brokerSpec.Insecure {
		tlsClientConfig.Insecure = true
	} else if useTokenCAForEndpoint {
		caDecoded, err := base64.StdEncoding.DecodeString(brokerSpec.Ca)
		if err != nil {
			return nil, "", fmt.Errorf("error decoding CA data: %v", err)
		}
		tlsClientConfig.CAData = caDecoded
	}

	return &rest.Config{
		Host:            fmt.Sprintf("https://%s", brokerSpec.APIServer),
		TLSClientConfig: tlsClientConfig,
		BearerToken:     brokerSpec.APIServerToken,
	}, brokerSpec.RemoteNamespace, nil
}
