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
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	fedv1 "sigs.k8s.io/kubefed/pkg/apis/core/v1beta1"
	"sigs.k8s.io/kubefed/pkg/client/generic/scheme"
)

const (
	tokenKey = "token"

	defaultEnqueueTimerInterval = time.Second * 10
)

func (f *federator) WatchClusters(handler federate.ClusterEventHandler) error {
	f.startInformerOnce.Do(func() {
		f.kubeFedClusterInformer.AddEventHandler(f)

		go func() {
			f.kubeFedClusterInformer.Run(f.stopChan)
			klog.Info("KubeFedCluster informer stopped")
		}()

		klog.Info("Started KubeFedCluster informer")
	})

	var handlerString string
	if stringer, ok := handler.(fmt.Stringer); ok {
		handlerString = stringer.String()
	} else {
		handlerString = reflect.TypeOf(handler).String()
	}

	clusterWatcher := &clusterWatcher{
		eventQueue:           make(chan func(), 1000),
		enqueueTimerInterval: defaultEnqueueTimerInterval,
		stopChan:             f.stopChan,
		handler:              handler,
		handlerString:        handlerString,
	}

	f.Lock()
	defer f.Unlock()

	f.clusterWatchers = append(f.clusterWatchers, clusterWatcher)

	go clusterWatcher.eventLoop()

	for clusterID, kubeConfig := range f.clusterMap {
		clusterWatcher.onAdd(clusterID, kubeConfig)
	}

	klog.Infof("Federator added ClusterEventHandler \"%s\"", handlerString)

	return nil
}

func (f *federator) OnAdd(obj interface{}) {
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

	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.onAdd(kubeFedCluster.Name, kubeConfig)
	}
}

func (f *federator) OnDelete(obj interface{}) {
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

	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.onRemove(kubeFedCluster.Name)
	}
}

func (f *federator) OnUpdate(oldObj, newObj interface{}) {
	klog.V(3).Infof("In federated cluster watcher OnUpdate - OLD OBJ: %#v\nNEW OBJ: %#v", oldObj, newObj)

	oldCluster := oldObj.(*fedv1.KubeFedCluster)
	newCluster := newObj.(*fedv1.KubeFedCluster)

	if reflect.DeepEqual(oldCluster.Spec, newCluster.Spec) {
		klog.V(2).Infof("KubeFedClusterSpecs are equal - not updating")
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

	for _, clusterWatcher := range f.clusterWatchers {
		clusterWatcher.onUpdate(newCluster.Name, kubeConfig)
	}
}

func (f *federator) buildFederatedClusterConfig(kubeFedCluster *fedv1.KubeFedCluster) (*rest.Config, error) {
	if kubeFedCluster.Spec.APIEndpoint == "" {
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

	token, tokenFound := secret.Data[tokenKey]
	if !tokenFound || len(token) == 0 {
		return nil, fmt.Errorf("the secret %#v is missing a non-empty value for %q", secret, tokenKey)
	}

	kubeConfig, err := clientcmd.BuildConfigFromFlags(kubeFedCluster.Spec.APIEndpoint, "")
	if err != nil {
		return nil, err
	}

	kubeConfig.CAData = kubeFedCluster.Spec.CABundle
	kubeConfig.BearerToken = string(token)

	return kubeConfig, nil
}

func newKubeFedClusterInformer(kubeFedConfig *rest.Config) (cache.SharedIndexInformer, error) {
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

	klog.V(3).Infof("Got RESTClient: %#v", client)

	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				var timeout time.Duration
				if options.TimeoutSeconds != nil {
					timeout = time.Duration(*options.TimeoutSeconds) * time.Second
				}

				result := &fedv1.KubeFedClusterList{}
				err := client.Get().Namespace(kubeFedNamespace).Resource(mapping.Resource.Resource).
					VersionedParams(&options, scheme.ParameterCodec).Timeout(timeout).Do().Into(result)
				return result, err
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				var timeout time.Duration
				if options.TimeoutSeconds != nil {
					timeout = time.Duration(*options.TimeoutSeconds) * time.Second
				}

				options.Watch = true
				return client.Get().Namespace(kubeFedNamespace).Resource(mapping.Resource.Resource).
					VersionedParams(&options, scheme.ParameterCodec).Timeout(timeout).Watch()
			},
		},
		obj,
		0, // Providing 0 duration to an informer indicates that resync should be delayed as long as possible
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	), nil
}

func (c *clusterWatcher) onAdd(clusterID string, kubeConfig *rest.Config) {
	c.enqueueEvent(func() {
		c.handler.OnAdd(clusterID, kubeConfig)
	}, clusterID, "Add")
}

func (c *clusterWatcher) onUpdate(clusterID string, kubeConfig *rest.Config) {
	c.enqueueEvent(func() {
		c.handler.OnUpdate(clusterID, kubeConfig)
	}, clusterID, "Update")
}

func (c *clusterWatcher) onRemove(clusterID string) {
	c.enqueueEvent(func() {
		c.handler.OnRemove(clusterID)
	}, clusterID, "Remove")
}

func (c *clusterWatcher) enqueueEvent(event func(), clusterID string, eventType string) {
	// First try to enqueue immediately to avoid the overhead of creating a Ticker for the common case.
	select {
	case c.eventQueue <- event:
		return
	default:
	}

	klog.V(2).Infof("Watcher for ClusterEventHandler \"%s\" eventQueue is full - starting timer loop", c.handlerString)

	// Next we loop indefinitely trying to enqueue and preriodically log a warning.

	startTime := time.Now()
	ticker := time.NewTicker(c.enqueueTimerInterval)
	defer ticker.Stop()

	for {
		select {
		case c.eventQueue <- event:
			klog.Infof("Watcher for ClusterEventHandler \"%s\" successfully enqueued %s event for cluster \"%s\" after %fs",
				c.handlerString, eventType, clusterID, time.Duration(time.Now().UnixNano()-startTime.UnixNano()).Seconds())
			return
		case <-ticker.C:
			klog.Warningf("Watcher for ClusterEventHandler \"%s\" timed out after %fs trying to enqueue %s event for cluster \"%s\"",
				c.handlerString, c.enqueueTimerInterval.Seconds(), eventType, clusterID)
		case <-c.stopChan:
			return
		}
	}
}

func (c *clusterWatcher) eventLoop() {
	for {
		select {
		case event := <-c.eventQueue:
			event()
		case <-c.stopChan:
			klog.V(2).Infof("ClusterEventHandler \"%s\" eventLoop stopped", c.handlerString)
			return
		}
	}
}
