/*
SPDX-License-Identifier: Apache-2.0

Copyright Contributors to the Submariner project.

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

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/workqueue"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
	s := "unknown"

	switch d {
	case LocalToRemote:
		s = "localToRemote"
	case RemoteToLocal:
		s = "remoteToLocal"
	case None:
		s = "none"
	}

	return s
}

type Operation int

const (
	Create Operation = iota
	Update
	Delete
)

func (o Operation) String() string {
	s := "unknown"

	switch o {
	case Create:
		s = "create"
	case Update:
		s = "update"
	case Delete:
		s = "delete"
	}

	return s
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
type OnSuccessfulSyncFunc func(synced runtime.Object, op Operation) bool

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

	// OnSuccessfulSync function invoked after a successful sync operation. If true is returned, the resource is re-queued
	// to be retried later.
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
	log         log.Logger
}

func NewResourceSyncer(config *ResourceSyncerConfig) (Interface, error) {
	syncer := &resourceSyncer{
		config:  *config,
		stopped: make(chan struct{}),
		log:     log.Logger{Logger: logf.Log.WithName("ResourceSyncer")},
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

	rawType, gvr, err := util.ToUnstructuredResource(config.ResourceType, config.RestMapper)
	if err != nil {
		return nil, err //nolint:wrapcheck // OK to return the error as is.
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

	//nolint:wrapcheck // These are wrapper functions.
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
	}, rawType, config.ResyncPeriod, cache.ResourceEventHandlerFuncs{
		AddFunc:    syncer.onCreate,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	})

	return syncer, nil
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	r.log.V(log.LIBDEBUG).Infof("Starting syncer %q", r.config.Name)

	r.stopCh = stopCh

	go func() {
		defer func() {
			r.stopped <- struct{}{}
			r.log.V(log.LIBDEBUG).Infof("Syncer %q stopped", r.config.Name)
		}()
		defer r.workQueue.ShutDownWithDrain()

		r.informer.Run(stopCh)
	}()

	if *r.config.WaitForCacheSync {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q waiting for informer cache to sync", r.config.Name)

		if ok := cache.WaitForCacheSync(stopCh, r.informer.HasSynced); !ok {
			return fmt.Errorf("failed to wait for informer cache to sync")
		}
	}

	r.workQueue.Run(stopCh, r.processNextWorkItem)

	r.log.V(log.LIBDEBUG).Infof("Syncer %q started", r.config.Name)

	return nil
}

func (r *resourceSyncer) AwaitStopped() {
	<-r.stopped
}

func (r *resourceSyncer) GetResource(name, namespace string) (runtime.Object, bool, error) {
	obj, exists, err := r.store.GetByKey(namespace + "/" + name)
	if err != nil {
		return nil, false, errors.Wrap(err, "error retrieving resource")
	}

	if !exists {
		return nil, false, nil
	}

	return r.mustConvert(obj.(*unstructured.Unstructured)), true, nil
}

func (r *resourceSyncer) RequeueResource(name, namespace string) {
	obj, exists, err := r.store.GetByKey(namespace + "/" + name)
	if err != nil {
		r.log.Errorf(err, "Error retrieving resource - unable to requeue")
		return
	}

	if exists {
		r.onCreate(obj)
	}
}

func (r *resourceSyncer) ListResources() []runtime.Object {
	return r.ListResourcesBySelector(k8slabels.Everything())
}

func (r *resourceSyncer) ListResourcesBySelector(selector k8slabels.Selector) []runtime.Object {
	var retObjects []runtime.Object

	return r.runIfCacheSynced(retObjects, func() any {
		list := r.store.List()
		retObjects := make([]runtime.Object, 0, len(list))

		for _, obj := range list {
			if !selector.Matches(k8slabels.Set(resourceUtil.MustToMeta(obj).GetLabels())) {
				continue
			}

			retObjects = append(retObjects, r.mustConvert(obj.(*unstructured.Unstructured)))
		}

		return retObjects
	}).([]runtime.Object)
}

func (r *resourceSyncer) Reconcile(resourceLister func() []runtime.Object) {
	go func() {
		_ = r.runIfCacheSynced(nil, func() any {
			r.doReconcile(resourceLister)
			return nil
		})
	}()
}

func (r *resourceSyncer) doReconcile(resourceLister func() []runtime.Object) {
	resourceType := reflect.TypeOf(r.config.ResourceType)

	for _, resource := range resourceLister() {
		if reflect.TypeOf(resource) != resourceType {
			// This would happen if the custom transform function returned a different type. We would need a custom
			// reverse transform function to handle this. Possible future work, for now bail.
			r.log.Warningf("Unable to reconcile type %T - expected type %v", resource, resourceType)
			return
		}

		metaObj := resourceUtil.MustToMeta(resource)
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
			r.log.Warningf("Unable to reconcile resource %s/%s - cannot determine originating namespace",
				metaObj.GetNamespace(), metaObj.GetName())
			continue
		}

		metaObj.SetNamespace(ns)

		key, _ := cache.MetaNamespaceKeyFunc(resource)

		_, exists, _ := r.store.GetByKey(key)
		if exists {
			continue
		}

		obj := resourceUtil.MustToUnstructuredUsingScheme(resource, r.config.Scheme)
		r.deleted.Store(key, obj)
		r.workQueue.Enqueue(obj)
	}
}

func (r *resourceSyncer) runIfCacheSynced(defaultReturn any, run func() any) any {
	if ok := cache.WaitForCacheSync(r.stopCh, r.informer.HasSynced); !ok {
		// This means the cache was stopped.
		r.log.Warningf("Syncer %q failed to wait for informer cache to sync", r.config.Name)

		return defaultReturn
	}

	return run()
}

func (r *resourceSyncer) processNextWorkItem(key, _, _ string) (bool, error) {
	obj, exists, err := r.store.GetByKey(key)
	if err != nil {
		return true, errors.Wrapf(err, "error retrieving resource %q", key)
	}

	if !exists {
		return r.handleDeleted(key)
	}

	resource := r.assertUnstructured(obj)

	op := Update
	_, found := r.created.Load(key)
	if found {
		op = Create
	}

	r.log.V(log.LIBTRACE).Infof("Syncer %q retrieved %sd resource %q: %#v", r.config.Name, op, resource.GetName(), resource)

	if !r.shouldSync(resource) {
		r.created.Delete(key)
		return false, nil
	}

	resource, transformed, requeue := r.transform(resource, key, op)
	if resource != nil {
		if r.config.SourceNamespace == metav1.NamespaceAll && resource.GetNamespace() != "" {
			resource = resource.DeepCopy()
			_ = unstructured.SetNestedField(resource.Object, resource.GetNamespace(),
				util.MetadataField, util.LabelsField, OrigNamespaceLabelKey)
		}

		r.log.V(log.LIBDEBUG).Infof("Syncer %q syncing resource %q", r.config.Name, resource.GetName())

		err = r.config.Federator.Distribute(resource)
		if err != nil || r.onSuccessfulSync(resource, transformed, op) {
			return true, errors.Wrapf(err, "error distributing resource %q", key)
		}

		if r.syncCounter != nil {
			r.syncCounter.With(prometheus.Labels{
				DirectionLabel:  r.config.Direction.String(),
				OperationLabel:  op.String(),
				SyncerNameLabel: r.config.Name,
			}).Inc()
		}

		r.log.V(log.LIBDEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, resource.GetName())
	}

	if !requeue {
		r.created.Delete(key)
	}

	return requeue, nil
}

func (r *resourceSyncer) handleDeleted(key string) (bool, error) {
	r.log.V(log.LIBDEBUG).Infof("Syncer %q informed of deleted resource %q", r.config.Name, key)

	obj, found := r.deleted.Load(key)
	if !found {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q: resource %q not found in deleted object cache", r.config.Name, key)
		return false, nil
	}

	r.deleted.Delete(key)

	deletedResource := r.assertUnstructured(obj)
	if !r.shouldSync(deletedResource) {
		return false, nil
	}

	resource, transformed, requeue := r.transform(deletedResource, key, Delete)
	if resource != nil {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q deleting resource %q: %#v", r.config.Name, resource.GetName(), resource)

		deleted := true

		err := r.config.Federator.Delete(resource)
		if apierrors.IsNotFound(err) {
			r.log.V(log.LIBDEBUG).Infof("Syncer %q: resource %q not found", r.config.Name, resource.GetName())

			deleted = false
			err = nil
		}

		if err != nil || r.onSuccessfulSync(resource, transformed, Delete) {
			r.deleted.Store(key, deletedResource)
			return true, errors.Wrapf(err, "error deleting resource %q", key)
		}

		if deleted {
			if r.syncCounter != nil {
				r.syncCounter.With(prometheus.Labels{
					DirectionLabel:  r.config.Direction.String(),
					OperationLabel:  Delete.String(),
					SyncerNameLabel: r.config.Name,
				}).Inc()
			}

			r.log.V(log.LIBDEBUG).Infof("Syncer %q successfully deleted %q", r.config.Name, resource.GetName())
		}
	}

	if requeue {
		r.deleted.Store(key, deletedResource)
	}

	return requeue, nil
}

func (r *resourceSyncer) mustConvert(from interface{}) runtime.Object {
	converted := r.config.ResourceType.DeepCopyObject()
	err := r.config.Scheme.Convert(from, converted, nil)
	utilruntime.Must(err)

	return converted
}

//nolint:interfacer //false positive for "`from` can be `k8s.io/apimachinery/pkg/runtime.Object`" as it returns 'from' as Unstructured
func (r *resourceSyncer) transform(from *unstructured.Unstructured, key string,
	op Operation,
) (*unstructured.Unstructured, runtime.Object, bool) {
	if r.config.Transform == nil {
		return from, nil, false
	}

	clusterID, _ := getClusterIDLabel(from)

	converted := r.mustConvert(from)

	transformed, requeue := r.config.Transform(converted, r.workQueue.NumRequeues(key), op)
	if transformed == nil {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q: transform function returned nil - not syncing - requeue: %v", r.config.Name, requeue)
		return nil, nil, requeue
	}

	result := resourceUtil.MustToUnstructuredUsingScheme(transformed, r.config.Scheme)

	// Preserve the cluster ID label
	if clusterID != "" {
		_ = unstructured.SetNestedField(result.Object, clusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	return result, transformed, requeue
}

func (r *resourceSyncer) onSuccessfulSync(resource, converted runtime.Object, op Operation) bool {
	if r.config.OnSuccessfulSync == nil {
		return false
	}

	if converted == nil {
		converted = r.mustConvert(resource)
	}

	r.log.V(log.LIBTRACE).Infof("Syncer %q: invoking OnSuccessfulSync function with: %#v", r.config.Name, converted)

	return r.config.OnSuccessfulSync(converted, op)
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

	oldResource := r.assertUnstructured(oldObj)
	newResource := r.assertUnstructured(newObj)

	if r.config.ResourcesEquivalent(oldResource, newResource) {
		r.log.V(log.LIBTRACE).Infof("Syncer %q: objects equivalent on update - not queueing resource\nOLD: %#v\nNEW: %#v",
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
			r.log.Errorf(nil, "Syncer %q: could not convert object %v to DeletedFinalStateUnknown", r.config.Name, obj)
			return
		}

		resource, ok = tombstone.Obj.(*unstructured.Unstructured)
		if !ok {
			r.log.Errorf(nil, "Syncer %q: could not convert object tombstone %v to Unstructured", r.config.Name, tombstone.Obj)
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
			r.log.V(log.LIBDEBUG).Infof("Syncer %q: found cluster ID label %q - not syncing resource %q", r.config.Name,
				clusterID, resource.GetName())
			return false
		}
	case RemoteToLocal:
		if r.config.LocalClusterID != "" && (!found || clusterID == r.config.LocalClusterID) {
			// This is the remote -> local case - do not sync local resources
			r.log.V(log.LIBDEBUG).Infof("Syncer %q: cluster ID label %q not present or matches local cluster ID %q - not syncing resource %q",
				r.config.Name, clusterID, r.config.LocalClusterID, resource.GetName())
			return false
		}
	case None:
	}

	return true
}

func (r *resourceSyncer) assertUnstructured(obj interface{}) *unstructured.Unstructured {
	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		panic(fmt.Sprintf("Syncer %q received type %T instead of *Unstructured", r.config.Name, obj))
	}

	return u
}

func getClusterIDLabel(resource runtime.Object) (string, bool) {
	clusterID, found := resourceUtil.MustToMeta(resource).GetLabels()[federate.ClusterIDLabelKey]
	return clusterID, found
}
