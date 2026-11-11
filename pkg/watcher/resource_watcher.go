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
	"reflect"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/federate"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/util"
	"github.com/submariner-io/admiral/pkg/workqueue"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type Interface interface {
	Start(stopCh <-chan struct{}) error
	ListResources(ofType runtime.Object, bySelector labels.Selector) []runtime.Object
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
	ResourceType        runtime.Object
	Handler             EventHandler
	ResourcesEquivalent syncer.ResourceEquivalenceFunc
	ShouldProcess       syncer.ShouldProcessFunc
	WorkQueueConfig     *workqueue.Config
	Name                string
	SourceNamespace     string
	SourceLabelSelector string
	SourceFieldSelector string
	MaxLogVerbosity     int
}

type Config struct {
	RestMapper       meta.RESTMapper
	Client           dynamic.Interface
	RestConfig       *rest.Config
	WaitForCacheSync *bool
	Scheme           *runtime.Scheme
	ResourceConfigs  []ResourceConfig
	ResyncPeriod     time.Duration
}

type resourceWatcher struct {
	syncers map[reflect.Type]syncer.Interface
}

func New(config *Config) (Interface, error) {
	var err error

	if len(config.ResourceConfigs) == 0 {
		return nil, errors.New("no resources to watch")
	}

	restMapper := config.RestMapper
	if restMapper == nil {
		if restMapper, err = util.BuildRestMapper(config.RestConfig); err != nil {
			return nil, errors.Wrap(err, "error building the REST mapper")
		}
	}

	client := config.Client
	if client == nil {
		if client, err = dynamic.NewForConfig(config.RestConfig); err != nil {
			return nil, errors.Wrap(err, "error creating dynamic client")
		}
	}

	watcher := &resourceWatcher{syncers: make(map[reflect.Type]syncer.Interface)}

	for i := range config.ResourceConfigs {
		rc := &config.ResourceConfigs[i]
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
			WorkQueueConfig:     rc.WorkQueueConfig,
			MaxLogVerbosity:     rc.MaxLogVerbosity,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "error creating resource syncer %q", rc.Name)
		}

		watcher.syncers[reflect.TypeOf(rc.ResourceType)] = s
	}

	return watcher, nil
}

func (r *resourceWatcher) Start(stopCh <-chan struct{}) error {
	for _, syncer := range r.syncers {
		err := syncer.Start(stopCh)
		if err != nil {
			return err //nolint:wrapcheck // OK to return the error as is.
		}
	}

	return nil
}

func (r *resourceWatcher) ListResources(ofType runtime.Object, bySelector labels.Selector) []runtime.Object {
	s, found := r.syncers[reflect.TypeOf(ofType)]
	if !found {
		panic(fmt.Errorf("no Syncer found for %#v", ofType))
	}

	if bySelector == nil {
		bySelector = labels.Everything()
	}

	return s.ListResourcesBySelector(bySelector)
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
