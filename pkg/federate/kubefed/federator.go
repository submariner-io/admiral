package kubefed

import (
	"flag"
	"fmt"
	"sync"

	"github.com/submariner-io/admiral/pkg/federate"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	kubeFedNamespace string
)

func init() {
	flag.StringVar(&kubeFedNamespace, "kubefed-namespace", "kube-federation-system", "The namespace in which the KubeFed control plane is deployed.")
}

type clusterWatcher struct {
	eventQueue workqueue.Interface
	handler    federate.ClusterEventHandler
}

type Federator struct {
	sync.Mutex
	clusterMap             map[string]*rest.Config
	kubeFedClient          client.Client
	kubeFedClusterInformer cache.Controller
	startInformerOnce      sync.Once
	clusterWatchers        []*clusterWatcher
	stopChan               <-chan struct{}
	scheme                 *runtime.Scheme
}

// New creates a new Federator instance.
func New(kubeFedConfig *rest.Config, stopChan <-chan struct{}) (*Federator, error) {
	kubeFedClient, err := client.New(kubeFedConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("error building kubernetes clientset: %v", err)
	}

	federator := &Federator{
		clusterMap:    make(map[string]*rest.Config),
		kubeFedClient: kubeFedClient,
		stopChan:      stopChan,
		scheme:        scheme.Scheme,
	}

	listerWatcher, err := newKubeFedClusterListerWatcher(kubeFedConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating KubeFedCluster ListerWatcher: %v", err)
	}

	federator.initKubeFedClusterInformer(listerWatcher)
	return federator, nil
}
