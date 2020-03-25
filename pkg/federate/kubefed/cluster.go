package kubefed

import (
	"context"
	"fmt"
	"reflect"
	"time"

	federate "github.com/submariner-io/admiral/pkg/federate"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	fedv1 "sigs.k8s.io/kubefed/pkg/apis/core/v1beta1"
	"sigs.k8s.io/kubefed/pkg/client/generic/scheme"
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

	kubeFedCluster := obj.(*fedv1.KubeFedCluster)

	kubeConfig, err := f.buildFederatedClusterConfig(kubeFedCluster)
	if err != nil {
		klog.Errorf("Error building kube config for federated cluster %#v: %v", kubeFedCluster, err)
		return
	}

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[kubeFedCluster.Name] != nil {
		klog.V(2).Infof("An entry for KubeFedCluster \"%s\" already exists", kubeFedCluster.Name)
		return
	}

	klog.Infof("KubeFedCluster \"%s\" has been added", kubeFedCluster.Name)

	f.clusterMap[kubeFedCluster.Name] = kubeConfig

	event := &clusterAdded{kubeFedCluster.Name, kubeConfig}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) OnDelete(obj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnDelete for %#v", obj)

	kubeFedCluster, ok := obj.(*fedv1.KubeFedCluster)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not convert object %#v to a KubeFedCluster", obj)
			return
		}
		kubeFedCluster, ok = tombstone.Obj.(*fedv1.KubeFedCluster)
		if !ok {
			klog.Errorf("Could not convert object tombstone %#v to a KubeFedCluster", obj)
			return
		}
	}

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[kubeFedCluster.Name] == nil {
		klog.Warningf("OnDelete - no cached entry for KubeFedCluster \"%s\" exists", kubeFedCluster.Name)
		return
	}

	klog.Infof("KubeFedCluster \"%s\" has been deleted", kubeFedCluster.Name)

	delete(f.clusterMap, kubeFedCluster.Name)

	event := &clusterRemoved{kubeFedCluster.Name}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) OnUpdate(oldObj, newObj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnUpdate - OLD OBJ: %#v\nNEW OBJ: %#v", oldObj, newObj)

	oldCluster := oldObj.(*fedv1.KubeFedCluster)
	newCluster := newObj.(*fedv1.KubeFedCluster)

	// KubeFedCluster has other fields/structs like KubeFedClusterStatus that may be updated periodically
	// (eg LastProbeTime). We're only interested in changes to the KubeFedClusterSpec which contains the
	// cluster's connection info so check if the old and new Specs differ before proceeding.
	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		klog.V(3).Infof("KubeFedClusterSpecs are equal - not updating")
		return
	}

	kubeConfig, err := f.buildFederatedClusterConfig(newCluster)
	if err != nil {
		klog.Errorf("Error building kube config for federated cluster %#v: %v", newCluster, err)
		return
	}

	klog.Infof("KubeFedCluster \"%s\" has been updated", newCluster.Name)

	f.Lock()
	defer f.Unlock()

	if f.clusterMap[newCluster.Name] == nil {
		klog.Warningf("OnUpdate - no cached entry for KubeFedCluster \"%s\" exists", newCluster.Name)
		return
	}

	f.clusterMap[newCluster.Name] = kubeConfig

	event := &clusterUpdated{newCluster.Name, kubeConfig}
	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.enqueue(event)
	}
}

func (f *Federator) buildFederatedClusterConfig(kubeFedCluster *fedv1.KubeFedCluster) (*rest.Config, error) {
	apiEndpoint := kubeFedCluster.Spec.APIEndpoint
	if apiEndpoint == "" {
		return nil, fmt.Errorf("the API endpoint is empty")
	}

	secretName := kubeFedCluster.Spec.SecretRef.Name
	if secretName == "" {
		return nil, fmt.Errorf("the cluster does not have a secret name")
	}

	secret := &corev1.Secret{}
	if err := f.kubeFedClient.Get(context.TODO(), client.ObjectKey{Namespace: kubeFedNamespace, Name: secretName}, secret); err != nil {
		return nil, fmt.Errorf("error getting Secret \"%s\": %v", secretName, err)
	}

	token, tokenFound := secret.Data[corev1.ServiceAccountTokenKey]
	if !tokenFound || len(token) == 0 {
		return nil, fmt.Errorf("the secret %#v is missing a non-empty value for %q", secret, corev1.ServiceAccountTokenKey)
	}

	kubeConfig, err := clientcmd.BuildConfigFromFlags(apiEndpoint, "")
	if err != nil {
		return nil, err
	}

	kubeConfig.CAData = kubeFedCluster.Spec.CABundle
	kubeConfig.BearerToken = string(token)

	return kubeConfig, nil
}

func (f *Federator) initKubeFedClusterInformer(listerWatcher cache.ListerWatcher) {
	// Providing 0 duration to an informer indicates that resync should be delayed as long as possible
	resyncPeriod := 0 * time.Second
	_, informer := cache.NewInformer(listerWatcher, &fedv1.KubeFedCluster{}, resyncPeriod, f)
	f.kubeFedClusterInformer = informer
}

func newKubeFedClusterListerWatcher(kubeFedConfig *rest.Config) (cache.ListerWatcher, error) {
	var obj runtime.Object = &fedv1.KubeFedCluster{}

	gvk, err := apiutil.GVKForObject(obj, scheme.Scheme)
	if err != nil {
		return nil, fmt.Errorf("error finding GroupVersionKind for %T: %v", obj, err)
	}

	klog.V(3).Infof("GroupVersionKind for %T: %#v", obj, gvk)

	mapper, err := apiutil.NewDiscoveryRESTMapper(kubeFedConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating RESTMapper from config: %v", err)
	}

	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, fmt.Errorf("error creating RESTMapping for %#v: %v", gvk, err)
	}

	client, err := apiutil.RESTClientForGVK(gvk, kubeFedConfig, scheme.Codecs)
	if err != nil {
		return nil, fmt.Errorf("error creating RESTClient for %#v: %v", gvk, err)
	}

	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			result := &fedv1.KubeFedClusterList{}
			err := client.Get().Namespace(kubeFedNamespace).Resource(mapping.Resource.Resource).
				VersionedParams(&options, scheme.ParameterCodec).Do().Into(result)
			return result, err
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.Watch = true
			return client.Get().Namespace(kubeFedNamespace).Resource(mapping.Resource.Resource).
				VersionedParams(&options, scheme.ParameterCodec).Watch()
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
