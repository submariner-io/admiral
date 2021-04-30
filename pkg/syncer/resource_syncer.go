/*
© 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package syncer

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
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

const OrigNamespaceLabelKey = "submariner-io/originatingNamespace"

type SyncDirection int

const (
	None SyncDirection = iota

	// Resources are synced from a local source to a remote source.
	LocalToRemote

	// Resources are synced from a remote source to a local source.
	RemoteToLocal
)

func (d SyncDirection) String() string {
	switch d {
	case LocalToRemote:
		return "localToRemote"
	case RemoteToLocal:
		return "remoteToLocal"
	default:
		return "none"
	}
}

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

const (
	DirectionLabel  = "direction"
	OperationLabel  = "operation"
	SyncerNameLabel = "syncer_name"
)

// TransformFunc is invoked prior to syncing to transform the resource or evaluate if it should be synced.
// If nil is returned, the resource is not synced and, if the second return value is true, the resource is re-queued
// to be retried later.
type TransformFunc func(from runtime.Object, numRequeues int, op Operation) (runtime.Object, bool)

// OnSuccessfulSyncFunc is invoked after a successful sync operation.
type OnSuccessfulSyncFunc func(synced runtime.Object, op Operation)

type ResourceEquivalenceFunc func(obj1, obj2 *unstructured.Unstructured) bool

type ShouldProcessFunc func(obj *unstructured.Unstructured, op Operation) bool

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
	// By default all updates are processed.
	ResourcesEquivalent ResourceEquivalenceFunc

	// ShouldProcess function invoked to determine if a resource should be processed.
	ShouldProcess ShouldProcessFunc

	// WaitForCacheSync if true, waits for the informer cache to sync on Start. Default is true.
	WaitForCacheSync *bool

	// Scheme used to convert resource objects. By default the global k8s Scheme is used.
	Scheme *runtime.Scheme

	// ResyncPeriod if non-zero, the period at which resources will be re-synced regardless if anything changed. Default is 0.
	ResyncPeriod time.Duration

	// SyncCounterOpts if specified, used to create a gauge to record counter metrics.
	// Alternatively the gauge can be created directly and passed via the SyncCounter field,
	// in which case SyncCounterOpts is ignored.
	SyncCounterOpts *prometheus.GaugeOpts

	// SyncCounter if specified, used to record counter metrics.
	SyncCounter *prometheus.GaugeVec
}

type resourceSyncer struct {
	workQueue   workqueue.Interface
	informer    cache.Controller
	store       cache.Store
	config      ResourceSyncerConfig
	deleted     sync.Map
	created     sync.Map
	stopped     chan struct{}
	syncCounter *prometheus.GaugeVec
	stopCh      <-chan struct{}
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

	if syncer.config.SyncCounter != nil {
		syncer.syncCounter = syncer.config.SyncCounter
	} else if syncer.config.SyncCounterOpts != nil {
		syncer.syncCounter = prometheus.NewGaugeVec(
			*syncer.config.SyncCounterOpts,
			[]string{
				DirectionLabel,
				OperationLabel,
				SyncerNameLabel,
			},
		)
		prometheus.MustRegister(syncer.syncCounter)
	}

	syncer.workQueue = workqueue.New(config.Name)

	resourceClient := config.SourceClient.Resource(*gvr).Namespace(config.SourceNamespace)
	syncer.store, syncer.informer = cache.NewInformer(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = config.SourceLabelSelector
			options.FieldSelector = config.SourceFieldSelector
			return resourceClient.List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = config.SourceLabelSelector
			options.FieldSelector = config.SourceFieldSelector
			return resourceClient.Watch(context.TODO(), options)
		},
	}, &unstructured.Unstructured{}, config.ResyncPeriod, cache.ResourceEventHandlerFuncs{
		AddFunc:    syncer.onCreate,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	})

	return syncer, nil
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	klog.V(log.LIBDEBUG).Infof("Starting syncer %q", r.config.Name)

	r.stopCh = stopCh

	go func() {
		defer func() {
			r.stopped <- struct{}{}
			klog.V(log.LIBDEBUG).Infof("Syncer %q stopped", r.config.Name)
		}()
		defer r.workQueue.ShutDown()

		r.informer.Run(stopCh)
	}()

	if *r.config.WaitForCacheSync {
		klog.V(log.LIBDEBUG).Infof("Syncer %q waiting for informer cache to sync", r.config.Name)

		if ok := cache.WaitForCacheSync(stopCh, r.informer.HasSynced); !ok {
			return fmt.Errorf("failed to wait for informer cache to sync")
		}
	}

	r.workQueue.Run(stopCh, r.processNextWorkItem)

	klog.V(log.LIBDEBUG).Infof("Syncer %q started", r.config.Name)

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

func (r *resourceSyncer) ListResources() ([]runtime.Object, error) {
	if ok := cache.WaitForCacheSync(r.stopCh, r.informer.HasSynced); !ok {
		return nil, fmt.Errorf("failed to wait for informer cache to sync")
	}

	list := r.store.List()
	retObjects := make([]runtime.Object, 0, len(list))

	for _, obj := range list {
		converted := r.config.ResourceType.DeepCopyObject()
		err := r.config.Scheme.Convert(obj.(*unstructured.Unstructured), converted, nil)
		if err != nil {
			return nil, err
		}

		retObjects = append(retObjects, converted)
	}

	return retObjects, nil
}

func (r *resourceSyncer) Reconcile(resourceLister func() []runtime.Object) {
	go func() {
		if ok := cache.WaitForCacheSync(r.stopCh, r.informer.HasSynced); !ok {
			klog.Errorf("Unable to reconcile - failed to wait for informer cache to sync")
			return
		}

		resourceType := reflect.TypeOf(r.config.ResourceType)

		for _, resource := range resourceLister() {
			if reflect.TypeOf(resource) != resourceType {
				// This would happen if the custom transform function returned a different type. We would need a custom
				// reverse transform function to handle this. Possible future work, for now bail.
				klog.Warningf("Unable to reconcile type %T - expected type %T", resource, resourceType)
				return
			}

			metaObj := resourceUtil.ToMeta(resource)
			clusterID, found := getClusterIDLabel(resource)
			ns := r.config.SourceNamespace

			switch r.config.Direction {
			case None:
				ns = metaObj.GetNamespace()
			case RemoteToLocal:
				if !found || clusterID == r.config.LocalClusterID {
					continue
				}
			case LocalToRemote:
				if clusterID != r.config.LocalClusterID {
					continue
				}

				labels := metaObj.GetLabels()
				delete(labels, federate.ClusterIDLabelKey)
				metaObj.SetLabels(labels)

				if ns == metav1.NamespaceAll {
					ns = metaObj.GetLabels()[OrigNamespaceLabelKey]
				}
			}

			if ns == "" {
				klog.Warningf("Unable to reconcile resource %s/%s - cannot determine originating namespace",
					metaObj.GetNamespace(), metaObj.GetName())
				continue
			}

			metaObj.SetNamespace(ns)

			key, _ := cache.MetaNamespaceKeyFunc(resource)

			_, exists, _ := r.store.GetByKey(key)
			if exists {
				continue
			}

			obj, _ := resourceUtil.ToUnstructured(resource)
			r.deleted.Store(key, obj)
			r.workQueue.Enqueue(obj)
		}
	}()
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

	klog.V(log.LIBTRACE).Infof("Syncer %q retrieved %sd resource %q: %#v", r.config.Name, op, resource.GetName(), resource)

	if r.shouldSync(resource) {
		resource, transformed, requeue := r.transform(resource, key, op)
		if resource == nil {
			if !requeue {
				r.created.Delete(key)
			}

			return requeue, nil
		}

		if r.config.SourceNamespace == metav1.NamespaceAll && resource.GetNamespace() != "" {
			resource = resource.DeepCopy()
			_ = unstructured.SetNestedField(resource.Object, resource.GetNamespace(),
				util.MetadataField, util.LabelsField, OrigNamespaceLabelKey)
		}

		klog.V(log.LIBDEBUG).Infof("Syncer %q syncing resource %q", r.config.Name, resource.GetName())

		err = r.config.Federator.Distribute(resource)
		if err != nil {
			return true, err
		}

		r.onSuccessfulSync(resource, transformed, op)

		if r.syncCounter != nil {
			r.syncCounter.With(prometheus.Labels{
				DirectionLabel:  r.config.Direction.String(),
				OperationLabel:  op.String(),
				SyncerNameLabel: r.config.Name,
			}).Inc()
		}

		klog.V(log.LIBDEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, resource.GetName())
	}

	r.created.Delete(key)

	return false, nil
}

func (r *resourceSyncer) handleDeleted(key string) (bool, error) {
	klog.V(log.LIBDEBUG).Infof("Syncer %q informed of deleted resource %q", r.config.Name, key)

	obj, found := r.deleted.Load(key)
	if !found {
		klog.V(log.LIBDEBUG).Infof("Syncer %q: resource %q not found in deleted object cache", r.config.Name, key)
		return false, nil
	}

	r.deleted.Delete(key)

	deletedResource := obj.(*unstructured.Unstructured)
	if r.shouldSync(deletedResource) {
		resource, transformed, requeue := r.transform(deletedResource, key, Delete)
		if resource == nil {
			if requeue {
				r.deleted.Store(key, deletedResource)
			}

			return requeue, nil
		}

		klog.V(log.LIBDEBUG).Infof("Syncer %q deleting resource %q: %#v", r.config.Name, resource.GetName(), resource)

		err := r.config.Federator.Delete(resource)
		if apierrors.IsNotFound(err) {
			klog.V(log.LIBDEBUG).Infof("Syncer %q: resource %q not found - ignoring", r.config.Name, resource.GetName())
			return false, nil
		}

		if err != nil {
			r.deleted.Store(key, deletedResource)
			return true, err
		}

		r.onSuccessfulSync(resource, transformed, Delete)

		if r.syncCounter != nil {
			r.syncCounter.With(prometheus.Labels{
				DirectionLabel:  r.config.Direction.String(),
				OperationLabel:  Delete.String(),
				SyncerNameLabel: r.config.Name,
			}).Inc()
		}

		klog.V(log.LIBDEBUG).Infof("Syncer %q successfully deleted %q", r.config.Name, resource.GetName())
	}

	return false, nil
}

func (r *resourceSyncer) convert(from interface{}) runtime.Object {
	converted := r.config.ResourceType.DeepCopyObject()
	err := r.config.Scheme.Convert(from, converted, nil)
	if err != nil {
		klog.Errorf("Syncer %q: error converting %#v to %T: %v", r.config.Name, from, r.config.ResourceType, err)
		return nil
	}

	return converted
}

//nolint:interfacer //false positive for "`from` can be `k8s.io/apimachinery/pkg/runtime.Object`" as it returns 'from' as Unstructured
func (r *resourceSyncer) transform(from *unstructured.Unstructured, key string,
	op Operation) (*unstructured.Unstructured, runtime.Object, bool) {
	if r.config.Transform == nil {
		return from, nil, false
	}

	clusterID, _ := getClusterIDLabel(from)

	converted := r.convert(from)
	if converted == nil {
		return nil, nil, false
	}

	transformed, requeue := r.config.Transform(converted, r.workQueue.NumRequeues(key), op)
	if transformed == nil {
		klog.V(log.LIBDEBUG).Infof("Syncer %q: transform function returned nil - not syncing - requeue: %v", r.config.Name, requeue)
		return nil, nil, requeue
	}

	result, err := resourceUtil.ToUnstructured(transformed)
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
		converted = r.convert(resource)
		if converted == nil {
			return
		}
	}

	klog.V(log.LIBTRACE).Infof("Syncer %q: invoking OnSuccessfulSync function with: %#v", r.config.Name, converted)

	r.config.OnSuccessfulSync(converted, op)
}

func (r *resourceSyncer) onCreate(resource interface{}) {
	if !r.shouldProcess(resource.(*unstructured.Unstructured), Create) {
		return
	}

	key, _ := cache.MetaNamespaceKeyFunc(resource)
	v := true
	r.created.Store(key, &v)
	r.workQueue.Enqueue(resource)
}

func (r *resourceSyncer) onUpdate(oldObj, newObj interface{}) {
	if !r.shouldProcess(newObj.(*unstructured.Unstructured), Update) {
		return
	}

	oldResource := oldObj.(*unstructured.Unstructured)
	newResource := newObj.(*unstructured.Unstructured)

	if r.config.ResourcesEquivalent(oldResource, newResource) {
		klog.V(log.LIBTRACE).Infof("Syncer %q: objects equivalent on update - not queueing resource\nOLD: %#v\nNEW: %#v",
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
			klog.Errorf("Syncer %q: could not convert object %v to DeletedFinalStateUnknown", r.config.Name, obj)
			return
		}

		resource, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			klog.Errorf("Syncer %q: could not convert object tombstone %v to Unstructured", r.config.Name, tombstone.Obj)
			return
		}
	}

	if !r.shouldProcess(resource, Delete) {
		return
	}

	key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	r.deleted.Store(key, resource)

	r.workQueue.Enqueue(obj)
}

func (r *resourceSyncer) shouldProcess(resource *unstructured.Unstructured, op Operation) bool {
	if r.config.ShouldProcess != nil && !r.config.ShouldProcess(resource, op) {
		return false
	}

	return true
}

func (r *resourceSyncer) shouldSync(resource *unstructured.Unstructured) bool {
	clusterID, found := getClusterIDLabel(resource)

	switch r.config.Direction {
	case LocalToRemote:
		if found {
			// This is the local -> remote case - only sync local resources w/o the label, assuming any resource with the
			// label originated from a remote source.
			klog.V(log.LIBDEBUG).Infof("Syncer %q: found cluster ID label %q - not syncing resource %q", r.config.Name,
				clusterID, resource.GetName())
			return false
		}
	case RemoteToLocal:
		if r.config.LocalClusterID != "" && (!found || clusterID == r.config.LocalClusterID) {
			// This is the remote -> local case - do not sync local resources
			klog.V(log.LIBDEBUG).Infof("Syncer %q: cluster ID label %q not present or matches local cluster ID %q - not syncing resource %q",
				r.config.Name, clusterID, r.config.LocalClusterID, resource.GetName())
			return false
		}
	default:
	}

	return true
}

func getClusterIDLabel(resource runtime.Object) (string, bool) {
	clusterID, found := resourceUtil.ToMeta(resource).GetLabels()[federate.ClusterIDLabelKey]
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
