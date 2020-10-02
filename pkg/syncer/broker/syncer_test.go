package broker_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	sync "github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
)

var _ = Describe("Broker Syncer", func() {
	var (
		syncer           *broker.Syncer
		config           *broker.SyncerConfig
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
		config = &broker.SyncerConfig{
			LocalNamespace:  test.LocalNamespace,
			LocalClusterID:  "east",
			BrokerNamespace: test.RemoteNamespace,
			ResourceConfigs: []broker.ResourceConfig{
				{
					LocalSourceNamespace: test.LocalNamespace,
					LocalResourceType:    resource,
					BrokerResourceType:   resource,
				},
			},
		}
	})

	JustBeforeEach(func() {
		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(resource)

		localDynClient := fake.NewDynamicClient(scheme.Scheme, test.PrepInitialClientObjs("", "", initialResources...)...)
		brokerDynClient := fake.NewDynamicClient(scheme.Scheme)

		localClient = localDynClient.Resource(*gvr).Namespace(config.ResourceConfigs[0].LocalSourceNamespace).(*fake.DynamicResourceClient)
		brokerClient = brokerDynClient.Resource(*gvr).Namespace(config.BrokerNamespace).(*fake.DynamicResourceClient)

		var err error
		syncer, err = broker.NewSyncerWithDetail(config, localDynClient, brokerDynClient, restMapper)
		Expect(err).To(Succeed())

		Expect(syncer.Start(stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(stopCh)
	})

	When("a local resource is created then deleted in the local datastore", func() {
		It("should correctly sync to the broker datastore", func() {
			test.CreateResource(localClient, resource)

			test.AwaitResource(brokerClient, resource.GetName())
			test.VerifyResource(brokerClient, resource, config.BrokerNamespace, config.LocalClusterID)

			Expect(localClient.ResourceInterface.Delete(resource.GetName(), nil)).To(Succeed())
			test.AwaitNoResource(brokerClient, resource.GetName())

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

			test.AwaitResource(localClient, resource.GetName())
			test.VerifyResource(localClient, resource, config.LocalNamespace, "remote")

			Expect(brokerClient.ResourceInterface.Delete(resource.GetName(), nil)).To(Succeed())
			test.AwaitNoResource(localClient, resource.GetName())

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
				test.AwaitResource(brokerClient, resource.GetName())
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

				test.AwaitResource(brokerClient, resource.GetName())
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

				test.AwaitResource(localClient, resource.GetName())
				test.VerifyResource(localClient, transformed, config.LocalNamespace, "remote")
			})
		})
	})

	When("a local resource's Status is updated in the local datastore", func() {
		JustBeforeEach(func() {
			test.CreateResource(localClient, resource)
			test.AwaitResource(brokerClient, resource.GetName())

			resource.Status.Phase = corev1.PodRunning
			test.UpdateResource(localClient, resource)
		})

		When("the default equivalence function is specified", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].LocalResourcesEquivalent = sync.DefaultResourcesEquivalent
			})

			It("should not sync to the broker datastore", func() {
				brokerClient.VerifyNoUpdate(resource.GetName())
			})
		})

		When("a custom equivalence function is specified that compares Status", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].LocalResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
					return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "status"),
						util.GetNestedField(obj2, "status"))
				}
			})

			It("should sync to the broker datastore", func() {
				test.AwaitAndVerifyResource(brokerClient, resource.GetName(), func(obj *unstructured.Unstructured) bool {
					v, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
					return corev1.PodPhase(v) == corev1.PodRunning
				})
			})
		})
	})

	When("a non-local resource's Status is updated in the broker datastore", func() {
		JustBeforeEach(func() {
			test.SetClusterIDLabel(resource, "remote")
			test.CreateResource(brokerClient, resource)
			test.AwaitResource(localClient, resource.GetName())

			resource.Status.Phase = corev1.PodRunning
			test.UpdateResource(brokerClient, resource)
		})

		When("the default equivalence function is specified", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].BrokerResourcesEquivalent = sync.DefaultResourcesEquivalent
			})

			It("should not sync to the local datastore", func() {
				localClient.VerifyNoUpdate(resource.GetName())
			})
		})

		When("a custom equivalence function is specified that compares Status", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].BrokerResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
					return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "status"),
						util.GetNestedField(obj2, "status"))
				}
			})

			It("should sync to the local datastore", func() {
				test.AwaitAndVerifyResource(localClient, resource.GetName(), func(obj *unstructured.Unstructured) bool {
					v, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
					return corev1.PodPhase(v) == corev1.PodRunning
				})
			})
		})
	})

	When("GetBrokerFederatorFor is called", func() {
		It("should return the Federator", func() {
			Expect(syncer.GetBrokerFederator()).ToNot(BeNil())
		})
	})

	When("GetLocalResource is called", func() {
		It("should return the correct resource", func() {
			test.CreateResource(localClient, resource)
			test.AwaitResource(brokerClient, resource.GetName())

			obj, exists, err := syncer.GetLocalResource(resource.Name, test.LocalNamespace, resource)
			Expect(err).To(Succeed())
			Expect(exists).To(BeTrue())

			pod, ok := obj.(*corev1.Pod)
			Expect(ok).To(BeTrue())
			Expect(pod.Name).To(Equal(resource.Name))
			Expect(pod.Spec).To(Equal(resource.Spec))
		})
	})
})
