package syncer

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/workqueue"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

type SyncDirection int

const (
	// Resources are synced from a local source to a remote source.
	LocalToRemote SyncDirection = iota

	// Resources are synced from a remote source to a local source.
	RemoteToLocal
)

// TransformFunc is invoked prior to syncing to transform the resource or evaluate if it should be synced.
// If nil is returned, the resource is not synced.
type TransformFunc func(from runtime.Object) runtime.Object

type ResourceSyncerConfig struct {
	// Name of this syncer used for logging.
	Name string

	// SourceClient the client used to obtain the resources to sync.
	SourceClient dynamic.Interface

	// SourceNamespace the namespace of the resources to sync.
	SourceNamespace string

	// LocalClusterID the cluster ID of the source client. This is used in conjunction with Direction to avoid sync loops.
	LocalClusterID string

	// Direction specifies how resources are synced. It is assumed that resources emanating from a remote source have
	// the cluster ID label specified by federate.ClusterIDLabelKey set appropriately. If set to LocalToRemote, only
	// resources that do not have a cluster ID label are synced. This avoids re-syncing non-local resources. If set to
	// RemoteToLocal, resources whose cluster ID label matches LocalClusterID are not synced. This avoids syncing
	// local resources from the remote source.
	Direction SyncDirection

	// RestMapper used to obtain GroupVersionResources.
	RestMapper meta.RESTMapper

	// Federator used to perform the syncing.
	Federator federate.Federator

	// ResourceType the type of the resources to sync.
	ResourceType runtime.Object

	// Transform function used to transform resources prior to syncing.
	Transform TransformFunc

	// Scheme used to convert resource objects. By default the global k8s Scheme is used.
	Scheme *runtime.Scheme
}

type resourceSyncer struct {
	workQueue workqueue.Interface
	informer  cache.Controller
	store     cache.Store
	config    ResourceSyncerConfig
	deleted   sync.Map
}

func NewResourceSyncer(config *ResourceSyncerConfig) (Interface, error) {
	syncer := &resourceSyncer{
		config: *config,
	}

	if syncer.config.Scheme == nil {
		syncer.config.Scheme = scheme.Scheme
	}

	_, gvr, err := util.ToUnstructuredResource(config.ResourceType, config.RestMapper)
	if err != nil {
		return nil, err
	}

	syncer.workQueue = workqueue.New(config.Name)

	resourceClient := config.SourceClient.Resource(*gvr).Namespace(config.SourceNamespace)
	syncer.store, syncer.informer = cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return resourceClient.List(metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return resourceClient.Watch(options)
		},
	}, &unstructured.Unstructured{}, 0, cache.ResourceEventHandlerFuncs{
		AddFunc:    syncer.workQueue.Enqueue,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	})

	return syncer, nil
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	klog.Infof("Starting syncer %q", r.config.Name)

	go func() {
		defer r.workQueue.ShutDown()
		r.informer.Run(stopCh)
	}()

	klog.Infof("Syncer %q waiting for informer cache to sync", r.config.Name)
	if ok := cache.WaitForCacheSync(stopCh, r.informer.HasSynced); !ok {
		return fmt.Errorf("failed to wait for informer cache to sync")
	}

	r.workQueue.Run(stopCh, r.processNextWorkItem)

	klog.Infof("Syncer %q started", r.config.Name)
	return nil
}

func (r *resourceSyncer) processNextWorkItem(key, name, ns string) (bool, error) {
	obj, exists, err := r.store.GetByKey(key)
	if err != nil {
		return true, err
	}

	if !exists {
		return r.handleDeleted(key)
	}

	resource := obj.(*unstructured.Unstructured)

	klog.V(log.DEBUG).Infof("Syncer %q retrieved added or updated resource: %#v", r.config.Name, resource)

	if r.shouldSync(resource) {
		resource = r.transform(resource)
		if resource == nil {
			return false, nil
		}

		klog.V(log.DEBUG).Infof("Syncing resource: %#v", resource)

		err = r.config.Federator.Distribute(resource)
		if err != nil {
			return true, err
		}

		klog.V(log.DEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, key)
	}

	return false, nil
}

