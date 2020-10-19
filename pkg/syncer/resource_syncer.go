package syncer

import (
	"fmt"
	"reflect"
	"sync"
	"time"

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

type Operation int

const (
	Create Operation = iota
	Update
	Delete
)

func (o Operation) String() string {
	switch o {
	case Create:
		return "create"
	case Update:
		return "update"
	default:
		return "delete"
	}
}

// TransformFunc is invoked prior to syncing to transform the resource or evaluate if it should be synced.
// If nil is returned, the resource is not synced and, if the second return value is true, the resource is re-queued
// to be retried later.
type TransformFunc func(from runtime.Object, op Operation) (runtime.Object, bool)

// OnSuccessfulSyncFunc is invoked after a successful sync operation.
type OnSuccessfulSyncFunc func(synced runtime.Object, op Operation)

type ResourceEquivalenceFunc func(obj1, obj2 *unstructured.Unstructured) bool

type ResourceSyncerConfig struct {
	// Name of this syncer used for logging.
	Name string

	// SourceClient the client used to obtain the resources to sync.
	SourceClient dynamic.Interface

	// SourceNamespace the namespace of the resources to sync.
	SourceNamespace string

	// SourceLabelSelector optional selector to restrict the resources to sync by their labels.
	SourceLabelSelector string

	// SourceFieldSelector optional selector to restrict the resources to sync by their fields.
	SourceFieldSelector string

	// LocalClusterID the cluster ID of the source client. This is used in conjunction with Direction to avoid
	// loops when syncing the same resources between the local and remote sources.
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

	// OnSuccessfulSync function invoked after a successful sync operation.
	OnSuccessfulSync OnSuccessfulSyncFunc

	// ResourcesEquivalent function to compare two resources for equivalence. This is invoked on an update notification
	// to compare the old and new resources. If true is returned, the update is ignored, otherwise the update is processed.
	// The default is DefaultResourcesEquivalent which checks annotations, labels and the Spec, if present.
	ResourcesEquivalent ResourceEquivalenceFunc

	// WaitForCacheSync if true, waits for the informer cache to sync on Start. Default is true.
	WaitForCacheSync *bool

	// Scheme used to convert resource objects. By default the global k8s Scheme is used.
	Scheme *runtime.Scheme

	// ResyncPeriod if non-zero, the period at which resources will be re-synced regardless if anything changed. Default is 0.
	ResyncPeriod time.Duration
}

type resourceSyncer struct {
	workQueue workqueue.Interface
	informer  cache.Controller
	store     cache.Store
	config    ResourceSyncerConfig
	deleted   sync.Map
	created   sync.Map
	stopped   chan struct{}
}

func NewResourceSyncer(config *ResourceSyncerConfig) (Interface, error) {
	syncer := &resourceSyncer{
		config:  *config,
		stopped: make(chan struct{}),
	}

	if syncer.config.Scheme == nil {
		syncer.config.Scheme = scheme.Scheme
	}

	if syncer.config.ResourcesEquivalent == nil {
		syncer.config.ResourcesEquivalent = ResourcesNotEquivalent
	}

	if syncer.config.WaitForCacheSync == nil {
		wait := true
		syncer.config.WaitForCacheSync = &wait
	}

	_, gvr, err := util.ToUnstructuredResource(config.ResourceType, config.RestMapper)
	if err != nil {
		return nil, err
	}

	syncer.workQueue = workqueue.New(config.Name)

	resourceClient := config.SourceClient.Resource(*gvr).Namespace(config.SourceNamespace)
	syncer.store, syncer.informer = cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = config.SourceLabelSelector
			options.FieldSelector = config.SourceFieldSelector
			return resourceClient.List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = config.SourceLabelSelector
			options.FieldSelector = config.SourceFieldSelector
			return resourceClient.Watch(options)
		},
	}, &unstructured.Unstructured{}, config.ResyncPeriod, cache.ResourceEventHandlerFuncs{
		AddFunc:    syncer.onCreate,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	})

	return syncer, nil
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	klog.Infof("Starting syncer %q", r.config.Name)

	go func() {
		defer func() {
			r.stopped <- struct{}{}
			klog.Infof("Syncer %q stopped", r.config.Name)
		}()
		defer r.workQueue.ShutDown()

		r.informer.Run(stopCh)
	}()

	if *r.config.WaitForCacheSync {
		klog.Infof("Syncer %q waiting for informer cache to sync", r.config.Name)

		if ok := cache.WaitForCacheSync(stopCh, r.informer.HasSynced); !ok {
			return fmt.Errorf("failed to wait for informer cache to sync")
		}
	}

	r.workQueue.Run(stopCh, r.processNextWorkItem)

	klog.Infof("Syncer %q started", r.config.Name)

	return nil
}

func (r *resourceSyncer) AwaitStopped() {
	<-r.stopped
}

