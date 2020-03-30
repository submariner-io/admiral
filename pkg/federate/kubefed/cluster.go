package kubefed

import (
	"context"
	"fmt"
	"reflect"
	"time"

	federate "github.com/submariner-io/admiral/pkg/federate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type clusterEvent interface {
	run(handler federate.ClusterEventHandler)
}

type clusterAdded struct {
	clusterID  string
	kubeConfig *rest.Config
}

type clusterUpdated struct {
	clusterID  string
	kubeConfig *rest.Config
}

type clusterRemoved struct {
	clusterID string
}

func (f *Federator) AddHandler(handler federate.ClusterEventHandler) error {
	f.startInformerOnce.Do(func() {
		go func() {
			f.kubeFedClusterInformer.Run(f.stopChan)
			klog.Info("KubeFedCluster informer stopped")

			// Note: we need to lock here to protect access to clusterWatchers
			f.Lock()
			defer f.Unlock()

			for _, clusterWatcher := range f.clusterWatchers {
				clusterWatcher.eventQueue.ShutDown()
			}
		}()

		klog.Info("Started KubeFedCluster informer")
	})

	clusterWatcher := &clusterWatcher{
		eventQueue: workqueue.NewNamed(fmt.Sprintf("clusterWatcher for %T", handler)),
		handler:    handler,
	}

	f.Lock()
	defer f.Unlock()

	f.clusterWatchers = append(f.clusterWatchers, clusterWatcher)

	go clusterWatcher.eventLoop()

	for clusterID, kubeConfig := range f.clusterMap {
		clusterWatcher.enqueue(&clusterAdded{clusterID, kubeConfig})
	}

	klog.Infof("Federator added ClusterEventHandler \"%T\"", handler)

	return nil
}

