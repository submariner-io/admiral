package kubefed

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	fedv1 "sigs.k8s.io/kubefed/pkg/apis/core/v1beta1"
)

type added string
type updated string
type removed string

type testClusterEventHandler struct {
	events chan interface{}
}

type testClusterEventHandler1 struct {
	testClusterEventHandler
}

type testClusterEventHandler2 struct {
	testClusterEventHandler
}

const (
	secretName = "my-secret"
)

var _ = Describe("Kubefed Cluster", func() {
	klog.InitFlags(nil)

	When("Adding a watch", testAddingWatch)
	When("Closing the stop channel", testClose)
	Describe("KubeFedCluster event notifications", testKubeFedClusterNotifications)
})

func testAddingWatch() {
	var stopChan chan struct{}
	BeforeEach(func() {
		stopChan = make(chan struct{})
	})

	AfterEach(func() {
		close(stopChan)
	})

	It("Should notify ClusterEventHandler of existing KubeFedCluster items", func() {
		federator := newFederatorWithWatcher(watch.NewFake(), stopChan, *newKubeFedCluster("east"),
			*newKubeFedCluster("west"))

		handler1 := &testClusterEventHandler1{*newTestClusterEventHandler()}
		err := federator.AddHandler(handler1)
		Expect(err).ToNot(HaveOccurred())

		handler1.verifyAddEvents("east", "west")

		handler2 := &testClusterEventHandler2{*newTestClusterEventHandler()}
		err = federator.AddHandler(handler2)
		Expect(err).ToNot(HaveOccurred())

		handler2.verifyAddEvents("east", "west")
	})
}

func testClose() {
	It("Should shutdown the ClusterEventHandler's work queue", func() {
		stopChan := make(chan struct{})
		federator := newFederatorWithWatcher(watch.NewFake(), stopChan)

		handler := &testClusterEventHandler1{*newTestClusterEventHandler()}
		err := federator.AddHandler(handler)
		Expect(err).ToNot(HaveOccurred())

		queue := federator.clusterWatchers[0].eventQueue

		close(stopChan)

		Eventually(queue.ShuttingDown).Should(BeTrue())
	})
}

func testOnAdd(fakeWatcher *watch.FakeWatcher, handler *testClusterEventHandler) *fedv1.KubeFedCluster {
	addedKubeFedCluster := newKubeFedCluster("east")
	fakeWatcher.Add(addedKubeFedCluster)

	var event interface{}
	Eventually(handler.events, 5).Should(Receive(&event))
	Expect(event).Should(Equal(added("east")))
	Consistently(handler.events).ShouldNot(Receive())

	return addedKubeFedCluster
}

func testOnRemove(fakeWatcher *watch.FakeWatcher, handler *testClusterEventHandler) {
	addedKubeFedCluster := testOnAdd(fakeWatcher, handler)

	fakeWatcher.Delete(addedKubeFedCluster)

	var event interface{}
	Eventually(handler.events, 5).Should(Receive(&event))
	Expect(event).Should(Equal(removed("east")))
	Consistently(handler.events).ShouldNot(Receive())
}

