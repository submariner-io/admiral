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
	resourceUtil "github.com/submariner-io/admiral/pkg/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

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
			continue
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
	if ok := cache.WaitForCacheSync(r.stopCh, r.cachesSynced...); !ok {
		// This means the cache was stopped.
		r.log.Warningf("Syncer %q failed to wait for informer cache to sync", r.config.Name)

		return defaultReturn
	}

	return run()
}
