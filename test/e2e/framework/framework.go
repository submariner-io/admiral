package framework

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/submariner-io/admiral/pkg/federate"
)

const (
	// Polling interval while trying to create objects
	PollInterval = 100 * time.Millisecond
)

const (
	Cluster1 = iota
	Cluster2
)

type ClusterIndexes []int

// Cluster contains the list of indexes of all clusters that are involved in the E2E testing
var Clusters = ClusterIndexes{
	Cluster1,
	Cluster2,
}

// InitFunc type holds the signature of functions used by
// control plane helpers to initalize the framework with a properly
// configured federate.Federator
type InitFunc func() Framework

// Framework supports common operations used by e2e tests; it will keep a client & a namespace for you.
// Eventual goal is to merge this with integration test framework.
type Framework interface {
	BeforeEach()
	AfterEach()

	CreateNamespace(clientSet *kubernetes.Clientset, baseName string, labels map[string]string) *v1.Namespace
	AddNamespacesToDelete(namespace ...*v1.Namespace)

	GetUniqueName() string
	GetBaseName() string
	GetTestNamespace() string
	GetKubeContexts() []string
	GetClusterClients() []*kubeclientset.Clientset
	GetClusterConfigs() []*rest.Config

	GetFederator() federate.Federator
	SetFederator(federate.Federator)
}

type frameworkWrapper struct {
	baseName string

	// Set together with creating the ClientSet and the namespace.
	// Guaranteed to be unique in the cluster even when running the same
	// test multiple times in parallel.
	uniqueName string

	clusterClients []*kubeclientset.Clientset
	clusterConfigs []*rest.Config

	skipNamespaceCreation    bool            // Whether to skip creating a namespace
	namespace                string          // Every test has a namespace at least unless creation is skipped
	namespacesToDelete       []*v1.Namespace // Some tests have more than one.
	namespaceDeletionTimeout time.Duration

	// To make sure that this framework cleans up after itself, no matter what,
	// we install a Cleanup action before each test and clear it after.  If we
	// should abort, the AfterSuite hook should run all Cleanup actions.
	cleanupHandle CleanupActionHandle

	// configuration for framework's client
	options Options

	// federator stores the federator that will be initialized before the test
	federator federate.Federator
}

// Options is a struct for managing test framework options.
type Options struct {
	ClientQPS    float32
	ClientBurst  int
	GroupVersion *schema.GroupVersion
}

// DefaultOptions are the options when invoking a NewDefaultFramework()
var DefaultOptions Options = Options{
	ClientQPS:   20,
	ClientBurst: 50,
}

// NewDefaultFramework makes a new framework and sets up a BeforeEach/AfterEach for
// you (you can write additional before/after each functions).
func NewDefaultFramework(baseName string) Framework {
	return NewFramework(baseName, DefaultOptions)
}

// NewFramework creates a test framework.
func NewFramework(baseName string, options Options) Framework {
	f := &frameworkWrapper{
		baseName: baseName,
		options:  options,
	}

	ginkgo.BeforeEach(f.BeforeEach)
	ginkgo.AfterEach(f.AfterEach)

	return f
}

func (f *frameworkWrapper) BeforeEach() {
	// workaround for a bug in ginkgo.
	// https://github.com/onsi/ginkgo/issues/222
	f.cleanupHandle = AddCleanupAction(f.AfterEach)

	Logf("Creating kubernetes clients")
	for _, context := range TestContext.KubeContexts {
		client, config := f.createKubernetesClient(context)
		f.clusterClients = append(f.clusterClients, client)
		f.clusterConfigs = append(f.clusterConfigs, config)
	}

	if !f.skipNamespaceCreation {
		Logf(fmt.Sprintf("Building namespace api objects, basename %s", f.baseName))

		namespaceLabels := map[string]string{
			"e2e-framework": f.baseName,
		}

		for idx, clientSet := range f.clusterClients {
			switch idx {
			case Cluster1: // On the first cluster we let k8s generate a name for the namespace
				namespace := generateNamespace(clientSet, f.baseName, namespaceLabels)
				f.namespace = namespace.GetName()
				f.uniqueName = namespace.GetName()
			default: // On the other clusters we use the same name to make tracing easier
				f.CreateNamespace(clientSet, f.namespace, namespaceLabels)
			}
		}
	} else {
		f.uniqueName = string(uuid.NewUUID())
	}

}

func (f *frameworkWrapper) createKubernetesClient(context string) (*kubeclientset.Clientset, *rest.Config) {

	restConfig, _, err := loadConfig(TestContext.KubeConfig, context)
	if err != nil {
		Errorf("Unable to load kubeconfig file %s for context %s, this is a non-recoverable error",
			TestContext.KubeConfig, context)
		Errorf("loadConfig err: %s", err.Error())
		os.Exit(1)
	}

	testDesc := ginkgo.CurrentGinkgoTestDescription()
	if len(testDesc.ComponentTexts) > 0 {
		componentTexts := strings.Join(testDesc.ComponentTexts, " ")
		restConfig.UserAgent = fmt.Sprintf(
			"%v -- %v",
			rest.DefaultKubernetesUserAgent(),
			componentTexts)
	}

	restConfig.QPS = f.options.ClientQPS
	restConfig.Burst = f.options.ClientBurst
	if f.options.GroupVersion != nil {
		restConfig.GroupVersion = f.options.GroupVersion
	}
	clientSet, err := kubeclientset.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred())

	// create scales getter, set GroupVersion and NegotiatedSerializer to default values
	// as they are required when creating a REST client.
	if restConfig.GroupVersion == nil {
		restConfig.GroupVersion = &schema.GroupVersion{}
	}
	if restConfig.NegotiatedSerializer == nil {
		restConfig.NegotiatedSerializer = scheme.Codecs
	}
	return clientSet, restConfig
}

