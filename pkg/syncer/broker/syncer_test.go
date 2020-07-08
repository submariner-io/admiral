package broker

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	sync "github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var _ = Describe("Broker Syncer", func() {
	var (
		syncer           *Syncer
		config           *SyncerConfig
		localClient      *fake.DynamicResourceClient
		brokerClient     *fake.DynamicResourceClient
		resource         *corev1.Pod
		transformed      *corev1.Pod
		initialResources []runtime.Object
		stopCh           chan struct{}
	)

	BeforeEach(func() {
		initialResources = nil
		stopCh = make(chan struct{})
		resource = test.NewPod("")
		config = &SyncerConfig{
			LocalNamespace:  test.LocalNamespace,
			LocalClusterID:  "east",
			BrokerNamespace: test.RemoteNamespace,
			ResourceConfigs: []ResourceConfig{
				{
					LocalResourceType:  resource,
					BrokerResourceType: resource,
				},
			},
		}
	})

	JustBeforeEach(func() {
		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(resource)

		localDynClient := fake.NewDynamicClient(test.PrepInitialClientObjs("", "", initialResources...)...)
		brokerDynClient := fake.NewDynamicClient()

		localClient = localDynClient.Resource(*gvr).Namespace(config.ResourceConfigs[0].LocalSourceNamespace).(*fake.DynamicResourceClient)
		brokerClient = brokerDynClient.Resource(*gvr).Namespace(config.BrokerNamespace).(*fake.DynamicResourceClient)

		var err error
		syncer, err = newSyncer(config, localDynClient, brokerDynClient, restMapper)
		Expect(err).To(Succeed())

		Expect(syncer.Start(stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(stopCh)
	})

	When("a local resource is created then deleted in the local datastore", func() {
		It("should correctly sync to the broker datastore", func() {
			test.CreateResource(localClient, resource)

			test.WaitForResource(brokerClient, resource.GetName())
			test.VerifyResource(brokerClient, resource, config.BrokerNamespace, config.LocalClusterID)

			Expect(localClient.ResourceInterface.Delete(resource.GetName(), nil)).To(Succeed())
			test.WaitForNoResource(brokerClient, resource.GetName())

			// Ensure the broker syncer did not try to sync back to the local datastore
			localClient.VerifyNoUpdate(resource.GetName())
			localClient.VerifyNoDelete(resource.GetName())
		})
	})

	When("a non-local resource is created in the local datastore", func() {
		It("should not sync to the broker datastore", func() {
			test.SetClusterIDLabel(resource, "remote")
			test.CreateResource(localClient, resource)

			brokerClient.VerifyNoCreate(resource.GetName())
		})
	})

	When("a non-local resource is created then deleted in the broker datastore", func() {
		It("should correctly sync to the local datastore", func() {
			test.SetClusterIDLabel(resource, "remote")
			test.CreateResource(brokerClient, resource)

			test.WaitForResource(localClient, resource.GetName())
			test.VerifyResource(localClient, resource, config.LocalNamespace, "remote")

			Expect(brokerClient.ResourceInterface.Delete(resource.GetName(), nil)).To(Succeed())
			test.WaitForNoResource(localClient, resource.GetName())

			// Ensure the local syncer did not try to sync back to the broker datastore
			brokerClient.VerifyNoUpdate(resource.GetName())
			brokerClient.VerifyNoDelete(resource.GetName())
		})
	})

	When("a local resource is created in the broker datastore", func() {
		It("should not sync to the local datastore", func() {
			test.SetClusterIDLabel(resource, config.LocalClusterID)
			test.CreateResource(brokerClient, resource)

			localClient.VerifyNoCreate(resource.GetName())
		})
	})

	When("syncing resources from all local namespaces", func() {
		BeforeEach(func() {
			config.ResourceConfigs[0].LocalSourceNamespace = metav1.NamespaceAll
			resource.SetNamespace(metav1.NamespaceDefault)
			initialResources = append(initialResources, resource)
		})

		When("a local resource is created in any namespace", func() {
			It("should sync to the broker datastore", func() {
				test.WaitForResource(brokerClient, resource.GetName())
				test.VerifyResource(brokerClient, resource, config.BrokerNamespace, config.LocalClusterID)
			})
		})
	})

	When("a local transform function is specified", func() {
		BeforeEach(func() {
			transformed = test.NewPodWithImage(config.LocalNamespace, "transformed")
			config.ResourceConfigs[0].LocalTransform = func(from runtime.Object, op sync.Operation) (runtime.Object, bool) {
				return transformed, false
			}
		})

		When("a resource is created in the local datastore", func() {
			It("should sync the transformed resource to the broker datastore", func() {
				test.CreateResource(localClient, resource)

				test.WaitForResource(brokerClient, resource.GetName())
				test.VerifyResource(brokerClient, transformed, config.BrokerNamespace, config.LocalClusterID)
			})
		})
	})

	When("a broker transform function is specified", func() {
		BeforeEach(func() {
			transformed = test.NewPodWithImage(config.LocalNamespace, "transformed")
			config.ResourceConfigs[0].BrokerTransform = func(from runtime.Object, op sync.Operation) (runtime.Object, bool) {
				return transformed, false
			}
		})

		When("a resource is created in the broker datastore", func() {
			It("should sync the transformed resource to the local datastore", func() {
				test.SetClusterIDLabel(resource, "remote")
				test.CreateResource(brokerClient, resource)

				test.WaitForResource(localClient, resource.GetName())
				test.VerifyResource(localClient, transformed, config.LocalNamespace, "remote")
			})
		})
	})

	When("GetBrokerFederatorFor is called for a valid resource type", func() {
		It("should return the Federator", func() {
			Expect(syncer.GetBrokerFederatorFor(resource)).ToNot(BeNil())
		})
	})

	When("GetBrokerFederatorFor is called for an invalid resource type", func() {
		It("should return nil", func() {
			Expect(syncer.GetBrokerFederatorFor(&corev1.Service{})).To(BeNil())
		})
	})
})
