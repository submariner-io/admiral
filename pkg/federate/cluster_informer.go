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

package federate

import (
	"k8s.io/client-go/rest"
)

// ClusterEventHandler handles federated cluster lifecycle event notifications.
type ClusterEventHandler interface {
	// OnAdd is called when a cluster is added. The given 'kubeConfig' can be used to access
	// the cluster's kube API endpoint.
	OnAdd(clusterID string, kubeConfig *rest.Config)

	// OnUpdate is called when some aspect of a cluster's kube API endpoint configuration has changed.
	OnUpdate(clusterID string, kubeConfig *rest.Config)

	// OnRemove is called when a cluster is removed.
	OnRemove(clusterID string)
}

// ClusterInformer provides functionality to inform on federated cluster lifecycle events.
type ClusterInformer interface {
	// AddHandler adds a ClusterEventHandler to be notified when cluster lifecycle events occur.
	// The handler is notified asynchronously of the existing set of clusters via OnAdd events, one per cluster.
	AddHandler(handler ClusterEventHandler) error
}
