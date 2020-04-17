package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	testV1 "github.com/submariner-io/admiral/test/apis/admiral.submariner.io/v1"
	_ "github.com/submariner-io/admiral/test/e2e/syncer"
	"github.com/submariner-io/shipyard/test/e2e"
	"github.com/submariner-io/shipyard/test/e2e/framework"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
)

func init() {
	klog.InitFlags(nil)

	framework.AddBeforeSuite(func() {
		Expect(testV1.AddToScheme(scheme.Scheme)).To(Succeed())
	})
}

func TestE2E(t *testing.T) {
	e2e.RunE2ETests(t)
}
