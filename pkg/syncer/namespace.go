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

	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/set"
)

func (r *resourceSyncer) recordNamespaceSeen(namespace string) {
	_, ok := r.missingNamespaces[namespace]
	if !ok {
		r.missingNamespaces[namespace] = set.New[string]()
	}
}

// handleMissingNamespace records the resource key to be retried when the namespace is created. It returns true if the
// namespace exists by the time the lock is acquired, in which case the caller should requeue the resource
// as handleNamespaceAdded may have already run and won't see the key.
func (r *resourceSyncer) handleMissingNamespace(key, namespace string) bool {
	r.log.Warningf("Syncer %q: Unable to distribute resource %q due to missing namespace %q", r.config.Name, key, namespace)

	if r.config.NamespaceInformer == nil {
		return false
	}

	r.missingNamespacesMutex.Lock()
	defer r.missingNamespacesMutex.Unlock()

	_, exists, _ := r.config.NamespaceInformer.GetStore().GetByKey(namespace)
	if exists {
		return true
	}

	r.recordNamespaceSeen(namespace)
	r.missingNamespaces[namespace].Insert(key)

	return false
}

func (r *resourceSyncer) handleNamespaceAdded(namespace string) {
	var keys []string

	r.missingNamespacesMutex.Lock()

	if keySet, ok := r.missingNamespaces[namespace]; ok {
		// Copy the keys while holding the lock.
		keys = keySet.UnsortedList()
	}

	delete(r.missingNamespaces, namespace)
	r.missingNamespacesMutex.Unlock()

	if len(keys) == 0 {
		return
	}

	r.log.V(log.DEBUG).Infof("Syncer %q: namespace %q created - re-queueing %d resources",
		r.config.Name, namespace, len(keys))

	for _, k := range keys {
		ns, name, _ := cache.SplitMetaNamespaceKey(k)
		r.RequeueResource(name, ns)
	}
}

func (r *resourceSyncer) handleNamespaceDeleted(ctx context.Context, namespace string) {
	keys := set.New[string]()

	for _, key := range r.store.ListKeys() {
		obj, exists, _ := r.store.GetByKey(key)
		if !exists {
			continue
		}

		resource, _, _ := r.transform(ctx, r.assertUnstructured(obj), key, Create)
		if resource == nil {
			continue
		}

		if resource.GetNamespace() == namespace {
			keys.Insert(key)
		}
	}

	// Don't hold the lock while transforming above as the Transform function may take a while.
	r.missingNamespacesMutex.Lock()
	defer r.missingNamespacesMutex.Unlock()

	keys = keys.Union(r.missingNamespaces[namespace])

	if keys.Len() > 0 {
		r.missingNamespaces[namespace] = keys
		r.log.Infof("Syncer %q: namespace %q deleted - recorded %d missing resources", r.config.Name, namespace, keys.Len())
	}
}
