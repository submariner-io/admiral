package controller

import (
	"errors"
	"fmt"
	"net"
	"reflect"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/federate/mocks"
	submarinerv1 "github.com/submariner-io/submariner/pkg/apis/submariner.io/v1"
	submarinerClientset "github.com/submariner-io/submariner/pkg/client/clientset/versioned"
	fakeSubm "github.com/submariner-io/submariner/pkg/client/clientset/versioned/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/testing"
	"k8s.io/klog"
)

type watchReactor struct {
	endpointsWatchStarted chan bool
	clustersWatchStarted  chan bool
}

type eqCluster struct {
	expected *submarinerv1.Cluster
}

type eqEndpoint struct {
	expected *submarinerv1.Endpoint
}

var _ = Describe("Submariner Orchestrator", func() {
	klog.InitFlags(nil)

	When("start is called", testStart)
	Describe("Cluster lifecycle notifications", testClusterLifecycleNotifications)
	Describe("Submariner resource distribution", testResourceDistribution)
})

func testStart() {
	var (
		mockCtrl      *gomock.Controller
		mockFederator *mocks.MockFederator
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockFederator = mocks.NewMockFederator(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
		mockCtrl = nil
		mockFederator = nil
	})

	When("WatchClusters succeeds", func() {
		It("should return no error", func() {
			mockFederator.EXPECT().WatchClusters(gomock.Any())
			Expect(New(mockFederator).Start()).To(Succeed())
		})
	})

	When("WatchClusters fails", func() {
		It("should return an error", func() {
			mockFederator.EXPECT().WatchClusters(gomock.Any()).Return(errors.New("mock error"))
			Expect(New(mockFederator).Start()).ToNot(Succeed())
		})
	})
}

func testClusterLifecycleNotifications() {
	var (
		controller   *controller
		watchReactor *watchReactor
	)

	BeforeEach(func() {
		controller = New(mocks.NewMockFederator(gomock.NewController(GinkgoT())))
		watchReactor = newWatchReactor(controller)
	})

	AfterEach(func() {
		watchReactor.close()
		controller.Stop()
	})

	testOnAdd := func(clusterID string) {
		controller.OnAdd(clusterID, &rest.Config{})

		Expect(controller.clusterWatchers).Should(HaveKey(clusterID))
		Eventually(watchReactor.endpointsWatchStarted).Should(Receive())
		Eventually(watchReactor.clustersWatchStarted).Should(Receive())
	}

	testOnRemove := func(clusterID string) {
		testOnAdd("east")
		stopChan := controller.clusterWatchers["east"].stopChan
		clusterWorkQueue := controller.clusterWatchers["east"].clusterWorkQueue
		endpointWorkQueue := controller.clusterWatchers["east"].endpointWorkQueue

		controller.OnRemove("east")

		Expect(controller.clusterWatchers).ShouldNot(HaveKey("east"))
		Expect(stopChan).To(BeClosed())
		Eventually(clusterWorkQueue.ShuttingDown).Should(BeTrue())
		Eventually(endpointWorkQueue.ShuttingDown).Should(BeTrue())
	}

	When("a cluster is added", func() {
		It("should start watches for the Submariner Endpoint and Cluster resources", func() {
			testOnAdd("east")
		})
	})

	When("a cluster is removed", func() {
		It("should remove and close the clusterWatch", func() {
			testOnRemove("east")
		})
	})

	When("a cluster is updated", func() {
		It("should restart the Submariner Endpoint and Cluster watches", func() {
			testOnAdd("east")
			watchReactor.reset()
			prevStopChan := controller.clusterWatchers["east"].stopChan
			prevClusterWorkQueue := controller.clusterWatchers["east"].clusterWorkQueue
			prevEndpointWorkQueue := controller.clusterWatchers["east"].endpointWorkQueue

			controller.OnUpdate("east", &rest.Config{})

			Expect(controller.clusterWatchers).Should(HaveKey("east"))
			Eventually(watchReactor.endpointsWatchStarted).Should(Receive())
			Eventually(watchReactor.clustersWatchStarted).Should(Receive())

			Eventually(prevClusterWorkQueue.ShuttingDown).Should(BeTrue())
			Eventually(prevEndpointWorkQueue.ShuttingDown).Should(BeTrue())
			Expect(prevStopChan).To(BeClosed())
		})
	})

	When("a cluster is re-added", func() {
		It("should stop and restart the Submariner Endpoint and Cluster watches", func() {
			testOnRemove("east")
			watchReactor.reset()
			testOnAdd("east")
		})
	})
}

