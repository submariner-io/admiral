package kubefed

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestFederator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Federator: KubeFed Suite")
}