func (f *Federator) OnAdd(obj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnAdd for %#v", obj)

	kubeFedCluster := obj.(*unstructured.Unstructured)
	clusterName := kubeFedCluster.GetName()

	kubeConfig, err := f.buildFederatedClusterConfig(kubeFedCluster)
	if err != nil {
		klog.Errorf("Error building kube config for federated cluster %#v: %v", kubeFedCluster, err)
		return
	}

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[clusterName] != nil {
		klog.V(2).Infof("An entry for KubeFedCluster %q already exists", clusterName)
		return
	}

	klog.Infof("KubeFedCluster %q has been added", clusterName)

	f.clusterMap[clusterName] = kubeConfig

	event := &clusterAdded{clusterName, kubeConfig}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) OnDelete(obj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnDelete for %#v", obj)

	kubeFedCluster, ok := obj.(*unstructured.Unstructured)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not convert object %#v to a KubeFedCluster", obj)
			return
		}
		kubeFedCluster, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			klog.Errorf("Could not convert object tombstone %#v to a KubeFedCluster", obj)
			return
		}
	}

	clusterName := kubeFedCluster.GetName()

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[clusterName] == nil {
		klog.Warningf("OnDelete - no cached entry for KubeFedCluster %q exists", clusterName)
		return
	}

	klog.Infof("KubeFedCluster %q has been deleted", clusterName)

	delete(f.clusterMap, clusterName)

	event := &clusterRemoved{clusterName}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) OnUpdate(oldObj, newObj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnUpdate - OLD OBJ: %#v\nNEW OBJ: %#v", oldObj, newObj)

	oldCluster := oldObj.(*unstructured.Unstructured)
	newCluster := newObj.(*unstructured.Unstructured)

	oldSpec, _, _ := unstructured.NestedMap(oldCluster.Object, SpecField)
	newSpec, _, _ := unstructured.NestedMap(newCluster.Object, SpecField)

	// KubeFedCluster has other fields/structs like KubeFedClusterStatus that may be updated periodically
	// (eg LastProbeTime). We're only interested in changes to the KubeFedClusterSpec which contains the
	// cluster's connection info so check if the old and new Specs differ before proceeding.
	if reflect.DeepEqual(oldSpec, newSpec) {
		klog.V(3).Infof("KubeFedClusterSpecs are equal - not updating")
		return
	}

	kubeConfig, err := f.buildFederatedClusterConfig(newCluster)
	if err != nil {
		klog.Errorf("Error building kube config for federated cluster %#v: %v", newCluster, err)
		return
	}

	clusterName := newCluster.GetName()

	klog.Infof("KubeFedCluster %q has been updated", clusterName)

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[clusterName] == nil {
		klog.Warningf("OnUpdate - no cached entry for KubeFedCluster %q exists", clusterName)
		return
	}

	f.clusterMap[clusterName] = kubeConfig

	event := &clusterUpdated{clusterName, kubeConfig}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) buildFederatedClusterConfig(kubeFedCluster *unstructured.Unstructured) (*rest.Config, error) {
	apiEndpoint, found, _ := unstructured.NestedString(kubeFedCluster.Object, SpecField, ApiEndpointField)
	if !found || apiEndpoint == "" {
		return nil, fmt.Errorf("the API endpoint is missing or empty")
	}

	secretName, found, _ := unstructured.NestedString(kubeFedCluster.Object, SpecField, SecretRefField, NameField)
	if !found || secretName == "" {
		return nil, fmt.Errorf("the secret name is missing or empty")
	}

	secret := &corev1.Secret{}
	if err := f.kubeFedClient.Get(context.TODO(), client.ObjectKey{Namespace: kubeFedNamespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("error getting Secret %q: %v", secretName, err)
	}

	token, tokenFound := secret.Data[corev1.ServiceAccountTokenKey]
	if !tokenFound || len(token) == 0 {
		return nil, fmt.Errorf("the secret %#v is missing a non-empty value for %q", secret, corev1.ServiceAccountTokenKey)
	}

	kubeConfig, err := clientcmd.BuildConfigFromFlags(apiEndpoint, "")
	if err != nil {
		return nil, err
	}

	caBundle, found, _ := unstructured.NestedString(kubeFedCluster.Object, SpecField, CaBundleField)
	if found {
		kubeConfig.CAData = []byte(caBundle)
	}

	kubeConfig.BearerToken = string(token)

	return kubeConfig, nil
}

func (f *Federator) initKubeFedClusterInformer(listerWatcher cache.ListerWatcher) {
	// Providing 0 duration to an informer indicates that resync should be delayed as long as possible
	resyncPeriod := 0 * time.Second
	_, informer := cache.NewInformer(listerWatcher, &unstructured.Unstructured{}, resyncPeriod, f)
	f.kubeFedClusterInformer = informer
}

func newKubeFedClusterListerWatcher(restConfig *rest.Config) (cache.ListerWatcher, error) {
	dynClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating dynamic Client: %v", err)
	}

	kubefedClustersRes := schema.GroupVersionResource{
		Group:    "core.kubefed.k8s.io",
		Version:  DefaultFederatedVersion,
		Resource: "kubefedclusters",
	}

	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return dynClient.Resource(kubefedClustersRes).Namespace(kubeFedNamespace).List(metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return dynClient.Resource(kubefedClustersRes).Namespace(kubeFedNamespace).Watch(options)
		},
	}, nil
}

func (c *clusterWatcher) enqueue(event clusterEvent) {
	c.eventQueue.Add(event)
}

func (c *clusterWatcher) eventLoop() {
	for {
		event, shutdown := c.eventQueue.Get()
		if shutdown {
			klog.V(2).Infof("ClusterEventHandler \"%T\" eventLoop stopped", c.handler)
			return
		}

		event.(clusterEvent).run(c.handler)
		c.eventQueue.Done(event)
	}
}

func (c *clusterAdded) run(handler federate.ClusterEventHandler) {
	handler.OnAdd(c.clusterID, c.kubeConfig)
}

func (c *clusterUpdated) run(handler federate.ClusterEventHandler) {
	handler.OnUpdate(c.clusterID, c.kubeConfig)
}

func (c *clusterRemoved) run(handler federate.ClusterEventHandler) {
	handler.OnRemove(c.clusterID)
}