func testKubeFedClusterNotifications() {
	var stopChan chan struct{}
	var fakeWatcher *watch.FakeWatcher
	var federator *Federator
	var handler *testClusterEventHandler
	var event interface{}
	BeforeEach(func() {
		stopChan = make(chan struct{})
		fakeWatcher = watch.NewFake()
		federator = newFederatorWithWatcher(fakeWatcher, stopChan)
		handler = newTestClusterEventHandler()

		err := federator.AddHandler(handler)
		Expect(err).ToNot(HaveOccurred())
		Consistently(handler.events).ShouldNot(Receive())
	})

	AfterEach(func() {
		close(stopChan)
	})

	When("a KubeFedCluster is added", func() {
		It("should notify the ClusterEventHandler once of OnAdd", func() {
			testOnAdd(fakeWatcher, handler)
		})
	})

	When("a KubeFedCluster is deleted", func() {
		It("should notify the ClusterEventHandler once of OnRemove", func() {
			testOnRemove(fakeWatcher, handler)
		})
	})

	When("a KubeFedCluster is modified", func() {
		It("should notify the ClusterEventHandler once of OnUpdate", func() {
			addedKubeFedCluster := testOnAdd(fakeWatcher, handler)

			updatedKubeFedCluster := addedKubeFedCluster.DeepCopy()
			updatedKubeFedCluster.Spec.CABundle = []byte{0, 1}

			fakeWatcher.Modify(updatedKubeFedCluster)

			Eventually(handler.events, 5).Should(Receive(&event))
			Expect(event).Should(Equal(updated("east")))
			Consistently(handler.events).ShouldNot(Receive())
		})
	})

	When("a KubeFedCluster is added, modified, and removed", func() {
		It("should notify the ClusterEventHandler of OnAdd, OnUpdate, and OnRemove in order", func() {
			addedKubeFedCluster := newKubeFedCluster("east")
			updatedKubeFedCluster := addedKubeFedCluster.DeepCopy()
			updatedKubeFedCluster.Spec.CABundle = []byte{0, 1}

			fakeWatcher.Add(addedKubeFedCluster)
			fakeWatcher.Modify(updatedKubeFedCluster)
			fakeWatcher.Delete(updatedKubeFedCluster)

			Eventually(handler.events, 5).Should(Receive(&event))
			Expect(event).Should(Equal(added("east")))

			Eventually(handler.events, 5).Should(Receive(&event))
			Expect(event).Should(Equal(updated("east")))

			Eventually(handler.events, 5).Should(Receive(&event))
			Expect(event).Should(Equal(removed("east")))

			Consistently(handler.events).ShouldNot(Receive())
		})
	})

	When("a KubeFedCluster is re-added after being deleted", func() {
		It("should notify the ClusterEventHandler of OnAdd", func() {
			testOnRemove(fakeWatcher, handler)
			testOnAdd(fakeWatcher, handler)
		})
	})

	When("a KubeFedCluster is re-added after being added", func() {
		It("should not notify the ClusterEventHandler of OnAdd", func() {
			testOnAdd(fakeWatcher, handler)

			fakeWatcher.Add(newKubeFedCluster("east"))
			Consistently(handler.events, time.Millisecond*300).ShouldNot(Receive())
		})
	})

	When("a KubeFedCluster is modified with no change to the KubeFedClusterSpec", func() {
		It("should not notify the ClusterEventHandler of OnUpdate", func() {
			addedKubeFedCluster := testOnAdd(fakeWatcher, handler)

			updatedKubeFedCluster := addedKubeFedCluster.DeepCopy()
			updatedKubeFedCluster.Status = fedv1.KubeFedClusterStatus{Region: "east"}

			fakeWatcher.Modify(updatedKubeFedCluster)
			Consistently(handler.events, time.Millisecond*300).ShouldNot(Receive())
		})
	})
}

func newKubeFedCluster(name string) *fedv1.KubeFedCluster {
	return &fedv1.KubeFedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: fedv1.KubeFedClusterSpec{
			APIEndpoint: "http://localhost",
			SecretRef:   fedv1.LocalSecretReference{Name: secretName},
		},
	}
}

func newFederatorWithWatcher(watcher watch.Interface, stopChan <-chan struct{}, initialKubeFedClusters ...fedv1.KubeFedCluster) *Federator {
	federator := &Federator{
		clusterMap:    make(map[string]*rest.Config),
		kubeFedClient: clientFake.NewFakeClient(newSecret()),
		stopChan:      stopChan,
	}

	federator.initKubeFedClusterInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return &fedv1.KubeFedClusterList{
				ListMeta: metav1.ListMeta{ResourceVersion: "1"},
				Items:    initialKubeFedClusters,
			}, nil
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return watcher, nil
		},
	})

	return federator
}

func newSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: kubeFedNamespace,
		},
		Data: map[string][]byte{corev1.ServiceAccountTokenKey: []byte{1, 2}},
	}
}

func newTestClusterEventHandler() *testClusterEventHandler {
	return &testClusterEventHandler{events: make(chan interface{}, 10)}
}

func (t *testClusterEventHandler) OnAdd(clusterID string, kubeConfig *rest.Config) {
	klog.Infof("testClusterEventHandler OnAdd for cluster %s", clusterID)
	t.events <- added(clusterID)
}

func (t *testClusterEventHandler) OnUpdate(clusterID string, kubeConfig *rest.Config) {
	klog.Infof("testClusterEventHandler OnUpdate for cluster %s", clusterID)
	t.events <- updated(clusterID)
}

func (t *testClusterEventHandler) OnRemove(clusterID string) {
	klog.Infof("testClusterEventHandler OnRemove for cluster %s", clusterID)
	t.events <- removed(clusterID)
}

func (t *testClusterEventHandler) verifyAddEvents(expectedClusters ...string) {
	expected := make(map[interface{}]bool)
	for _, c := range expectedClusters {
		expected[added(c)] = true
	}

	var event interface{}
	for i := 0; i < len(expectedClusters); i++ {
		Eventually(t.events, 5).Should(Receive(&event))
		Expect(expected).Should(HaveKey(event), "Received unexpected event: %v", event)
		delete(expected, event)
	}

	Expect(expected).To(BeEmpty())
	Consistently(t.events).ShouldNot(Receive())
}
