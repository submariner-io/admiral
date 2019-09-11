package kubefed

import (
	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	apikubefed "github.com/submariner-io/admiral/pkg/federate/kubefed"
	"github.com/submariner-io/admiral/test/e2e/framework"
	"github.com/submariner-io/admiral/test/e2e/framework/kubefed"
)

const Name = "KubeFed"

func InitTestFramework() framework.Framework {
	f := kubefed.NewDefaultFederatedFramework("framework-kubefed")

	var stopCh chan struct{}

	ginkgo.BeforeEach(func() {
		stopCh = make(chan struct{})
		federator, err := apikubefed.New(
			f.GetClusterConfigs()[framework.Cluster1],
			stopCh,
		)
		Expect(err).ToNot(HaveOccurred())
		f.SetFederator(federator)
	})

	ginkgo.AfterEach(func() {
		close(stopCh)
		f.SetFederator(nil)
	})

	return f
}
