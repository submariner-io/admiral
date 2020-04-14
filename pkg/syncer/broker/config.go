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

func BuildBrokerConfigFromEnv() (*rest.Config, string, error) {
	brokerSpec := brokerSpecification{}
	err := envconfig.Process("broker_k8s", &brokerSpec)
	if err != nil {
		return nil, "", err
	}

	tlsClientConfig := rest.TLSClientConfig{}
	if brokerSpec.Insecure {
		tlsClientConfig.Insecure = true
	} else {
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
