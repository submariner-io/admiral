package kubefed

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/submariner-io/admiral/test/e2e/framework"
)

const federateCmd = "kubefedctl federate namespace %s --host-cluster-context=%s"

// FederatedFramework expands the E2E framework.Framework to include KubeFed related methods
type FederatedFramework struct {
	framework.Framework
}

// NewDefaultFederatedFramework makes a new framework and sets up a
// BeforeEach/AfterEach for you (you can write additional before/after
// each functions).
func NewDefaultFederatedFramework(baseName string) *FederatedFramework {
	return NewFederatedFramework(baseName, framework.DefaultOptions)
}

// NewFederatedFramework creates a test framework.
func NewFederatedFramework(baseName string, options framework.Options) *FederatedFramework {
	f := &FederatedFramework{
		framework.NewFramework(baseName, options),
	}

	ginkgo.BeforeEach(f.BeforeEach)

	return f
}

func (f *FederatedFramework) BeforeEach() {
	context := f.GetKubeContexts()[framework.Cluster1]
	namespace := f.GetTestNamespace()

	framework.Logf(fmt.Sprintf("Federating namespace %s with kubefedctl", namespace))
	args := strings.Split(fmt.Sprintf(federateCmd, namespace, context), " ")
	cmd := exec.Command(args[0], args[1:]...)
	stdoutStderr, err := cmd.CombinedOutput()
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "Federation failed: %s", stdoutStderr)
}
