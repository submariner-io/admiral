package federate

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

// ClusterEventHandler can handle notifications for events that happen to a federated cluster.
type ClusterEventHandler interface {
	// OnAdd is called when a cluster is added. The given 'kubeConfig' can be used to access
	// the cluster's kube API endpoint.

	OnAdd(clusterID string, kubeConfig *rest.Config)

	// OnUpdate is called when some aspect of a cluster's kube API endpoint configuration has changed.
	OnUpdate(clusterID string, kubeConfig *rest.Config)

	// OnRemove is called when a cluster is removed.
	OnRemove(clusterID string)
}

// Federator provides methods for accessing federated clusters.
type Federator interface {
	// WatchClusters adds a ClusterEventHandler to be notified when cluster events occur.
	// The handler is notified asynchronously of the existing set of clusters via OnAdd events, one per cluster.
	WatchClusters(handler ClusterEventHandler) error

	// Distribute distributes the given resource to the given list of clusters referred to by their IDs.
	// The actual distribution may occur asynchronously in which case any returned error only indicates that the request
	// failed.
	//
	// If the resource was previously distributed and the given resource differs, each previous cluster will receive the
	// updated resource. If previously distributed and the given list of clusters differs, the resource will be
	// distributed to any new clusters in the updated list and deleted from previous clusters not in the updated list.
	Distribute(resource runtime.Object, clusterIDs []string) error

	// Delete stops distributing the given resource and deletes it from all clusters to which it was distributed.
	// The actual deletion may occur asynchronously in which any returned error only indicates that the request
	// failed.
	Delete(resource runtime.Object) error
}
