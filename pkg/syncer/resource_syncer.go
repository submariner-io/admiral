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
	"k8s.io/utils/set"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	OrigNamespaceLabelKey = "submariner-io/originatingNamespace"
	namespaceAddedKey     = "$namespace-added$"
	namespaceDeletedKey   = "$namespace-deleted$"
)

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

	// NamespaceInformer if specified, used to retry resources that initially failed due to missing namespace.
	NamespaceInformer cache.SharedInformer
}

type resourceSyncer struct {
	workQueue         workqueue.Interface
	hasSynced         func() bool
	unregHandler      func()
	informer          cache.Controller
	store             cache.Store
	config            ResourceSyncerConfig
	operationQueues   *operationQueueMap
	stopped           chan struct{}
	syncCounter       *prometheus.GaugeVec
	stopCh            <-chan struct{}
	log               log.Logger
	missingNamespaces map[string]set.Set[string]
}

func NewResourceSyncer(config *ResourceSyncerConfig) (Interface, error) {
	syncer, err := newResourceSyncer(config)
	if err != nil {
		return nil, err
	}

	rawType, gvr, err := util.ToUnstructuredResource(config.ResourceType, config.RestMapper)
	if err != nil {
		return nil, err //nolint:wrapcheck // OK to return the error as is.
	}

	resourceClient := config.SourceClient.Resource(*gvr).Namespace(config.SourceNamespace)

	syncer.store, syncer.informer = cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: &cache.ListWatch{
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
		},
		ObjectType:   rawType,
		ResyncPeriod: config.ResyncPeriod,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    syncer.onCreate,
			UpdateFunc: syncer.onUpdate,
			DeleteFunc: syncer.onDelete,
		},
		Transform: resourceUtil.TrimManagedFields,
	})

	syncer.hasSynced = syncer.informer.HasSynced

	return syncer, nil
}

func NewResourceSyncerWithSharedInformer(config *ResourceSyncerConfig, informer cache.SharedInformer) (Interface, error) {
	syncer, err := newResourceSyncer(config)
	if err != nil {
		return nil, err
	}

	syncer.store = informer.GetStore()

	reg, err := informer.AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerFuncs{
		AddFunc:    syncer.onCreate,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	}, config.ResyncPeriod)
	if err != nil {
		return nil, errors.Wrapf(err, "error registering event handler")
	}

	syncer.hasSynced = reg.HasSynced

	syncer.unregHandler = func() {
		_ = informer.RemoveEventHandler(reg)
	}

	return syncer, nil
}

func newResourceSyncer(config *ResourceSyncerConfig) (*resourceSyncer, error) {
	syncer := &resourceSyncer{
		config: *config,
		operationQueues: &operationQueueMap{
			queues: map[string][]any{},
		},
		stopped:           make(chan struct{}),
		log:               log.Logger{Logger: logf.Log.WithName("ResourceSyncer")},
		missingNamespaces: map[string]set.Set[string]{},
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

	if config.NamespaceInformer != nil {
		_, err := config.NamespaceInformer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
			AddFunc: func(obj interface{}, _ bool) {
				syncer.workQueue.Enqueue(cache.ExplicitKey(cache.NewObjectName(namespaceAddedKey, resourceUtil.MustToMeta(obj).GetName()).String()))
			},
			DeleteFunc: func(obj interface{}) {
				objName, err := cache.DeletionHandlingObjectToName(obj)
				utilruntime.Must(err)

				syncer.workQueue.Enqueue(cache.ExplicitKey(cache.NewObjectName(namespaceDeletedKey, objName.Name).String()))
			},
		})
		if err != nil {
			return nil, errors.Wrapf(err, "error registering namespace handler")
		}
	}

	return syncer, nil
}