func deleteNamespace(client kubeclientset.Interface, namespaceName string) error {

	return client.CoreV1().Namespaces().Delete(
		namespaceName,
		&metav1.DeleteOptions{})

}

// AfterEach deletes the namespace, after reading its events.
func (f *frameworkWrapper) AfterEach() {
	RemoveCleanupAction(f.cleanupHandle)

	// DeleteNamespace at the very end in defer, to avoid any
	// expectation failures preventing deleting the namespace.
	defer func() {
		nsDeletionErrors := map[string][]error{}
		// Whether to delete namespace is determined by 3 factors: delete-namespace flag, delete-namespace-on-failure flag and the test result
		// if delete-namespace set to false, namespace will always be preserved.
		// if delete-namespace is true and delete-namespace-on-failure is false, namespace will be preserved if test failed.
		if TestContext.DeleteNamespace && (TestContext.DeleteNamespaceOnFailure || !ginkgo.CurrentGinkgoTestDescription().Failed) {
			for _, ns := range f.namespacesToDelete {
				Logf(fmt.Sprintf("Destroying namespace %s for this suite on all clusters.", ns.Name))
				if errors := f.deleteNamespaceFromAllClusters(ns); errors != nil {
					nsDeletionErrors[ns.Name] = errors
				}
			}
		} else {
			if !TestContext.DeleteNamespace {
				Logf("Found DeleteNamespace=false, skipping namespace deletion!")
			} else {
				Logf("Found DeleteNamespaceOnFailure=false and current test failed, skipping namespace deletion!")
			}
		}

		// Paranoia-- prevent reuse!
		f.namespace = ""
		f.clusterClients = nil
		f.clusterConfigs = nil
		f.namespacesToDelete = nil

		// if we had errors deleting, report them now.
		if len(nsDeletionErrors) != 0 {
			messages := []string{}
			for namespaceKey, namespaceErrors := range nsDeletionErrors {
				for clusterIdx, namespaceErr := range namespaceErrors {
					messages = append(messages, fmt.Sprintf("Couldn't delete ns: %q (@cluster %d): %s (%#v)",
						namespaceKey, clusterIdx, namespaceErr, namespaceErr))
				}
			}
			Failf(strings.Join(messages, ","))
		}
	}()

}

func (f *frameworkWrapper) deleteNamespaceFromAllClusters(ns *v1.Namespace) []error {
	var errors []error
	for _, clientSet := range f.clusterClients {
		if err := deleteNamespace(clientSet, ns.Name); err != nil {
			switch {
			case apierrors.IsNotFound(err):
				Logf("Namespace %v was already deleted", ns.Name)
			case apierrors.IsConflict(err):
				Logf("Namespace %v scheduled for deletion, resources being purged", ns.Name)
			default:
				Logf("Failed deleting namespace: %v", err)
				errors = append(errors, err)
			}
		}
	}
	return errors
}

// CreateNamespace creates a namespace for e2e testing.
func (f *frameworkWrapper) CreateNamespace(clientSet *kubeclientset.Clientset,
	baseName string, labels map[string]string) *v1.Namespace {

	ns := createTestNamespace(clientSet, baseName, labels)
	f.AddNamespacesToDelete(ns)
	return ns
}

func (f *frameworkWrapper) AddNamespacesToDelete(namespaces ...*v1.Namespace) {
	for _, ns := range namespaces {
		if ns == nil {
			continue
		}
		f.namespacesToDelete = append(f.namespacesToDelete, ns)
	}
}

func (f *frameworkWrapper) GetTestNamespace() string {
	return f.namespace
}

func (_ *frameworkWrapper) GetKubeContexts() []string {
	return TestContext.KubeContexts
}

func (f *frameworkWrapper) GetClusterClients() []*kubeclientset.Clientset {
	return f.clusterClients
}

func (f *frameworkWrapper) GetClusterConfigs() []*rest.Config {
	return f.clusterConfigs
}

func (f *frameworkWrapper) GetUniqueName() string {
	return f.uniqueName
}

func (f *frameworkWrapper) GetBaseName() string {
	return f.baseName
}

func (f *frameworkWrapper) GetFederator() federate.Federator {
	return f.federator
}

func (f *frameworkWrapper) SetFederator(federator federate.Federator) {
	f.federator = federator
}

func generateNamespace(client kubeclientset.Interface, baseName string, labels map[string]string) *v1.Namespace {
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("e2e-tests-%v-", baseName),
			Labels:       labels,
		},
	}

	namespace, err := client.CoreV1().Namespaces().Create(namespaceObj)
	Expect(err).NotTo(HaveOccurred(), "Error generating namespace %v", namespaceObj)
	return namespace
}

func createTestNamespace(client kubeclientset.Interface, name string, labels map[string]string) *v1.Namespace {
	Logf(fmt.Sprintf("Creating a namespace %s to execute the test in", name))
	namespace := createNamespace(client, name, labels)
	return namespace
}

func createNamespace(client kubeclientset.Interface, name string, labels map[string]string) *v1.Namespace {
	namespaceObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}

	namespace, err := client.CoreV1().Namespaces().Create(namespaceObj)
	Expect(err).NotTo(HaveOccurred(), "Error creating namespace %v", namespaceObj)
	return namespace
}
