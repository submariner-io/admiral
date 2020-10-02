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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("Federator", func() {
	Describe("Distribute", testDistribute)
	Describe("Delete", testDelete)
})

func testDistribute() {
	var (
		f                  federate.Federator
		resource           *corev1.Pod
		localClusterID     string
		federatorNamespace string
		targetNamespace    string
		resourceClient     *fake.DynamicResourceClient
		initObjs           []runtime.Object
		keepMetadataFields []string
	)

	BeforeEach(func() {
		localClusterID = "east"
		resource = test.NewPod(test.LocalNamespace)
		initObjs = nil
		keepMetadataFields = nil
		federatorNamespace = test.RemoteNamespace
		targetNamespace = test.RemoteNamespace
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, localClusterID, federatorNamespace, targetNamespace, keepMetadataFields...)
	})

	When("the resource does not already exist in the broker datastore", func() {
		When("a local cluster ID is specified", func() {
			It("should create the resource with the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
			})
		})

		When("a local cluster ID is not specified", func() {
			BeforeEach(func() {
				localClusterID = ""
			})

			It("should create the resource without the cluster ID label", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
			})
		})

		When("the resource contains Status data", func() {
			BeforeEach(func() {
				resource.Status = corev1.PodStatus{
					Phase:   "PodRunning",
					Message: "Pod is running",
				}
			})

			It("should create the resource with the Status data", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
			})
		})

		When("the resource contains OwnerReferences", func() {
			BeforeEach(func() {
				keepMetadataFields = []string{"ownerReferences"}
				resource.OwnerReferences = []metav1.OwnerReference{
					{
						Kind: "DaemonSet",
						Name: "foo",
					},
				}
			})

			It("should create the resource with the OwnerReferences", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
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
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
			})
		})
	})

	When("the resource already exists in the broker datastore", func() {
		BeforeEach(func() {
			existing := resource.DeepCopy()
			existing.SetNamespace(targetNamespace)
			initObjs = append(initObjs, existing)

			resource = test.NewPodWithImage(test.LocalNamespace, "apache")
		})

		It("should update the resource", func() {
			Expect(f.Distribute(resource)).To(Succeed())
			test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
		})

		When("update initially fails due to conflict", func() {
			JustBeforeEach(func() {
				resourceClient.FailOnUpdate = apierrors.NewConflict(schema.GroupResource{}, "", errors.New("fake"))
			})

			It("should retry until it succeeds", func() {
				Expect(f.Distribute(resource)).To(Succeed())
				test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
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

	When("no target namespace is specified", func() {
		BeforeEach(func() {
			targetNamespace = "another-ns"
			federatorNamespace = corev1.NamespaceAll
			resource.SetNamespace(targetNamespace)
		})

		It("should create the resource in the source namespace", func() {
			Expect(f.Distribute(resource)).To(Succeed())
			test.VerifyResource(resourceClient, resource, targetNamespace, localClusterID)
		})
	})
}

func testDelete() {
	var (
		f                  federate.Federator
		resource           *corev1.Pod
		resourceClient     *fake.DynamicResourceClient
		initObjs           []runtime.Object
		targetNamespace    string
		federatorNamespace string
	)

	BeforeEach(func() {
		resource = test.NewPod(test.LocalNamespace)
		initObjs = nil
		targetNamespace = test.RemoteNamespace
		federatorNamespace = test.RemoteNamespace
	})

	JustBeforeEach(func() {
		f, resourceClient = setupFederator(resource, initObjs, "", federatorNamespace, targetNamespace)
	})

	When("the resource exists in the broker datastore", func() {
		BeforeEach(func() {
			existing := resource.DeepCopy()
			existing.SetNamespace(targetNamespace)
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

		When("no target namespace is specified", func() {
			BeforeEach(func() {
				targetNamespace = "another-ns"
				federatorNamespace = corev1.NamespaceAll
				initObjs[0].(*corev1.Pod).SetNamespace(targetNamespace)
				resource.SetNamespace(targetNamespace)
			})

			It("should delete the resource from the source namespace", func() {
				Expect(f.Delete(resource)).To(Succeed())

				_, err := test.GetResourceAndError(resourceClient, resource)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	When("the resource does not exist in the broker datastore", func() {
		It("should return NotFound error", func() {
			Expect(apierrors.IsNotFound(f.Delete(resource))).To(BeTrue())
		})
	})
}

func setupFederator(resource *corev1.Pod, initObjs []runtime.Object, localClusterID, federatorNS, targetNS string,
	keepMetadataField ...string) (federate.Federator,
	*fake.DynamicResourceClient) {
	dynClient := fake.NewDynamicClient(scheme.Scheme, test.PrepInitialClientObjs("", localClusterID, initObjs...)...)
	restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(resource)
	f := broker.NewFederator(dynClient, restMapper, federatorNS, localClusterID, keepMetadataField...)

	return f, dynClient.Resource(*gvr).Namespace(targetNS).(*fake.DynamicResourceClient)
}
