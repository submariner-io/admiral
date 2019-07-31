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
	id     string
	events chan interface{}
}

const (
	secretName = "my-secret"
)

var _ = Describe("Kubefed Cluster", func() {
	klog.InitFlags(nil)

	Describe("Adding a watch", testAddingWatch)
	Describe("When KubeFedCluster items are added, updated, removed", testOnKubeFedClusterChanges)
	Describe("clusterWatcher", testClusterWatcher)
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
		federator := newFederator(watch.NewFake(), stopChan, *newKubeFedCluster("east"),
			*newKubeFedCluster("west"))

		handler1 := newtTestClusterEventHandler("1")
		err := federator.WatchClusters(handler1)
		Expect(err).ToNot(HaveOccurred())

		Eventually(handler1.events, 5).Should(Receive(Or(Equal(added("east")), Equal(added("west")))))
		Eventually(handler1.events, 5).Should(Receive(Or(Equal(added("east")), Equal(added("west")))))
		Consistently(handler1.events).ShouldNot(Receive())

		handler2 := newtTestClusterEventHandler("2")
		err = federator.WatchClusters(handler2)
		Expect(err).ToNot(HaveOccurred())

		Eventually(handler2.events, 5).Should(Receive(Or(Equal(added("east")), Equal(added("west")))))
		Eventually(handler2.events, 5).Should(Receive(Or(Equal(added("east")), Equal(added("west")))))
	})
}

func testOnKubeFedClusterChanges() {
	var stopChan chan struct{}
	BeforeEach(func() {
		stopChan = make(chan struct{})
	})

	AfterEach(func() {
		close(stopChan)
	})

	It("Should notify ClusterEventHandler of appropriate change events", func() {
		fakeWatcher := watch.NewFake()
		federator := newFederator(fakeWatcher, stopChan)

		handler := newtTestClusterEventHandler("")
		err := federator.WatchClusters(handler)
		Expect(err).ToNot(HaveOccurred())

		Consistently(handler.events).ShouldNot(Receive())

		By("Add, modify, delete KubeFedCluster \"east\" - expect events in order")

		addedKubeFedCluster := newKubeFedCluster("east")
		updatedKubeFedCluster := addedKubeFedCluster.DeepCopy()
		updatedKubeFedCluster.Spec.CABundle = []byte{0, 1}

		fakeWatcher.Add(addedKubeFedCluster)
		fakeWatcher.Modify(updatedKubeFedCluster)
		fakeWatcher.Delete(updatedKubeFedCluster)

		var event interface{}
		Eventually(handler.events, 5).Should(Receive(&event))
		Expect(event).Should(Equal(added("east")))

		Eventually(handler.events, 5).Should(Receive(&event))
		Expect(event).Should(Equal(updated("east")))

		Eventually(handler.events, 5).Should(Receive(&event))
		Expect(event).Should(Equal(removed("east")))

		Consistently(handler.events).ShouldNot(Receive())

		By("Add KubeFedCluster \"east\" again - expect OnAdd event")

		fakeWatcher.Add(addedKubeFedCluster)

		Eventually(handler.events, 5).Should(Receive(&event))
		Expect(event).Should(Equal(added("east")))

		By("Add KubeFedCluster \"east\" again - expect no OnAdd event")

		fakeWatcher.Add(addedKubeFedCluster)

		Consistently(handler.events, time.Millisecond*300).ShouldNot(Receive())

		By("Modify KubeFedCluster \"east\" with no change to the KubeFedClusterSpec - expect no OnUpdate event")

		updatedKubeFedCluster = addedKubeFedCluster.DeepCopy()
		updatedKubeFedCluster.Status = fedv1.KubeFedClusterStatus{Region: "east"}
		fakeWatcher.Modify(updatedKubeFedCluster)

		Consistently(handler.events, time.Millisecond*300).ShouldNot(Receive())
	})
}

func testClusterWatcher() {
	var stopChan chan struct{}
	var watcher *clusterWatcher
	BeforeEach(func() {
		stopChan = make(chan struct{})
		watcher = &clusterWatcher{
			eventQueue:           make(chan func()),
			enqueueTimerInterval: time.Duration(time.Millisecond * 100),
			stopChan:             stopChan,
			handlerString:        "test-handler",
		}
	})

	Context("When enqueueEvent is called with the eventQueue blocked", func() {
		It("Should send the event and return when the eventQueue is unblocked", func() {
			enqueueEventStarting := make(chan struct{})
			enqueueEventDone := make(chan struct{})
			go func() {
				enqueueEventStarting <- struct{}{}
				watcher.enqueueEvent(func() {}, "east", "Add")
				enqueueEventDone <- struct{}{}
			}()

			Eventually(enqueueEventStarting, 5).Should(Receive(), "enqueueEvent goroutine did not start")

			time.Sleep(time.Millisecond * 500)

			Eventually(watcher.eventQueue, 3).Should(Receive(), "Event was not sent")
			Eventually(enqueueEventDone, 3).Should(Receive(), "enqueueEvent did not complete")
		})

		It("Should return when the stopChan receives an event", func() {
			enqueueEventDone := make(chan struct{})
			go func() {
				watcher.enqueueEvent(func() {}, "east", "Add")
				enqueueEventDone <- struct{}{}
			}()

			close(stopChan)
			Eventually(enqueueEventDone, 3).Should(Receive(), "enqueueEvent did not complete")
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

func newFederator(watcher watch.Interface, stopChan <-chan struct{}, initialKubeFedClusters ...fedv1.KubeFedCluster) *federator {
	federator := &federator{
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
		Data: map[string][]byte{tokenKey: []byte{1, 2}},
	}
}

func newtTestClusterEventHandler(id string) *testClusterEventHandler {
	return &testClusterEventHandler{
		id:     id,
		events: make(chan interface{}, 10),
	}
}

func (t *testClusterEventHandler) OnAdd(clusterID string, kubeConfig *rest.Config) {
	klog.Infof("testClusterEventHandler %s OnAdd for cluster %s", t.id, clusterID)
	t.events <- added(clusterID)
}

func (t *testClusterEventHandler) OnUpdate(clusterID string, kubeConfig *rest.Config) {
	klog.Infof("testClusterEventHandler %s OnUpdate for cluster %s", t.id, clusterID)
	t.events <- updated(clusterID)
}

func (t *testClusterEventHandler) OnRemove(clusterID string) {
	klog.Infof("testClusterEventHandler %s OnRemove for cluster %s", t.id, clusterID)
	t.events <- removed(clusterID)
}

func (t *testClusterEventHandler) String() string {
	return "testClusterEventHandler " + t.id
}
