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

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

func (r *resourceSyncer) processNextWorkItem(key, name, ns string) (bool, error) {
	resourceOp := r.operationQueues.peek(key)

	if ns == namespaceKey {
		switch resourceOp.(type) {
		case deleteOperation:
			r.handleNamespaceDeleted(name)
		case createOperation:
			r.handleNamespaceAdded(name)
		}

		if r.operationQueues.remove(key, resourceOp) {
			r.workQueue.Enqueue(cache.ExplicitKey(key))
		}

		return false, nil
	}

	var (
		requeue bool
		err     error
	)

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

	r.log.V(log.DEBUG).Infof("Syncer %q retrieved %sd resource %q", r.config.Name, op, resource.GetName())
	r.log.V(log.TRACE).Infof("Syncer %q resource: %s", r.config.Name, resourceUtil.JSONStringer{Obj: resource})

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

		r.log.V(log.DEBUG).Infof("Syncer %q syncing resource %q", r.config.Name, resource.GetName())

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

		r.incOpCounter(op)

		r.log.V(log.DEBUG).Infof("Syncer %q successfully synced %q", r.config.Name, resource.GetName())
	}

	if requeue && op == Create && !exists {
		// Don't requeue a create operation if the resource no longer exists.
		requeue = false
	}

	return requeue, nil
}

func (r *resourceSyncer) handleDeleted(key string, deletedResource *unstructured.Unstructured) (bool, error) {
	r.log.V(log.DEBUG).Infof("Syncer %q informed of deleted resource %q", r.config.Name, key)

	if !r.shouldSync(deletedResource) {
		return false, nil
	}

	resource, transformed, requeue := r.transform(deletedResource, key, Delete)
	if resource != nil {
		r.log.V(log.DEBUG).Infof("Syncer %q deleting resource %q", r.config.Name, resource.GetName())

		deleted := true

		err := r.config.Federator.Delete(context.Background(), resource)
		if apierrors.IsNotFound(err) {
			r.log.V(log.DEBUG).Infof("Syncer %q: resource %q not found", r.config.Name, resource.GetName())

			deleted = false
			err = nil
		}

		if err != nil || r.onSuccessfulSync(resource, transformed, Delete) {
			return true, errors.Wrapf(err, "error deleting resource %q", key)
		}

		if deleted {
			r.incOpCounter(Delete)

			r.log.V(log.DEBUG).Infof("Syncer %q successfully deleted %q", r.config.Name, resource.GetName())
		}
	}

	return requeue, nil
}

func (r *resourceSyncer) shouldSync(resource *unstructured.Unstructured) bool {
	clusterID, found := getClusterIDLabel(resource)

	switch r.config.Direction {
	case LocalToRemote:
		if found {
			// This is the local -> remote case - only sync local resources w/o the label, assuming any resource with the
			// label originated from a remote source.
			r.log.V(log.DEBUG).Infof("Syncer %q: found cluster ID label %q - not syncing resource %q", r.config.Name,
				clusterID, resource.GetName())

			return false
		}
	case RemoteToLocal:
		if r.config.LocalClusterID != "" && (!found || clusterID == r.config.LocalClusterID) {
			// This is the remote -> local case - do not sync local resources
			r.log.V(log.DEBUG).Infof("Syncer %q: cluster ID label %q not present or matches local cluster ID %q - not syncing resource %q",
				r.config.Name, clusterID, r.config.LocalClusterID, resource.GetName())

			return false
		}
	case None:
	}

	return true
}
