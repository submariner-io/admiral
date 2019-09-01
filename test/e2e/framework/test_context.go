package framework

import (
	"flag"
	"os"
	"strings"

	"k8s.io/klog"
)

type contextArray []string

type TestContextType struct {
	KubeConfig               string
	KubeContexts             contextArray
	ReportDir                string
	ReportPrefix             string
	DeleteNamespace          bool
	DeleteNamespaceOnFailure bool
}

func (contexts *contextArray) String() string {
	return strings.Join(*contexts, ",")
}

func (contexts *contextArray) Set(value string) error {
	*contexts = append(*contexts, value)
	return nil
}

var TestContext *TestContextType = &TestContextType{}

func registerFlags(t *TestContextType) {
	flag.StringVar(&t.KubeConfig, "kubeconfig", os.Getenv("KUBECONFIG"),
		"Path to kubeconfig containing embedded authinfo.")
	flag.Var(&TestContext.KubeContexts, "dp-context", "kubeconfig context for dataplane clusters (use several times).")
	flag.StringVar(&TestContext.ReportPrefix, "report-prefix", "", "Optional prefix for JUnit XML reports. Default is empty, which doesn't prepend anything to the default name.")
	flag.StringVar(&TestContext.ReportDir, "report-dir", "", "Path to the directory where the JUnit XML reports should be saved. Default is empty, which doesn't generate these reports.")
	flag.BoolVar(&TestContext.DeleteNamespace, "delete-namespace", true, "If true tests will delete namespace after completion. It is only designed to make debugging easier, DO NOT turn it off by default.")
	flag.BoolVar(&TestContext.DeleteNamespaceOnFailure, "delete-namespace-on-failure", false, "If true, framework will delete test namespace on failure. Used only during test debugging.")
}

func validateFlags(t *TestContextType) {
	if len(t.KubeConfig) == 0 {
		klog.Fatalf("kubeconfig parameter or KUBECONFIG environment variable is required")
	}

	if len(t.KubeContexts) < 2 {
		klog.Fatalf("several kubernetes contexts are necessary east, west, etc..")
	}
}

func ParseFlags() {
	registerFlags(TestContext)
	flag.Parse()
	validateFlags(TestContext)
}
