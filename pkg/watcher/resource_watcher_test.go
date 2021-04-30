/*
© 2020 Red Hat, Inc.

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
package watcher_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/watcher"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

var _ = Describe("Resource Watcher", func() {
	var (
		config          *watcher.Config
		pods            dynamic.ResourceInterface
		services        dynamic.ResourceInterface
		pod             *corev1.Pod
		createdPods     chan *corev1.Pod
		updatedPods     chan *corev1.Pod
		deletedPods     chan *corev1.Pod
		createdServices chan *corev1.Service
		stopCh          chan struct{}
	)

	BeforeEach(func() {
		stopCh = make(chan struct{})
		createdPods = make(chan *corev1.Pod, 100)
		updatedPods = make(chan *corev1.Pod, 100)
		deletedPods = make(chan *corev1.Pod, 100)
		createdServices = make(chan *corev1.Service, 100)

		pod = test.NewPod("")

		config = &watcher.Config{
			Scheme: runtime.NewScheme(),
			ResourceConfigs: []watcher.ResourceConfig{
				{
					Name:            "Pod watcher",
					SourceNamespace: test.LocalNamespace,
					ResourceType:    &corev1.Pod{},
					Handler: watcher.EventHandlerFuncs{
						OnCreateFunc: func(obj runtime.Object, numRequeues int) bool {
							createdPods <- obj.(*corev1.Pod)
							return false
						},
						OnUpdateFunc: func(obj runtime.Object, numRequeues int) bool {
							updatedPods <- obj.(*corev1.Pod)
							return false
						},
						OnDeleteFunc: func(obj runtime.Object, numRequeues int) bool {
							deletedPods <- obj.(*corev1.Pod)
							return false
						},
					},
				},
				{
					Name:            "Node watcher",
					SourceNamespace: test.LocalNamespace,
					ResourceType:    &corev1.Service{},
					Handler: watcher.EventHandlerFuncs{
						OnCreateFunc: func(obj runtime.Object, numRequeues int) bool {
							createdServices <- obj.(*corev1.Service)
							return false
						},
					},
				},
			},
		}
	})

	JustBeforeEach(func() {
		Expect(corev1.AddToScheme(config.Scheme)).To(Succeed())

		config.RestMapper = test.GetRESTMapperFor(&corev1.Pod{}, &corev1.Service{})

		config.Client = fake.NewDynamicClient(config.Scheme)

		pods = config.Client.Resource(*test.GetGroupVersionResourceFor(config.RestMapper, &corev1.Pod{})).Namespace(
			config.ResourceConfigs[0].SourceNamespace)
		services = config.Client.Resource(*test.GetGroupVersionResourceFor(config.RestMapper, &corev1.Service{})).Namespace(
			config.ResourceConfigs[0].SourceNamespace)

		resourceWatcher, err := watcher.New(config)
		Expect(err).To(Succeed())

		Expect(resourceWatcher.Start(stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(stopCh)
	})

	When("a Pod is created, updated and deleted", func() {
		It("should notify the appropriate handler of each event", func() {
			obj := test.CreateResource(pods, pod)
			pod.Namespace = obj.GetNamespace()
			pod.ResourceVersion = obj.GetResourceVersion()
			pod.UID = obj.GetUID()

			Eventually(createdPods).Should(Receive(Equal(pod)))
			Consistently(createdPods).ShouldNot(Receive())

			pod.Spec.Containers[0].Image = "apache"
			test.UpdateResource(pods, pod)

			Eventually(updatedPods).Should(Receive(Equal(pod)))
			Consistently(updatedPods).ShouldNot(Receive())

			Expect(pods.Delete(context.TODO(), pod.GetName(), v1.DeleteOptions{})).To(Succeed())

			Eventually(deletedPods).Should(Receive(Equal(pod)))
			Consistently(deletedPods).ShouldNot(Receive())
		})
	})

	When("a Service is created", func() {
		It("should notify the appropriate handler", func() {
			service := &corev1.Service{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-service",
				},
			}

			obj := test.CreateResource(services, service)
			service.Namespace = obj.GetNamespace()
			service.ResourceVersion = obj.GetResourceVersion()
			service.UID = obj.GetUID()

			Eventually(createdServices).Should(Receive(Equal(service)))
		})
	})

	When("a custom equivalence function is specified that compares the Spec", func() {
		BeforeEach(func() {
			config.ResourceConfigs[0].ResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
				return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "spec"),
					util.GetNestedField(obj2, "spec"))
			}
		})

		When("the Pod's Status is updated", func() {
			It("should not notify of the update", func() {
				test.CreateResource(pods, pod)
				Eventually(createdPods).Should(Receive())

				pod.Status.Phase = corev1.PodRunning
				test.UpdateResource(pods, pod)

				Consistently(updatedPods, 300*time.Millisecond).ShouldNot(Receive())
			})
		})
	})

	When("a ShouldProcess function is specified that returns false", func() {
		BeforeEach(func() {
			config.ResourceConfigs[0].ShouldProcess = func(obj *unstructured.Unstructured, op syncer.Operation) bool {
				return false
			}
		})

		When("a Pod is created", func() {
			It("should not notify of the event", func() {
				test.CreateResource(pods, pod)
				Consistently(createdPods, 300*time.Millisecond).ShouldNot(Receive())
			})
		})
	})
})