func testResourceDistribution() {
	const clusterID = "east"

	var (
		cluster          *submarinerv1.Cluster
		endpoint         *submarinerv1.Endpoint
		distributeCalled chan bool
		mockCtrl         *gomock.Controller
		mockFederator    *mocks.MockFederator
		controller       *controller
		fakeClientset    *fakeSubm.Clientset
	)

	BeforeEach(func() {
		cluster = newCluster()
		endpoint = newEndpoint()
		distributeCalled = make(chan bool, 1)
		mockCtrl = gomock.NewController(GinkgoT())
		mockFederator = mocks.NewMockFederator(mockCtrl)
		controller = New(mockFederator)
		fakeClientset = fakeSubm.NewSimpleClientset()

		controller.newSubmClientset = func(c *rest.Config) (submarinerClientset.Interface, error) {
			return fakeClientset, nil
		}

		controller.OnAdd(clusterID, &rest.Config{})
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	createCluster := func() error {
		_, err := fakeClientset.SubmarinerV1().Clusters(submarinerNamespace).Create(cluster)
		return err
	}

	createEndpoint := func() error {
		_, err := fakeClientset.SubmarinerV1().Endpoints(submarinerNamespace).Create(endpoint)
		return err
	}

	When("a Cluster resource is added", func() {
		It("should distribute the resource", func() {
			// Distribute is called async and gomock doesn't have a way of specifying a timeout for the invocation
			// so we use our own Do action and a channel.
			var captured **submarinerv1.Cluster = new(*submarinerv1.Cluster)
			mockFederator.EXPECT().Distribute(EqCluster(cluster)).Return(nil).Do(func(c *submarinerv1.Cluster) {
				*captured = c
				distributeCalled <- true
			})

			Expect(createCluster()).To(Succeed())

			Eventually(distributeCalled, 5).Should(Receive(), "Distribute was not called")
			Expect((*captured).GetLabels()).Should(HaveKeyWithValue(clusterIDLabelKey, clusterID))
		})
	})

	When("a Cluster resource is added and Distribute initially fails", func() {
		It("should retry until it succeeds", func() {
			// Simulate the first call to Distribute fails and the second succeeds.
			gomock.InOrder(
				mockFederator.EXPECT().Distribute(EqCluster(cluster)).Return(errors.New("mock")),
				mockFederator.EXPECT().Distribute(gomock.Any()).Return(nil).Do(func(c *submarinerv1.Cluster) {
					distributeCalled <- true
				}))

			Expect(createCluster()).To(Succeed())

			Eventually(distributeCalled, 5).Should(Receive(), "Distribute was not retried")
		})
	})

	When("a Cluster resource is added with a non-matching clusterID label", func() {
		It("should not distribute the resource", func() {
			cluster.SetLabels(map[string]string{clusterIDLabelKey: "west"})
			mockFederator.EXPECT().Distribute(EqCluster(cluster)).Return(nil).MaxTimes(0).Do(func(c *submarinerv1.Cluster) {
				distributeCalled <- true
			})

			Expect(createCluster()).To(Succeed())

			Consistently(distributeCalled).ShouldNot(Receive(), "Distribute was unexpectedly called")
		})
	})

	When("a Cluster resource is added with a matching clusterID label", func() {
		It("should distribute the resource", func() {
			cluster.SetLabels(map[string]string{clusterIDLabelKey: clusterID})
			mockFederator.EXPECT().Distribute(EqCluster(cluster)).Return(nil).Do(func(c *submarinerv1.Cluster) {
				distributeCalled <- true
			})

			Expect(createCluster()).To(Succeed())

			Eventually(distributeCalled, 5).Should(Receive(), "Distribute was not called")
		})
	})

	When("an Endpoint resource is added", func() {
		It("should distribute the resource", func() {
			var captured **submarinerv1.Endpoint = new(*submarinerv1.Endpoint)
			mockFederator.EXPECT().Distribute(EqEndpoint(endpoint)).Return(nil).Do(func(e *submarinerv1.Endpoint) {
				*captured = e
				distributeCalled <- true
			})

			Expect(createEndpoint()).To(Succeed())

			Eventually(distributeCalled, 5).Should(Receive(), "Distribute was not called")
			Expect((*captured).GetLabels()).Should(HaveKeyWithValue(clusterIDLabelKey, clusterID))
		})
	})

	When("an Endpoint resource is added and Distribute initially fails", func() {
		It("should retry until it succeeds", func() {
			// Simulate the first call to Distribute fails and the second succeeds.
			gomock.InOrder(
				mockFederator.EXPECT().Distribute(EqEndpoint(endpoint)).Return(errors.New("mock")),
				mockFederator.EXPECT().Distribute(gomock.Any()).Return(nil).Do(func(e *submarinerv1.Endpoint) {
					distributeCalled <- true
				}))

			Expect(createEndpoint()).To(Succeed())

			Eventually(distributeCalled, 5).Should(Receive(), "Distribute was not retried")
		})
	})

	When("an Endpoint resource is added with a non-matching clusterID label", func() {
		It("should not distribute the resource", func() {
			endpoint.SetLabels(map[string]string{clusterIDLabelKey: "west"})
			mockFederator.EXPECT().Distribute(EqEndpoint(endpoint)).Return(nil).MaxTimes(0).Do(func(e *submarinerv1.Endpoint) {
				distributeCalled <- true
			})

			Expect(createEndpoint()).To(Succeed())

			Consistently(distributeCalled).ShouldNot(Receive(), "Distribute was unexpectedly called")
		})
	})

	When("an Endpoint resource is added with a matching clusterID label", func() {
		It("should distribute the resource", func() {
			cluster.SetLabels(map[string]string{clusterIDLabelKey: clusterID})
			mockFederator.EXPECT().Distribute(EqEndpoint(endpoint)).Return(nil).Do(func(c *submarinerv1.Endpoint) {
				distributeCalled <- true
			})

			Expect(createEndpoint()).To(Succeed())

			Eventually(distributeCalled).Should(Receive(), "Distribute was not called")
		})
	})
}

func newEndpoint() *submarinerv1.Endpoint {
	return &submarinerv1.Endpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name: "east-endpoint",
		},
		Spec: submarinerv1.EndpointSpec{
			ClusterID: "east",
			CableName: "cable-east-10.20.30.40",
			Hostname:  "myhost",
			Subnets:   []string{"10.20.30.40/16"},
			PrivateIP: net.IP{1, 2, 3, 4},
		},
	}
}

