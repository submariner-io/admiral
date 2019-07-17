package kubefed

import (
	"k8s.io/apimachinery/pkg/runtime"
)

const (
//	federatedKindPrefix string = "Federated"
)

func (f *federator) Distribute(resource runtime.Object, clusterIDs []string) error {
	panic("not implemented")
}

func (f *federator) Delete(resoure runtime.Object) error {
	panic("not implemented")
}
