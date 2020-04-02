package broker_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
)

const brokerNamespace = "broker-ns"
const sourceNamespace = "source-ns"

var _ = Describe("Federator", func() {
	klog.InitFlags(nil)

	Describe("Distribute", testDistribute)
	Describe("Delete", testDelete)
})

func testDistribute() {
	var (
		f              federate.Federator
		resource       *corev1.Pod
		localClusterID string
		resourceClient *fake.DynamicResourceClient
		initObjs       []runtime.Object
	)

	BeforeEach(func() {
		localClusterID = "east"
		resource = newPod()
		initObjs = nil
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, localClusterID)
	})

	When("the resource does not already exist in the broker datastore", func() {
		When("a local cluster ID is specified", func() {
			It("should create the resource with the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				verifyResource(resourceClient, resource, localClusterID)
			})
		})

		When("a local cluster ID is not specified", func() {
			BeforeEach(func() {
				localClusterID = ""
			})

			It("should create the resource without the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				verifyResource(resourceClient, resource, localClusterID)
			})
		})

		When("creation fails", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnCreate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(resource)).ToNot(Succeed())
			})
		})
	})

	When("the resource already exists in the broker datastore", func() {
		BeforeEach(func() {
			resource.SetNamespace(brokerNamespace)
			resource.SetUID(uuid.NewUUID())
			resource.SetResourceVersion("1")
			resource.GetLabels()[federate.ClusterIDLabelKey] = localClusterID
			initObjs = append(initObjs, resource)

			resource = newPodWithImage("apache")
		})

		It("should update the resource", func() {
			Expect(f.Distribute(resource)).To(Succeed())
			verifyResource(resourceClient, resource, localClusterID)
		})

		When("update initially fails due to conflict", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake"))
			})

			It("should retry until it succeeds", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				verifyResource(resourceClient, resource, localClusterID)
			})
		})

		When("update fails not due to conflict", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnUpdate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(resource)).ToNot(Succeed())
			})
		})
	})

	When("retrieval to find an existing resource in the broker datastore fails", func() {
		JustBeforeEach(func() {
			resourceClient.FailOnGet = apierrors.NewServiceUnavailable("fake")
		})

		It("should return an error", func() {
			Expect(f.Distribute(resource)).ToNot(Succeed())
		})
	})
}

func testDelete() {
	var (
		f              federate.Federator
		resource       *corev1.Pod
		resourceClient *fake.DynamicResourceClient
		initObjs       []runtime.Object
	)

	BeforeEach(func() {
		resource = newPod()
		initObjs = nil
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, "")
	})

	When("the resource exists in the broker datastore", func() {
		BeforeEach(func() {
			existing := resource.DeepCopy()
			existing.SetNamespace(brokerNamespace)
			initObjs = append(initObjs, existing)
		})

		It("should delete the resource", func() {
			Expect(f.Delete(resource)).To(Succeed())

			_, err := getResource(resourceClient, resource)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		When("delete fails", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnDelete = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Delete(resource)).ToNot(Succeed())
			})
		})
	})

	When("the resource does not exist in the broker datastore", func() {
		It("should return NotFound error", func() {
			Expect(apierrors.IsNotFound(f.Delete(resource))).To(BeTrue())
		})
	})
}

func setupFederator(resource *corev1.Pod, initObjs []runtime.Object, localClusterID string) (federate.Federator, *fake.DynamicResourceClient) {
	gvks, _, err := scheme.Scheme.ObjectKinds(resource)
	Expect(err).To(Succeed())
	Expect(gvks).ToNot(HaveLen(0))
	gvk := gvks[0]

	dynClient := fake.NewDynamicClient(initObjs...)

	restMapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{gvk.GroupVersion()})
	restMapper.Add(gvk, meta.RESTScopeNamespace)

	f := broker.NewTestFederator(dynClient, restMapper, brokerNamespace, localClusterID)

	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	Expect(err).To(Succeed())

	return f, dynClient.Resource(mapping.Resource).Namespace(brokerNamespace).(*fake.DynamicResourceClient)
}
