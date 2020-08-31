package syncer

import "k8s.io/apimachinery/pkg/runtime"

type Interface interface {
	Start(stopCh <-chan struct{}) error

	AwaitStopped()

	GetResource(name, namespace string) (runtime.Object, bool, error)
}
