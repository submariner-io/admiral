package kubefed

import (
	"github.com/submariner-io/admiral/pkg/federate"
	"k8s.io/client-go/rest"
)

type federator struct {
	kubeConfig *rest.Config
}

// New creates a new kubefed Federator instance.
func New(kubeConfig *rest.Config) (federate.Federator, error) {
	return &federator{
		kubeConfig: kubeConfig,
	}, nil
}
