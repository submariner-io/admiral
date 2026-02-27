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
	"time"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/workqueue"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/set"
)

const (
	OrigNamespaceLabelKey = "submariner-io/originatingNamespace"
	DirectionLabel        = "direction"
	OperationLabel        = "operation"
	SyncerNameLabel       = "syncer_name"
	namespaceKey          = "$namespace-key$"
)

type SyncDirection int

const (
	None SyncDirection = iota

	// LocalToRemote resources are synced from a local source to a remote source.
	LocalToRemote

	// RemoteToLocal resources are synced from a remote source to a local source.
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

	// NamespaceInformer if specified, used to retry resources that initially failed due to missing namespace.
	NamespaceInformer cache.SharedInformer

	// WorkQueueConfig if specified, configures the underlying work queue
	WorkQueueConfig *workqueue.Config

	// Metrics if specified, configures optional Prometheus metrics for federation, transform, and queue operations.
	Metrics MetricsConfig

	// DrainWorkQueueTimeout configures the maximum amount of time to wait for the work queue to drain on shutdown.
	// Default is 5 seconds.
	DrainWorkQueueTimeout time.Duration

	// MaxLogVerbosity configures the maximum verbosity for debug logging. Default is 0 which disables debug logging.
	MaxLogVerbosity int
}

type resourceSyncer struct {
	workQueue         workqueue.Interface
	cachesSynced      []cache.InformerSynced
	unregHandler      func()
	informer          cache.Controller
	store             cache.Store
	config            ResourceSyncerConfig
	operationQueues   *operationQueueMap
	stopped           chan struct{}
	metrics           *syncerMetrics
	stopCh            <-chan struct{}
	log               log.Logger
	missingNamespaces map[string]set.Set[string]
}
