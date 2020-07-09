package fake

import (
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

type Federator struct {
	distribute       chan runtime.Object
	delete           chan runtime.Object
	FailOnDistribute error
	FailOnDelete     error
	ResetOnFailure   bool
}

func New() *Federator {
	return &Federator{
		distribute:     make(chan runtime.Object, 100),
		delete:         make(chan runtime.Object, 100),
		ResetOnFailure: true,
	}
}

func (f *Federator) Distribute(resource runtime.Object) error {
	err := f.FailOnDistribute
	if err != nil {
		if f.ResetOnFailure {
			f.FailOnDistribute = nil
		}
		return err
	}

	f.distribute <- resource
	return nil
}

func (f *Federator) Delete(resource runtime.Object) error {
	err := f.FailOnDelete
	if err != nil {
		if f.ResetOnFailure {
			f.FailOnDelete = nil
		}
		return err
	}

	f.delete <- resource
	return nil
}

func (f *Federator) VerifyDistribute(expected runtime.Object) {
	Eventually(f.distribute, 5).Should(Receive(Equal(expected)), "Distribute was not called")
}

func (f *Federator) VerifyNoDistribute() {
	Consistently(f.distribute, 300*time.Millisecond).ShouldNot(Receive(), "Distribute was unexpectedly called")
}

func (f *Federator) VerifyDelete(expected runtime.Object) {
	Eventually(f.delete, 5).Should(Receive(Equal(expected)), "Delete was not called")
}

func (f *Federator) VerifyNoDelete() {
	Consistently(f.delete, 300*time.Millisecond).ShouldNot(Receive(), "Delete was unexpectedly called")
}
