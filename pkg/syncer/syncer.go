package syncer

type Interface interface {
	Start(stopCh <-chan struct{}) error
}
