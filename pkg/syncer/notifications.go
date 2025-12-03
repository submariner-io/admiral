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
	"reflect"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/workqueue"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func (r *resourceSyncer) onCreate(obj any, isInInitialList bool) {
	resource := r.assertUnstructured(obj)

	if !r.shouldProcess(resource, Create) {
		return
	}

	key, _ := cache.MetaNamespaceKeyFunc(resource)

	r.operationQueues.add(key, createOperation(resource))

	// If this is from the initial listing on startup then enqueue with low priority to prioritize newly
	// created or updated resources. Also don't enqueue with rate limiting since we already know this is
	// part of a one-time burst.
	if isInInitialList {
		r.workQueue.EnqueueWithOpts(resource, workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
	} else {
		r.workQueue.Enqueue(resource)
	}
}

func (r *resourceSyncer) onUpdate(oldObj, newObj any) {
	if !r.shouldProcess(newObj.(*unstructured.Unstructured), Update) {
		return
	}

	oldResource := r.assertUnstructured(oldObj)
	newResource := r.assertUnstructured(newObj)

	if r.config.ResourcesEquivalent(oldResource, newResource) {
		r.log.V(log.TRACE).Infof("Syncer %q: objects equivalent on update - not queueing resource\nOLD: %#v\nNEW: %#v",
			r.config.Name, oldResource, newResource)

		return
	}

	// If the resource version didn't change, that indicates a re-sync by the informer so enqueue at low priority.
	// We want to prioritize processing resources that did actually change.
	if oldResource.GetResourceVersion() == newResource.GetResourceVersion() {
		r.workQueue.EnqueueWithOpts(newObj, workqueue.EnqueueOpts{Priority: workqueue.LowPriority})
	} else {
		r.workQueue.Enqueue(newObj)
	}
}

func (r *resourceSyncer) onDelete(obj any) {
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

func (r *resourceSyncer) onSuccessfulSync(resource, converted runtime.Object, op Operation) bool {
	if r.config.OnSuccessfulSync == nil {
		return false
	}

	if converted == nil {
		converted = r.mustConvert(resource)
	}

	r.log.V(log.TRACE).Infof("Syncer %q: invoking OnSuccessfulSync function with: %#v", r.config.Name, converted)

	return r.config.OnSuccessfulSync(converted, op)
}

func (r *resourceSyncer) transform(from *unstructured.Unstructured, key string,
	op Operation,
) (*unstructured.Unstructured, runtime.Object, bool) {
	if r.config.Transform == nil {
		return from, nil, false
	}

	clusterID, _ := getClusterIDLabel(from)

	converted := r.mustConvert(from)

	transformed, requeue := r.config.Transform(converted, r.workQueue.NumRequeues(key), op)
	if transformed == nil || reflect.ValueOf(transformed).IsNil() {
		r.log.V(log.DEBUG).Infof("Syncer %q: transform function returned nil - not syncing - requeue: %v", r.config.Name, requeue)
		return nil, nil, requeue
	}

	result := resourceUtil.MustToUnstructuredUsingScheme(transformed, r.config.Scheme)

	// Preserve the cluster ID label
	if clusterID != "" {
		_ = unstructured.SetNestedField(result.Object, clusterID, util.MetadataField, util.LabelsField, federate.ClusterIDLabelKey)
	}

	return result, transformed, requeue
}

func (r *resourceSyncer) shouldProcess(resource *unstructured.Unstructured, op Operation) bool {
	return r.config.ShouldProcess == nil || r.config.ShouldProcess(resource, op)
}
