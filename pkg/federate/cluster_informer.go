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
