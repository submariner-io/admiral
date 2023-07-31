/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package broker_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("Broker Syncer", func() {
	var (
		syncer                 *broker.Syncer
		config                 *broker.SyncerConfig
		localClient            *fake.DynamicResourceClient
		brokerClient           *fake.DynamicResourceClient
		resource               *corev1.Pod
		transformed            *corev1.Pod
		initialLocalResources  []runtime.Object
		initialBrokerResources []runtime.Object
		stopCh                 chan struct{}
	)

	ctx := context.TODO()

	BeforeEach(func() {
		initialLocalResources = nil
		initialBrokerResources = nil
		stopCh = make(chan struct{})
		resource = test.NewPod("")

		wait := true
		config = &broker.SyncerConfig{
			LocalNamespace:  test.LocalNamespace,
			LocalClusterID:  "east",
			BrokerNamespace: test.RemoteNamespace,
			ResourceConfigs: []broker.ResourceConfig{
				{
					LocalSourceNamespace:   test.LocalNamespace,
					LocalResourceType:      &corev1.Pod{},
					BrokerResourceType:     &corev1.Pod{},
					BrokerWaitForCacheSync: &wait,
				},
			},
			Scheme: runtime.NewScheme(),
		}
	})

	JustBeforeEach(func() {
		Expect(corev1.AddToScheme(config.Scheme)).To(Succeed())

		var gvr *schema.GroupVersionResource

		config.RestMapper, gvr = test.GetRESTMapperAndGroupVersionResourceFor(resource)

		config.LocalClient = fake.NewDynamicClient(config.Scheme, test.PrepInitialClientObjs("", "", initialLocalResources...)...)
		config.BrokerClient = fake.NewDynamicClient(config.Scheme, test.PrepInitialClientObjs(config.BrokerNamespace,
			"", initialBrokerResources...)...)

		localClient, _ = config.LocalClient.Resource(*gvr).Namespace(config.ResourceConfigs[0].LocalSourceNamespace).(*fake.DynamicResourceClient)
		brokerClient, _ = config.BrokerClient.Resource(*gvr).Namespace(config.BrokerNamespace).(*fake.DynamicResourceClient)

		var err error
		syncer, err = broker.NewSyncer(*config)
		Expect(err).To(Succeed())

		Expect(syncer.Start(stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(stopCh)
	})

	When("a local resource is created in the local datastore", func() {
		JustBeforeEach(func() {
			test.CreateResource(localClient, resource)
			test.AwaitResource(brokerClient, resource.GetName())
		})

		It("should correctly sync to the broker datastore", func() {
			test.VerifyResource(brokerClient, resource, config.BrokerNamespace, config.LocalClusterID)
		})

		Context("and then deleted", func() {
			It("should be deleted from the broker datastore", func() {
				Expect(localClient.ResourceInterface.Delete(ctx, resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				test.AwaitNoResource(brokerClient, resource.GetName())

				// Ensure the broker syncer did not try to sync back to the local datastore
				localClient.VerifyNoUpdate(resource.GetName())
				localClient.VerifyNoDelete(resource.GetName())
			})
		})
	})

	When("a non-local resource is created in the local datastore", func() {
		It("should not sync to the broker datastore", func() {
			test.SetClusterIDLabel(resource, "remote")
			test.CreateResource(localClient, resource)

			brokerClient.VerifyNoCreate(resource.GetName())
		})
	})

	When("a non-local resource is created in the broker datastore", func() {
		JustBeforeEach(func() {
			test.SetClusterIDLabel(resource, "remote")
			test.CreateResource(brokerClient, resource)
			test.AwaitResource(localClient, resource.GetName())
		})

		It("should correctly sync to the local datastore", func() {
			test.VerifyResource(localClient, resource, config.LocalNamespace, "remote")
		})

		Context("and then deleted", func() {
			It("should be deleted from the broker datastore", func() {
				Expect(brokerClient.ResourceInterface.Delete(ctx, resource.GetName(), metav1.DeleteOptions{})).To(Succeed())
				test.AwaitNoResource(localClient, resource.GetName())

				// Ensure the local syncer did not try to sync back to the broker datastore
				brokerClient.VerifyNoUpdate(resource.GetName())
				brokerClient.VerifyNoDelete(resource.GetName())
			})
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
			initialLocalResources = append(initialLocalResources, resource)
		})

		Context("and a local resource is created in any namespace", func() {
			It("should sync to the broker datastore", func() {
				test.AwaitResource(brokerClient, resource.GetName())

				actual := test.GetPod(brokerClient, resource)
				Expect(actual.Labels).To(HaveKeyWithValue(sync.OrigNamespaceLabelKey, metav1.NamespaceDefault))
			})
		})

		Context("and a local resource is stale in the broker datastore on startup", func() {
			var staleResource *corev1.Pod

			BeforeEach(func() {
				staleResource = test.NewPod(config.BrokerNamespace)
				staleResource.Name = "stale-pod"
				test.SetClusterIDLabel(staleResource, config.LocalClusterID)
				staleResource.Labels[sync.OrigNamespaceLabelKey] = metav1.NamespaceDefault
				initialBrokerResources = append(initialBrokerResources, staleResource)
			})

			It("should delete it from the broker datastore on reconciliation", func() {
				test.AwaitNoResource(brokerClient, staleResource.GetName())
			})
		})

		Context("and a resource exists locally and in the broker datastore on startup", func() {
			var localResource *corev1.Pod

			BeforeEach(func() {
				localResource = test.NewPod(metav1.NamespaceDefault)
				localResource.Name = "local-pod"
				initialLocalResources = append(initialLocalResources, localResource)

				brokerResource := localResource.DeepCopy()
				test.SetClusterIDLabel(brokerResource, config.LocalClusterID)
				brokerResource.Labels[sync.OrigNamespaceLabelKey] = metav1.NamespaceDefault
				initialBrokerResources = append(initialBrokerResources, brokerResource)
			})

			It("should not delete it from the broker datastore on reconciliation", func() {
				time.Sleep(100 * time.Millisecond)
				test.AwaitResource(brokerClient, localResource.GetName())
			})
		})
	})

	When("a local transform function is specified", func() {
		BeforeEach(func() {
			transformed = test.NewPodWithImage(config.LocalNamespace, "transformed")
			config.ResourceConfigs[0].TransformLocalToBroker = func(from runtime.Object, numRequeues int,
				op sync.Operation,
			) (runtime.Object, bool) {
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
			config.ResourceConfigs[0].TransformBrokerToLocal = func(from runtime.Object, numRequeues int,
				op sync.Operation,
			) (runtime.Object, bool) {
				return transformed, false
			}
		})

		Context("and a resource is created in the broker datastore", func() {
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

		Context("and the default equivalence function is specified", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].LocalResourcesEquivalent = sync.DefaultResourcesEquivalent
			})

			It("should not sync to the broker datastore", func() {
				brokerClient.VerifyNoUpdate(resource.GetName())
			})
		})

		Context("and a custom equivalence function is specified that compares Status", func() {
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

		Context("and the default equivalence function is specified", func() {
			BeforeEach(func() {
				config.ResourceConfigs[0].BrokerResourcesEquivalent = sync.DefaultResourcesEquivalent
			})

			It("should not sync to the local datastore", func() {
				localClient.VerifyNoUpdate(resource.GetName())
			})
		})

		Context("and a custom equivalence function is specified that compares Status", func() {
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

	When("a synced non-local resource is stale in the local datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.LocalNamespace)
			test.SetClusterIDLabel(resource, "remote")
			initialLocalResources = append(initialLocalResources, resource)
		})

		It("should delete it from the local datastore on reconciliation", func() {
			test.AwaitNoResource(localClient, resource.GetName())
		})
	})

	When("a synced non-local resource exists locally and in the broker datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.LocalNamespace)
			test.SetClusterIDLabel(resource, "remote")
			initialLocalResources = append(initialLocalResources, resource)

			brokerResource := resource.DeepCopy()
			brokerResource.SetNamespace(config.BrokerNamespace)
			initialBrokerResources = append(initialBrokerResources, brokerResource)
		})

		It("should not delete it from the local datastore on reconciliation", func() {
			time.Sleep(100 * time.Millisecond)
			test.AwaitResource(localClient, resource.GetName())
		})
	})

	When("an unsynced local resource does not exist in the broker datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.LocalNamespace)
			initialLocalResources = append(initialLocalResources, resource)
		})

		It("should not delete it from the local datastore on reconciliation", func() {
			time.Sleep(100 * time.Millisecond)
			test.AwaitResource(localClient, resource.GetName())
		})
	})

	When("a synced local resource is stale in the broker datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.BrokerNamespace)
			test.SetClusterIDLabel(resource, config.LocalClusterID)
			initialBrokerResources = append(initialBrokerResources, resource)
		})

		It("should delete it from the broker datastore on reconciliation", func() {
			test.AwaitNoResource(brokerClient, resource.GetName())
		})
	})

	When("a synced local resource exists in the broker datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.LocalNamespace)
			initialLocalResources = append(initialLocalResources, resource)

			brokerResource := resource.DeepCopy()
			brokerResource.SetNamespace(config.BrokerNamespace)
			test.SetClusterIDLabel(brokerResource, config.LocalClusterID)
			initialBrokerResources = append(initialBrokerResources, brokerResource)
		})

		It("should not delete it from the broker datastore on reconciliation", func() {
			time.Sleep(100 * time.Millisecond)
			test.AwaitResource(brokerClient, resource.GetName())
		})
	})

	When("a synced non-local resource does not exist in the local datastore on startup", func() {
		BeforeEach(func() {
			resource.SetNamespace(config.BrokerNamespace)
			test.SetClusterIDLabel(resource, "remote")
			initialBrokerResources = append(initialBrokerResources, resource)
		})

		It("should not delete it from the broker datastore on reconciliation", func() {
			time.Sleep(100 * time.Millisecond)
			test.AwaitResource(brokerClient, resource.GetName())
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

		Context("with an invalid resource type", func() {
			It("should panic", func() {
				Expect(func() {
					_, _, _ = syncer.GetLocalResource("", "", &corev1.Namespace{})
				}).To(Panic())
			})
		})
	})

	When("ListLocalResources is called", func() {
		It("should return the correct resources", func() {
			test.CreateResource(localClient, resource)
			test.AwaitResource(brokerClient, resource.GetName())

			list := syncer.ListLocalResources(resource)
			Expect(list).To(HaveLen(1))
			Expect(list[0]).To(BeAssignableToTypeOf(&corev1.Pod{}))
			Expect(&list[0].(*corev1.Pod).Spec).To(Equal(&resource.Spec))
		})

		Context("with an invalid resource type", func() {
			It("should panic", func() {
				Expect(func() {
					_, _, _ = syncer.GetLocalResource("", "", &corev1.Namespace{})
				}).To(Panic())
			})
		})
	})

	When("ListLocalResourcesBySelector is called", func() {
		It("should return the correct resources", func() {
			test.CreateResource(localClient, resource)
			test.AwaitResource(brokerClient, resource.GetName())

			list := syncer.ListLocalResourcesBySelector(resource, labels.Set(resource.Labels).AsSelector())
			Expect(list).To(HaveLen(1))
			Expect(list[0]).To(BeAssignableToTypeOf(&corev1.Pod{}))
			Expect(&list[0].(*corev1.Pod).Spec).To(Equal(&resource.Spec))
		})

		Context("with an invalid resource type", func() {
			It("should panic", func() {
				Expect(func() {
					_, _, _ = syncer.GetLocalResource("", "", &corev1.Namespace{})
				}).To(Panic())
			})
		})
	})
})
