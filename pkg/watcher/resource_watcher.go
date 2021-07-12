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

// Package watcher provides an interface that allows clients to watch for
// change events on Kubernetes resources.
package watcher

import (
	"fmt"
	"time"

	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type Interface interface {
	Start(stopCh <-chan struct{}) error
}

// EventHandler can handle notifications of events that happen to a resource. The bool return value from each event
// notification method indicates whether or not the resource should be re-queued to be retried later, typically in case
// of an error.
type EventHandler interface {
	OnCreate(obj runtime.Object, numRequeues int) bool
	OnUpdate(obj runtime.Object, numRequeues int) bool
	OnDelete(obj runtime.Object, numRequeues int) bool
}

// EventHandlerFuncs is an adaptor to let you easily specify as many or as few of the notification functions as you want
// while still implementing EventHandler.
type EventHandlerFuncs struct {
	OnCreateFunc func(obj runtime.Object, numRequeues int) bool
	OnUpdateFunc func(obj runtime.Object, numRequeues int) bool
	OnDeleteFunc func(obj runtime.Object, numRequeues int) bool
}

type ResourceConfig struct {
	// Name of this watcher used for logging.
	Name string

	// ResourceType the types of the resources to watch.
	ResourceType runtime.Object

	// Handler that is notified of create, update, and delete events.
	Handler EventHandler

	// ResourcesEquivalent function to compare two resources for equivalence. This is invoked on an update notification
	// to compare the old and new resources. If true is returned, the update is ignored, otherwise the update is processed.
	// By default all updates are processed.
	ResourcesEquivalent syncer.ResourceEquivalenceFunc

	// ShouldProcess function invoked to determine if a resource should be processed.
	ShouldProcess syncer.ShouldProcessFunc

	// SourceNamespace the namespace of the resources to watch.
	SourceNamespace string

	// SourceLabelSelector optional selector to restrict the resources to watch by their labels.
	SourceLabelSelector string

	// SourceFieldSelector optional selector to restrict the resources to watch by their fields.
	SourceFieldSelector string
}

type Config struct {
	// RestConfig the REST config used to access the resources to watch.
	RestConfig *rest.Config

	// RestMapper used to obtain GroupVersionResources. This is optional and is provided for unit testing in lieu of the
	// RestConfig. If not specified, one is created from the RestConfig.
	RestMapper meta.RESTMapper

	// Client the client used to access the resources to watch. This is optional and is provided for unit testing in lieu
	// of the RestConfig. If not specified, one is created from the RestConfig.
	Client dynamic.Interface

	// WaitForCacheSync if true, waits for the informer cache to sync on Start. Default is true.
	WaitForCacheSync *bool

	// Scheme used to convert resource objects. By default the global k8s Scheme is used.
	Scheme *runtime.Scheme

	// ResyncPeriod if non-zero, the period at which resources will be re-synced regardless if anything changed. Default is 0.
	ResyncPeriod time.Duration

	// ResourceConfigs the configurations for the resources to watch.
	ResourceConfigs []ResourceConfig
}

type resourceWatcher struct {
	syncers []Interface
}

func New(config *Config) (Interface, error) {
	var err error

	if len(config.ResourceConfigs) == 0 {
		return nil, fmt.Errorf("no resources to watch")
	}

	restMapper := config.RestMapper
	if restMapper == nil {
		if restMapper, err = util.BuildRestMapper(config.RestConfig); err != nil {
			return nil, err
		}
	}

	client := config.Client
	if client == nil {
		if client, err = dynamic.NewForConfig(config.RestConfig); err != nil {
			return nil, fmt.Errorf("error creating dynamic client: %v", err)
		}
	}

	watcher := &resourceWatcher{syncers: []Interface{}}

	for _, rc := range config.ResourceConfigs {
		handler := rc.Handler
		s, err := syncer.NewResourceSyncer(&syncer.ResourceSyncerConfig{
			Name:                rc.Name,
			SourceClient:        client,
			SourceNamespace:     rc.SourceNamespace,
			SourceLabelSelector: rc.SourceLabelSelector,
			SourceFieldSelector: rc.SourceFieldSelector,
			Direction:           syncer.RemoteToLocal,
			RestMapper:          restMapper,
			Federator:           federate.NewNoopFederator(),
			ResourceType:        rc.ResourceType,
			Transform: func(obj runtime.Object, numRequeues int, op syncer.Operation) (runtime.Object, bool) {
				switch op {
				case syncer.Create:
					return nil, handler.OnCreate(obj, numRequeues)
				case syncer.Update:
					return nil, handler.OnUpdate(obj, numRequeues)
				case syncer.Delete:
					return nil, handler.OnDelete(obj, numRequeues)
				}

				return nil, false
			},
			ResourcesEquivalent: rc.ResourcesEquivalent,
			ShouldProcess:       rc.ShouldProcess,
			WaitForCacheSync:    config.WaitForCacheSync,
			Scheme:              config.Scheme,
			ResyncPeriod:        config.ResyncPeriod,
		})

		if err != nil {
			return nil, err
		}

		watcher.syncers = append(watcher.syncers, s)
	}

	return watcher, nil
}

func (r *resourceWatcher) Start(stopCh <-chan struct{}) error {
	for _, syncer := range r.syncers {
		err := syncer.Start(stopCh)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r EventHandlerFuncs) OnCreate(obj runtime.Object, numRequeues int) bool {
	if r.OnCreateFunc != nil {
		return r.OnCreateFunc(obj, numRequeues)
	}

	return false
}

func (r EventHandlerFuncs) OnUpdate(obj runtime.Object, numRequeues int) bool {
	if r.OnUpdateFunc != nil {
		return r.OnUpdateFunc(obj, numRequeues)
	}

	return false
}

func (r EventHandlerFuncs) OnDelete(obj runtime.Object, numRequeues int) bool {
	if r.OnDeleteFunc != nil {
		return r.OnDeleteFunc(obj, numRequeues)
	}

	return false
}
