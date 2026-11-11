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
	Metrics               MetricsConfig
	SourceClient          dynamic.Interface
	RestMapper            meta.RESTMapper
	Federator             federate.Federator
	ResourceType          runtime.Object
	NamespaceInformer     cache.SharedInformer
	WaitForCacheSync      *bool
	Scheme                *runtime.Scheme
	WorkQueueConfig       *workqueue.Config
	ShouldProcess         ShouldProcessFunc
	ResourcesEquivalent   ResourceEquivalenceFunc
	Transform             TransformFunc
	OnSuccessfulSync      OnSuccessfulSyncFunc
	LocalClusterID        string
	SourceFieldSelector   string
	Name                  string
	SourceNamespace       string
	SourceLabelSelector   string
	ResyncPeriod          time.Duration
	DrainWorkQueueTimeout time.Duration
	Direction             SyncDirection
	MaxLogVerbosity       int
}

type resourceSyncer struct {
	workQueue         workqueue.Interface
	informer          cache.Controller
	store             cache.Store
	operationQueues   *operationQueueMap
	stopped           chan struct{}
	metrics           *syncerMetrics
	stopCh            <-chan struct{}
	missingNamespaces map[string]set.Set[string]
	unregHandler      func()
	log               log.Logger
	cachesSynced      []cache.InformerSynced
	config            ResourceSyncerConfig
}