func NewSharedInformer(config *ResourceSyncerConfig) (cache.SharedInformer, error) {
	rawType, gvr, err := util.ToUnstructuredResource(config.ResourceType, config.RestMapper)
	if err != nil {
		return nil, err //nolint:wrapcheck // OK to return the error as is.
	}

	resourceClient := config.SourceClient.Resource(*gvr).Namespace(config.SourceNamespace)

	informer := cache.NewSharedIndexInformerWithOptions(&cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return resourceClient.List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return resourceClient.Watch(context.TODO(), options)
		},
	}, rawType, cache.SharedIndexInformerOptions{
		ResyncPeriod: config.ResyncPeriod,
	})

	//nolint:wrapcheck // OK to return the error as is.
	return informer, informer.SetTransform(resourceUtil.TrimManagedFields)
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	r.log.V(log.LIBDEBUG).Infof("Starting syncer %q", r.config.Name)

	r.stopCh = stopCh

	go func() {
		defer func() {
			if r.config.SyncCounterOpts != nil {
				prometheus.Unregister(r.syncCounter)
			}

			if r.unregHandler != nil {
				r.unregHandler()
			}

			r.stopped <- struct{}{}
			r.log.V(log.LIBDEBUG).Infof("Syncer %q stopped", r.config.Name)
		}()
		defer r.workQueue.ShutDownWithDrain()

		if r.informer != nil {
			r.informer.Run(stopCh)
		} else {
			<-r.stopCh
		}
	}()

	if *r.config.WaitForCacheSync {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q waiting for informer cache to sync", r.config.Name)

		_ = cache.WaitForCacheSync(stopCh, r.hasSynced)
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
		r.operationQueues.add(key, deleteOperation(obj))
		r.workQueue.Enqueue(obj)
	}
}

func (r *resourceSyncer) runIfCacheSynced(defaultReturn any, run func() any) any {
	if ok := cache.WaitForCacheSync(r.stopCh, r.hasSynced); !ok {
		// This means the cache was stopped.
		r.log.Warningf("Syncer %q failed to wait for informer cache to sync", r.config.Name)

		return defaultReturn
	}

	return run()
}

func (r *resourceSyncer) processNextWorkItem(key, name, ns string) (bool, error) {
	if ns == namespaceAddedKey {
		r.handleNamespaceAdded(name)
		return false, nil
	}

	if ns == namespaceDeletedKey {
		r.handleNamespaceDeleted(name)
		return false, nil
	}

	var (
		requeue bool
		err     error
	)

	resourceOp := r.operationQueues.peek(key)

	switch t := resourceOp.(type) {
	case deleteOperation:
		requeue, err = r.handleDeleted(key, t)
	case createOperation:
		requeue, err = r.handleCreatedOrUpdated(key, t)
	default:
		requeue, err = r.handleCreatedOrUpdated(key, nil)
	}

	// If not re-queueing the current operation then remove it from the operation queue. If there's another operation queued
	// then add the key back to the work queue, so it's processed later. Note that we don't simply return true to re-queue
	// b/c we're not retrying the current operation, and we don't want the re-queue limit to be reached.
	if !requeue && r.operationQueues.remove(key, resourceOp) {
		r.workQueue.Enqueue(cache.ExplicitKey(key))
	}

	return requeue, err
}

func (r *resourceSyncer) handleCreatedOrUpdated(key string, created *unstructured.Unstructured) (bool, error) {
	resource := created

	op := Update
	if created != nil {
		op = Create
	}

	obj, exists, err := r.store.GetByKey(key)
	if err != nil {
		return true, errors.Wrapf(err, "error retrieving resource %q", key)
	}

	// Use the latest resource from the cache regardless of the operation. If it doesn't exist, for a create operation, this means
	// a deletion occurred afterward, in which case we'll process the 'created' resource.
	if exists {
		resource = r.assertUnstructured(obj)
	} else if op == Update {
		return false, nil
	}

	r.log.V(log.LIBTRACE).Infof("Syncer %q retrieved %sd resource %q: %#v", r.config.Name, op, resource.GetName(), resource)

	if !r.shouldSync(resource) {
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

		err = r.config.Federator.Distribute(context.Background(), resource)
		if err != nil || r.onSuccessfulSync(resource, transformed, op) {
			namespace := resourceUtil.ExtractMissingNamespaceFromErr(err)
			if namespace != "" {
				r.handleMissingNamespace(key, namespace)

				return false, nil
			}

			return true, errors.Wrapf(err, "error distributing resource %q", key)
		}

		r.recordNamespaceSeen(resource.GetNamespace())

		if r.syncCounter != nil {
			r.syncCounter.With(prometheus.Labels{
				DirectionLabel:  r.config.Direction.String(),
				OperationLabel:  op.String(),
				SyncerNameLabel: r.config.Name,
			}).Inc()
		}

		r.log.V(log.LIBDEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, resource.GetName())
	}

	return requeue, nil
}

