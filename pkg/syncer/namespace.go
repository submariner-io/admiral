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
		r.log.V(log.DEBUG).Infof("Syncer %q: namespace %q created - re-queueing %d resources", r.config.Name, namespace, keys.Len())

		for _, k := range keys.UnsortedList() {
			ns, name, _ := cache.SplitMetaNamespaceKey(k)
			r.RequeueResource(name, ns)
		}
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
