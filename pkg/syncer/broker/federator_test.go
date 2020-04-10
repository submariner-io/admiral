package broker_test

import (
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("Federator", func() {
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
		resource = test.NewPod(test.LocalNamespace)
		initObjs = nil
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, localClusterID)
	})

	When("the resource does not already exist in the broker datastore", func() {
		When("a local cluster ID is specified", func() {
			It("should create the resource with the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, test.RemoteNamespace, localClusterID)
			})
		})

		When("a local cluster ID is not specified", func() {
			BeforeEach(func() {
				localClusterID = ""
			})

			It("should create the resource without the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, test.RemoteNamespace, localClusterID)
			})
		})

		When("create fails", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnCreate = apierrors.NewServiceUnavailable("fake")
			})

			It("should return an error", func() {
				Expect(f.Distribute(resource)).ToNot(Succeed())
			})
		})

		When("create returns AlreadyExists error due to a simulated out-of-band create", func() {
			BeforeEach(func() {
				initObjs = append(initObjs, resource)
				resource = test.NewPodWithImage(test.LocalNamespace, "apache")
			})

			JustBeforeEach(func() {
				resourceClient.FailOnGet = apierrors.NewNotFound(schema.GroupResource{}, resource.GetName())
				resourceClient.FailOnCreate = apierrors.NewAlreadyExists(schema.GroupResource{}, resource.GetName())
			})

			It("should update the resource", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, test.RemoteNamespace, localClusterID)
			})
		})
	})

	When("the resource already exists in the broker datastore", func() {
		BeforeEach(func() {
			initObjs = append(initObjs, resource)

			resource = test.NewPodWithImage(test.LocalNamespace, "apache")
		})

		It("should update the resource", func() {
			Expect(f.Distribute(resource)).To(Succeed())
			test.VerifyResource(resourceClient, resource, test.RemoteNamespace, localClusterID)
		})

		When("update initially fails due to conflict", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake"))
			})

			It("should retry until it succeeds", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, test.RemoteNamespace, localClusterID)
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
		resource = test.NewPod(test.LocalNamespace)
		initObjs = nil
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, "")
	})

	When("the resource exists in the broker datastore", func() {
		BeforeEach(func() {
			existing := resource.DeepCopy()
			existing.SetNamespace(test.RemoteNamespace)
			initObjs = append(initObjs, existing)
		})

		It("should delete the resource", func() {
			Expect(f.Delete(resource)).To(Succeed())

			_, err := test.GetResourceAndError(resourceClient, resource)
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
	dynClient := fake.NewDynamicClient(test.PrepInitialClientObjs(test.RemoteNamespace, localClusterID, initObjs...)...)
	restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(resource)
	f := broker.NewTestFederator(dynClient, restMapper, test.RemoteNamespace, localClusterID)

	return f, dynClient.Resource(*gvr).Namespace(test.RemoteNamespace).(*fake.DynamicResourceClient)
}
