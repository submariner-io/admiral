package api

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/test/e2e/api/kubefed"
	"github.com/submariner-io/admiral/test/e2e/framework"
)

var controlPlanes = map[string]framework.InitFunc{
	kubefed.Name: kubefed.InitTestFramework,
}

var _ = Describe("E2E tests to validate Admiral", func() {
	for name, initFramework := range controlPlanes {
		When(fmt.Sprintf("using the %s control plane", name), func() {
			testAPI(initFramework)
		})
	}
})

func testAPI(initFramework framework.InitFunc) {
	var (
		federator federate.Federator
		testPod   *v1.Pod
		contexts  []string
	)
	f := initFramework()

	BeforeEach(func() {
		federator = f.GetFederator()
		testPod = newPod("api-tester", f.GetTestNamespace())
		contexts = f.GetKubeContexts()
	})

	AfterEach(func() {
		federator = nil
		testPod = nil
		contexts = nil
	})

	testResourceAPIOperations := func(clusters ...string) {
		By("Distributing")
		err := federator.Distribute(testPod, clusters...)
		Expect(err).ToNot(HaveOccurred())

		By("[slow] Checking correct distribution")
		presence := framework.BuildPresenceMap(contexts, clusters...)
		framework.AssertPodInClusters(f, testPod, presence)

		By("Deleting")
		err = federator.Delete(testPod)
		Expect(err).ToNot(HaveOccurred())

		By("[slow] Checking correct deletion")
		// By sending an empty string means it will initialize everything to false
		presence = framework.BuildPresenceMap(contexts, "")
		framework.AssertPodInClusters(f, testPod, presence)
	}

	testResourceAPI := func(clusters ...string) {
		It("Should handle the case of empty namespaces in the targeted clusters", func() {
			testResourceAPIOperations(clusters...)
		})

		It("Should handle the case of an existing resource in one of the clusters", func() {
			client := f.GetClusterClients()[framework.Cluster2]
			podClient := client.CoreV1().Pods(f.GetTestNamespace())
			_, err := podClient.Create(testPod)
			Expect(err).ToNot(HaveOccurred())

			testResourceAPIOperations(clusters...)
		})
	}

	Context("when we want to distribute to all clusters", func() {
		testResourceAPI()
	})

	Context("when we want to distribute to cluster2 only", func() {
		testResourceAPI("cluster2")
	})
}

func newPod(name, namespace string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1.PodSpec{
			RestartPolicy: v1.RestartPolicyNever,
			Containers: []v1.Container{
				{
					Name:  "busybox",
					Image: "busybox",
					Command: []string{
						"sh",
						"-c",
						"while true; do sleep 30; done",
					},
				},
			},
		},
	}
}
