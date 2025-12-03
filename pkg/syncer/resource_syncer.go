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
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/workqueue"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/set"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

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
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				options.LabelSelector = config.SourceLabelSelector
				options.FieldSelector = config.SourceFieldSelector

				return resourceClient.List(ctx, options)
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = config.SourceLabelSelector
				options.FieldSelector = config.SourceFieldSelector

				return resourceClient.Watch(ctx, options)
			},
		},
		ObjectType:   rawType,
		ResyncPeriod: config.ResyncPeriod,
		Handler: cache.ResourceEventHandlerDetailedFuncs{
			AddFunc:    syncer.onCreate,
			UpdateFunc: syncer.onUpdate,
			DeleteFunc: syncer.onDelete,
		},
		Transform: resourceUtil.TrimManagedFields,
	})

	syncer.cachesSynced = append(syncer.cachesSynced, syncer.informer.HasSynced)

	return syncer, nil
}

func NewResourceSyncerWithSharedInformer(config *ResourceSyncerConfig, informer cache.SharedInformer) (Interface, error) {
	syncer, err := newResourceSyncer(config)
	if err != nil {
		return nil, err
	}

	syncer.store = informer.GetStore()

	reg, err := informer.AddEventHandlerWithResyncPeriod(cache.ResourceEventHandlerDetailedFuncs{
		AddFunc:    syncer.onCreate,
		UpdateFunc: syncer.onUpdate,
		DeleteFunc: syncer.onDelete,
	}, config.ResyncPeriod)
	if err != nil {
		return nil, errors.Wrapf(err, "error registering event handler")
	}

	syncer.cachesSynced = append(syncer.cachesSynced, reg.HasSynced)

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

	syncer.log.SetMaxVerbosity(config.MaxLogVerbosity)

	if f, ok := syncer.config.Federator.(federate.FederatorExt); ok {
		f.SetMaxVerbosity(config.MaxLogVerbosity)
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

	if syncer.config.DrainWorkQueueTimeout == 0 {
		syncer.config.DrainWorkQueueTimeout = time.Second * 5
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

	workqueueConfig := workqueue.DefaultConfigIfNil(syncer.config.WorkQueueConfig)
	if config.MaxLogVerbosity > workqueueConfig.MaxVerbosity {
		workqueueConfig.MaxVerbosity = config.MaxLogVerbosity
	}

	syncer.workQueue = workqueue.NewWithConfig(config.Name, workqueueConfig)

	if config.NamespaceInformer != nil {
		reg, err := config.NamespaceInformer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
			AddFunc: func(obj any, _ bool) {
				key := cache.NewObjectName(namespaceKey, resourceUtil.MustToMeta(obj).GetName()).String()
				syncer.operationQueues.add(key, createOperation(&unstructured.Unstructured{}))
				syncer.workQueue.Enqueue(cache.ExplicitKey(key))
			},
			DeleteFunc: func(obj any) {
				objName, err := cache.DeletionHandlingObjectToName(obj)
				utilruntime.Must(err)

				key := cache.NewObjectName(namespaceKey, objName.Name).String()
				syncer.operationQueues.add(key, deleteOperation(&unstructured.Unstructured{}))
				syncer.workQueue.Enqueue(cache.ExplicitKey(key))
			},
		})
		if err != nil {
			return nil, errors.Wrapf(err, "error registering namespace handler")
		}

		syncer.cachesSynced = append(syncer.cachesSynced, reg.HasSynced)
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
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			return resourceClient.List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			return resourceClient.Watch(ctx, options)
		},
	}, rawType, cache.SharedIndexInformerOptions{
		ResyncPeriod: config.ResyncPeriod,
	})

	//nolint:wrapcheck // OK to return the error as is.
	return informer, informer.SetTransform(resourceUtil.TrimManagedFields)
}

func (r *resourceSyncer) Start(stopCh <-chan struct{}) error {
	r.log.V(log.DEBUG).Infof("Starting syncer %q", r.config.Name)

	r.stopCh = stopCh

	go func() {
		defer func() {
			if r.config.SyncCounterOpts != nil {
				prometheus.Unregister(r.syncCounter)
			}

			if r.unregHandler != nil {
				r.unregHandler()
			}

			r.log.V(log.DEBUG).Infof("Syncer %q stopped", r.config.Name)
		}()
		defer r.shutDownWorkQueue()

		if r.informer != nil {
			r.informer.Run(stopCh)
		} else {
			<-r.stopCh
		}
	}()

	if *r.config.WaitForCacheSync {
		r.log.V(log.DEBUG).Infof("Syncer %q waiting for informer cache to sync", r.config.Name)

		_ = cache.WaitForCacheSync(stopCh, r.cachesSynced...)
	}

	r.workQueue.Run(r.processNextWorkItem)

	r.log.V(log.DEBUG).Infof("Syncer %q started", r.config.Name)

	return nil
}

func (r *resourceSyncer) shutDownWorkQueue() {
	shutDownWithDrain := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), r.config.DrainWorkQueueTimeout)
		defer cancel()

		return r.workQueue.ShutDownWithDrain(ctx)
	}

	for {
		err := shutDownWithDrain()
		if err == nil {
			break
		}

		r.log.Warning(err.Error())
	}

	close(r.stopped)
}

func (r *resourceSyncer) AwaitStopped(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, r.config.DrainWorkQueueTimeout)
		defer cancel()
	}

	select {
	case <-r.stopped:
	case <-ctx.Done():
		return errors.Wrapf(ctx.Err(), "syncer %q await stopped did not complete", r.config.Name)
	}

	return nil
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
		r.onCreate(obj, false)
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

func (r *resourceSyncer) incOpCounter(op Operation) {
	if r.syncCounter != nil {
		r.syncCounter.With(prometheus.Labels{
			DirectionLabel:  r.config.Direction.String(),
			OperationLabel:  op.String(),
			SyncerNameLabel: r.config.Name,
		}).Inc()
	}
}

func (r *resourceSyncer) mustConvert(from any) runtime.Object {
	converted := r.config.ResourceType.DeepCopyObject()
	err := r.config.Scheme.Convert(from, converted, nil)
	utilruntime.Must(err)

	return converted
}

func (r *resourceSyncer) assertUnstructured(obj any) *unstructured.Unstructured {
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