func newCluster() *submarinerv1.Cluster {
	return &submarinerv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "east",
		},
		Spec: submarinerv1.ClusterSpec{
			ClusterID:   "east",
			ClusterCIDR: []string{"10.20.30.40/16"},
			ServiceCIDR: []string{"11.21.31.41/16"},
		},
	}
}

func newWatchReactor(c *controller) *watchReactor {
	fakeClientset := fakeSubm.NewSimpleClientset()

	w := &watchReactor{
		endpointsWatchStarted: make(chan bool, 1),
		clustersWatchStarted:  make(chan bool, 1),
	}

	fakeClientset.PrependWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		if action.GetResource().Resource == "endpoints" {
			w.endpointsWatchStarted <- true
		} else if action.GetResource().Resource == "clusters" {
			w.clustersWatchStarted <- true
		} else {
			fmt.Printf("Watch reactor received unexpected Resource: %s\n", action.GetResource().Resource)
		}
		return false, nil, nil
	})

	c.newSubmClientset = func(c *rest.Config) (submarinerClientset.Interface, error) {
		return fakeClientset, nil
	}

	return w
}

func (w *watchReactor) reset() {
	w.close()
	w.endpointsWatchStarted = make(chan bool, 1)
	w.clustersWatchStarted = make(chan bool, 1)
}

func (w *watchReactor) close() {
	close(w.endpointsWatchStarted)
	close(w.clustersWatchStarted)
}

func EqCluster(expected *submarinerv1.Cluster) *eqCluster {
	return &eqCluster{expected}
}

func (m *eqCluster) Matches(x interface{}) bool {
	actual, ok := x.(*submarinerv1.Cluster)
	if !ok {
		return false
	}
	return m.expected.Name == actual.Name && reflect.DeepEqual(m.expected.Spec, actual.Spec)
}

func (m *eqCluster) String() string {
	return fmt.Sprintf("is equal to %#v", m.expected)
}

func EqEndpoint(expected *submarinerv1.Endpoint) *eqEndpoint {
	return &eqEndpoint{expected}
}

func (m *eqEndpoint) Matches(x interface{}) bool {
	actual, ok := x.(*submarinerv1.Endpoint)
	if !ok {
		return false
	}
	return m.expected.Name == actual.Name && reflect.DeepEqual(m.expected.Spec, actual.Spec)
}

func (m *eqEndpoint) String() string {
	return fmt.Sprintf("is equal to %#v", m.expected)
}