func (r *resourceSyncer) handleDeleted(key string) (bool, error) {
	klog.V(log.DEBUG).Infof("Syncer %q informed of deleted resource %q", r.config.Name, key)

	obj, found := r.deleted.Load(key)
	if !found {
		klog.V(log.DEBUG).Infof("Syncer %q: resource %q not found in deleted object cache", r.config.Name, key)
		return false, nil
	}

	r.deleted.Delete(key)

	resource := obj.(*unstructured.Unstructured)
	if r.shouldSync(resource) {
		resource = r.transform(resource)
		if resource == nil {
			return false, nil
		}

		klog.V(log.DEBUG).Infof("Syncer %q deleting resource: %#v", r.config.Name, resource)

		err := r.config.Federator.Delete(resource)
		if apierrors.IsNotFound(err) {
			klog.V(log.DEBUG).Infof("Syncer %q: resource %q not found - ignoring", r.config.Name, key)
			return false, nil
		}

		if err != nil {
			r.deleted.Store(key, resource)
			return true, err
		}

		klog.V(log.DEBUG).Infof("Syncer %q successfully deleted %q", r.config.Name, key)
	}

	return false, nil
}

func (r *resourceSyncer) transform(from *unstructured.Unstructured) *unstructured.Unstructured {
	if r.config.Transform == nil {
		return from
	}

	clusterID, _ := getClusterIDLabel(from)

	converted := r.config.ResourceType.DeepCopyObject()
	err := r.config.Scheme.Convert(from, converted, nil)
	if err != nil {
		klog.Errorf("Syncer %q: error converting %#v to %T: %v", r.config.Name, from, r.config.ResourceType, err)
		return nil
	}

	transformed := r.config.Transform(converted)
	if transformed == nil {
		klog.V(log.DEBUG).Infof("Syncer %q: transform function returned nil - not syncing", r.config.Name)
		return nil
	}

	result, err := util.ToUnstructured(transformed)
	if err != nil {
		klog.Errorf("Syncer %q: error converting transform function result: %v", r.config.Name, err)
		return nil
	}

	// Preserve the cluster ID label
	if clusterID != "" {
		_ = unstructured.SetNestedField(result.Object, clusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	return result
}

func (r *resourceSyncer) onUpdate(old interface{}, new interface{}) {
	oldObj := old.(*unstructured.Unstructured)
	newObj := new.(*unstructured.Unstructured)

	equal := reflect.DeepEqual(oldObj.GetLabels(), newObj.GetLabels()) &&
		reflect.DeepEqual(oldObj.GetAnnotations(), newObj.GetAnnotations()) &&
		equality.Semantic.DeepEqual(r.getSpec(oldObj), r.getSpec(newObj))
	if equal {
		klog.V(log.TRACE).Infof("Syncer %q: objects equivalent on update - not queueing resource\nOLD: %#v\nNEW: %#v",
			r.config.Name, old, new)
		return
	}

	r.workQueue.Enqueue(new)
}

func (r *resourceSyncer) getSpec(obj *unstructured.Unstructured) interface{} {
	spec, _, err := unstructured.NestedFieldNoCopy(obj.Object, "spec")
	if err != nil {
		klog.Errorf("Syncer %q: error retrieving spec field for %#v: %v", r.config.Name, obj, err)
	}

	return spec
}

func (r *resourceSyncer) onDelete(obj interface{}) {
	var resource *unstructured.Unstructured
	var ok bool
	if resource, ok = obj.(*unstructured.Unstructured); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Could not convert object %v to DeletedFinalStateUnknown", obj)
			return
		}

		resource, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			klog.Errorf("Could not convert object tombstone %v to Unstructured", tombstone.Obj)
			return
		}
	}

	key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	r.deleted.Store(key, resource)

	r.workQueue.Enqueue(obj)
}

func (r *resourceSyncer) shouldSync(resource *unstructured.Unstructured) bool {
	clusterID, found := getClusterIDLabel(resource)
	if r.config.Direction == LocalToRemote && found {
		// This is the local -> remote case - only sync local resources w/o the label
		klog.V(log.DEBUG).Infof("Found cluster ID label %q - not syncing resource %q", clusterID, resource.GetName())
		return false
	}

	if r.config.Direction == RemoteToLocal && (!found || clusterID == r.config.LocalClusterID) {
		// This is the remote -> local case - do not sync local resources
		klog.V(log.DEBUG).Infof("Cluster ID label %q matches local cluster ID - not syncing resource %q",
			clusterID, resource.GetName())
		return false
	}

	return true
}

func getClusterIDLabel(resource *unstructured.Unstructured) (string, bool) {
	clusterID, found, _ := unstructured.NestedString(resource.Object, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	return clusterID, found
}