func (r *resourceSyncer) handleDeleted(key string, deletedResource *unstructured.Unstructured) (bool, error) {
	r.log.V(log.LIBDEBUG).Infof("Syncer %q informed of deleted resource %q", r.config.Name, key)

	if !r.shouldSync(deletedResource) {
		return false, nil
	}

	resource, transformed, requeue := r.transform(deletedResource, key, Delete)
	if resource != nil {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q deleting resource %q: %#v", r.config.Name, resource.GetName(), resource)

		deleted := true

		err := r.config.Federator.Delete(context.Background(), resource)
		if apierrors.IsNotFound(err) {
			r.log.V(log.LIBDEBUG).Infof("Syncer %q: resource %q not found", r.config.Name, resource.GetName())

			deleted = false
			err = nil
		}

		if err != nil || r.onSuccessfulSync(resource, transformed, Delete) {
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

func (r *resourceSyncer) onCreate(obj interface{}) {
	resource := r.assertUnstructured(obj)

	if !r.shouldProcess(resource, Create) {
		return
	}

	key, _ := cache.MetaNamespaceKeyFunc(resource)

	r.operationQueues.add(key, createOperation(resource))
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
	switch t := obj.(type) {
	case cache.DeletedFinalStateUnknown:
		obj = t.Obj
	default:
	}

	resource := r.assertUnstructured(obj)

	if !r.shouldProcess(resource, Delete) {
		return
	}

	key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)

	r.operationQueues.add(key, deleteOperation(resource))
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

func (r *resourceSyncer) recordNamespaceSeen(namespace string) {
	_, ok := r.missingNamespaces[namespace]
	if !ok {
		r.missingNamespaces[namespace] = set.New[string]()
	}
}

func (r *resourceSyncer) handleMissingNamespace(key, namespace string) {
	r.log.Warningf("Syncer %q: Unable to distribute resource %q due to missing namespace %q", r.config.Name, key, namespace)

	if r.config.NamespaceInformer == nil {
		return
	}

	r.recordNamespaceSeen(namespace)
	r.missingNamespaces[namespace].Insert(key)
}

func (r *resourceSyncer) handleNamespaceAdded(namespace string) {
	keys, ok := r.missingNamespaces[namespace]
	if ok {
		r.log.V(log.LIBDEBUG).Infof("Syncer %q: namespace %q created - re-queueing %d resources", r.config.Name, namespace, keys.Len())

		for _, k := range keys.UnsortedList() {
			ns, name, _ := cache.SplitMetaNamespaceKey(k)
			r.RequeueResource(name, ns)
		}

		delete(r.missingNamespaces, namespace)
	}
}

func (r *resourceSyncer) handleNamespaceDeleted(namespace string) {
	keys, ok := r.missingNamespaces[namespace]
	if !ok {
		return
	}

	for _, key := range r.store.ListKeys() {
		obj, exists, _ := r.store.GetByKey(key)
		if !exists {
			continue
		}

		resource, _, _ := r.transform(r.assertUnstructured(obj), key, Create)
		if resource == nil {
			continue
		}

		if resource.GetNamespace() == namespace {
			keys.Insert(key)
		}
	}

	if keys.Len() > 0 {
		r.log.Infof("Syncer %q: namespace %q deleted - recorded %d missing resources", r.config.Name, namespace, keys.Len())
	}
}

func getClusterIDLabel(resource runtime.Object) (string, bool) {
	clusterID, found := resourceUtil.MustToMeta(resource).GetLabels()[federate.ClusterIDLabelKey]
	return clusterID, found
}