func (r *resourceSyncer) GetResource(name, namespace string) (runtime.Object, bool, error) {
	obj, exists, err := r.store.GetByKey(namespace + "/" + name)
	if err != nil {
		return nil, false, err
	}

	if !exists {
		return nil, false, nil
	}

	converted := r.config.ResourceType.DeepCopyObject()
	err = r.config.Scheme.Convert(obj.(*unstructured.Unstructured), converted, nil)
	if err != nil {
		return nil, false, err
	}

	return converted, true, nil
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

	op := Update
	_, found := r.created.Load(key)
	if found {
		op = Create
	}

	klog.V(log.DEBUG).Infof("Syncer %q retrieved %sd resource: %#v", r.config.Name, op, resource)

	if r.shouldSync(resource) {
		resource, transformed, requeue := r.transform(resource, op)
		if resource == nil {
			if !requeue {
				r.created.Delete(key)
			}

			return requeue, nil
		}

		klog.V(log.DEBUG).Infof("Syncing resource: %#v", resource)

		err = r.config.Federator.Distribute(resource)
		if err != nil {
			return true, err
		}

		r.onSuccessfulSync(resource, transformed, op)

		klog.V(log.DEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, key)
	}

	r.created.Delete(key)

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

	deletedResource := obj.(*unstructured.Unstructured)
	if r.shouldSync(deletedResource) {
		resource, transformed, requeue := r.transform(deletedResource, Delete)
		if resource == nil {
			if requeue {
				r.deleted.Store(key, deletedResource)
			}

			return requeue, nil
		}

		klog.V(log.DEBUG).Infof("Syncer %q deleting resource: %#v", r.config.Name, resource)

		err := r.config.Federator.Delete(resource)
		if apierrors.IsNotFound(err) {
			klog.V(log.DEBUG).Infof("Syncer %q: resource %q not found - ignoring", r.config.Name, key)
			return false, nil
		}

		if err != nil {
			r.deleted.Store(key, deletedResource)
			return true, err
		}

		r.onSuccessfulSync(resource, transformed, Delete)

		klog.V(log.DEBUG).Infof("Syncer %q successfully deleted %q", r.config.Name, key)
	}

	return false, nil
}

func (r *resourceSyncer) transform(from *unstructured.Unstructured, op Operation) (*unstructured.Unstructured, runtime.Object, bool) {
	if r.config.Transform == nil {
		return from, nil, false
	}

	clusterID, _ := getClusterIDLabel(from)

	converted := r.config.ResourceType.DeepCopyObject()
	err := r.config.Scheme.Convert(from, converted, nil)
	if err != nil {
		klog.Errorf("Syncer %q: error converting %#v to %T: %v", r.config.Name, from, r.config.ResourceType, err)
		return nil, nil, false
	}

	transformed, requeue := r.config.Transform(converted, op)
	if transformed == nil {
		klog.V(log.DEBUG).Infof("Syncer %q: transform function returned nil - not syncing - requeue: %v", r.config.Name, requeue)
		return nil, nil, requeue
	}

	result, err := util.ToUnstructured(transformed)
	if err != nil {
		klog.Errorf("Syncer %q: error converting transform function result: %v", r.config.Name, err)
		return nil, nil, false
	}

	// Preserve the cluster ID label
	if clusterID != "" {
		_ = unstructured.SetNestedField(result.Object, clusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	return result, transformed, false
}

func (r *resourceSyncer) onSuccessfulSync(resource, converted runtime.Object, op Operation) {
	if r.config.OnSuccessfulSync == nil {
		return
	}

	if converted == nil {
		converted = r.config.ResourceType.DeepCopyObject()
		err := r.config.Scheme.Convert(resource, converted, nil)
		if err != nil {
			klog.Errorf("Syncer %q: error converting %#v to %T: %v", r.config.Name, resource, r.config.ResourceType, err)
			return
		}
	}

	klog.V(log.DEBUG).Infof("Syncer %q: invoking OnSuccessfulSync function with: %#v", r.config.Name, converted)

	r.config.OnSuccessfulSync(converted, op)
}

func (r *resourceSyncer) onCreate(resource interface{}) {
	key, _ := cache.MetaNamespaceKeyFunc(resource)
	v := true
	r.created.Store(key, &v)
	r.workQueue.Enqueue(resource)
}

func (r *resourceSyncer) onUpdate(oldObj, newObj interface{}) {
	oldResource := oldObj.(*unstructured.Unstructured)
	newResource := newObj.(*unstructured.Unstructured)

	if r.config.ResourcesEquivalent(oldResource, newResource) {
		klog.V(log.TRACE).Infof("Syncer %q: objects equivalent on update - not queueing resource\nOLD: %#v\nNEW: %#v",
			r.config.Name, oldResource, newResource)
		return
	}

	r.workQueue.Enqueue(newObj)
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
		// This is the local -> remote case - only sync local resources w/o the label, assuming any resource with the
		// label originated from a remote source.
		klog.V(log.DEBUG).Infof("Found cluster ID label %q - not syncing resource %q", clusterID, resource.GetName())
		return false
	}

	if r.config.Direction == RemoteToLocal && r.config.LocalClusterID != "" && (!found || clusterID == r.config.LocalClusterID) {
		// This is the remote -> local case - do not sync local resources
		klog.V(log.DEBUG).Infof("Cluster ID label %q not present or matches local cluster ID %q - not syncing resource %q",
			clusterID, r.config.LocalClusterID, resource.GetName())
		return false
	}

	return true
}

func getClusterIDLabel(resource *unstructured.Unstructured) (string, bool) {
	clusterID, found, _ := unstructured.NestedString(resource.Object, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	return clusterID, found
}

func DefaultResourcesEquivalent(obj1, obj2 *unstructured.Unstructured) bool {
	return reflect.DeepEqual(obj1.GetLabels(), obj2.GetLabels()) &&
		reflect.DeepEqual(obj1.GetAnnotations(), obj2.GetAnnotations()) &&
		equality.Semantic.DeepEqual(util.GetSpec(obj1), util.GetSpec(obj2))
}

func ResourcesNotEquivalent(obj1, obj2 *unstructured.Unstructured) bool {
	return false
}
