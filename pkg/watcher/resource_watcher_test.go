package watcher_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/watcher"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

var _ = Describe("Resource Watcher", func() {
	var (
		config   *watcher.Config
		client   dynamic.ResourceInterface
		resource *corev1.Pod
		created  chan *corev1.Pod
		updated  chan *corev1.Pod
		deleted  chan *corev1.Pod
		stopCh   chan struct{}
	)

	BeforeEach(func() {
		stopCh = make(chan struct{})
		created = make(chan *corev1.Pod, 100)
		updated = make(chan *corev1.Pod, 100)
		deleted = make(chan *corev1.Pod, 100)

		resource = test.NewPod("")

		config = &watcher.Config{
			Name:            "Pod watcher",
			SourceNamespace: test.LocalNamespace,
			Scheme:          runtime.NewScheme(),
			ResourceConfigs: []watcher.ResourceConfig{
				{
					ResourceType: &corev1.Pod{},
					Handler: watcher.EventHandlerFuncs{
						OnCreateFunc: func(obj runtime.Object) bool {
							created <- obj.(*corev1.Pod)
							return false
						},
						OnUpdateFunc: func(obj runtime.Object) bool {
							updated <- obj.(*corev1.Pod)
							return false
						},
						OnDeleteFunc: func(obj runtime.Object) bool {
							deleted <- obj.(*corev1.Pod)
							return false
						},
					},
				},
			},
		}
	})

	JustBeforeEach(func() {
		Expect(corev1.AddToScheme(config.Scheme)).To(Succeed())

		restMapper, gvr := test.GetRESTMapperAndGroupVersionResourceFor(resource)

		localDynClient := fake.NewDynamicClient(config.Scheme)
		client = localDynClient.Resource(*gvr).Namespace(config.SourceNamespace)

		resourceWatcher, err := watcher.NewWithDetail(config, restMapper, localDynClient)
		Expect(err).To(Succeed())

		Expect(resourceWatcher.Start(stopCh)).To(Succeed())
	})

	AfterEach(func() {
		close(stopCh)
	})

	When("a resource is created, updated and deleted", func() {
		It("should notify the handler of each event", func() {
			obj := test.CreateResource(client, resource)
			resource.Namespace = obj.GetNamespace()
			resource.ResourceVersion = obj.GetResourceVersion()
			resource.UID = obj.GetUID()

			Eventually(created).Should(Receive(Equal(resource)))
			Consistently(created).ShouldNot(Receive())

			resource.Spec.Containers[0].Image = "apache"
			test.UpdateResource(client, resource)

			Eventually(updated).Should(Receive(Equal(resource)))
			Consistently(updated).ShouldNot(Receive())

			Expect(client.Delete(resource.GetName(), nil)).To(Succeed())

			Eventually(deleted).Should(Receive(Equal(resource)))
			Consistently(deleted).ShouldNot(Receive())
		})
	})

	When("a custom equivalence function is specified that compares the Spec", func() {
		BeforeEach(func() {
			config.ResourceConfigs[0].ResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
				return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "spec"),
					util.GetNestedField(obj2, "spec"))
			}
		})

		When("the resource's Status is updated", func() {
			It("should not notify of the update", func() {
				test.CreateResource(client, resource)
				Eventually(created).Should(Receive())

				resource.Status.Phase = corev1.PodRunning
				test.UpdateResource(client, resource)

				Consistently(updated, 300*time.Millisecond).ShouldNot(Receive())
			})
		})
	})
})
