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

// Package workqueue provides a simplified wrapper interface for Kubernetes workqueues.
package workqueue

import (
	"fmt"
	"time"

	"github.com/submariner-io/admiral/pkg/log"
	"golang.org/x/time/rate"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller/priorityqueue"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	LowPriority    = -100
	NormalPriority = 0
)

type ProcessFunc func(key, name, namespace string) (bool, error)

type Interface interface {
	Enqueue(obj interface{})
	EnqueueWithOpts(obj interface{}, opts EnqueueOpts)
	NumRequeues(key string) int
	Run(stopCh <-chan struct{}, process ProcessFunc)
	ShutDown()
	ShutDownWithDrain()
}

type EnqueueOpts struct {
	RateLimited bool
	Priority    int
}

type queueType struct {
	workqueue.TypedRateLimitingInterface[string]
	priorityQueue priorityqueue.PriorityQueue[string]
	name          string
}

var logger = log.Logger{Logger: logf.Log.WithName("WorkQueue")}

func New(name string) Interface {
	return NewWithConfig(name, DefaultConfig())
}

func NewWithConfig(name string, config Config) Interface {
	rateLimiter := workqueue.NewTypedWithMaxWaitRateLimiter(
		workqueue.NewTypedMaxOfRateLimiter(
			// exponential per-item rate limiter
			workqueue.NewTypedItemExponentialFailureRateLimiter[string](
				config.ItemRateLimiterBaseDelay, config.ItemRateLimiterMaxDelay),
			// overall rate limiter (not per item)
			&workqueue.TypedBucketRateLimiter[string]{Limiter: rate.NewLimiter(rate.Limit(config.BucketRateLimiterItemsPerSec),
				config.BucketRateLimiterMaxBurst)},
		), config.OverallRateLimiterMaxDelay)

	priorityQueue := priorityqueue.New(name, func(o *priorityqueue.Opts[string]) {
		o.RateLimiter = rateLimiter
		o.Log = logger.Logger
	})

	return &queueType{
		priorityQueue:              priorityQueue,
		TypedRateLimitingInterface: priorityQueue,
		name:                       name,
	}
}

func (q *queueType) Enqueue(obj interface{}) {
	q.EnqueueWithOpts(obj, EnqueueOpts{Priority: NormalPriority, RateLimited: true})
}

func (q *queueType) EnqueueWithOpts(obj interface{}, opts EnqueueOpts) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	utilruntime.Must(err)

	logger.V(log.LIBTRACE).Infof("%s: enqueueing key %q for %T object with priority %d",
		q.name, key, obj, opts.Priority)

	q.priorityQueue.AddWithOpts(priorityqueue.AddOpts{
		RateLimited: opts.RateLimited,
		Priority:    opts.Priority,
	}, key)
}

func (q *queueType) Run(stopCh <-chan struct{}, process ProcessFunc) {
	go wait.Until(func() {
		for q.processNextWorkItem(process) {
		}
	}, time.Second, stopCh)
}

func (q *queueType) processNextWorkItem(process ProcessFunc) bool {
	key, shutdown := q.Get()
	if shutdown {
		return false
	}

	defer q.Done(key)

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	utilruntime.Must(err)

	requeue, err := process(key, name, ns)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("%s: Failed to process object with key %q using function %#v: %w", q.name, key, process, err))
	}

	if requeue {
		q.AddRateLimited(key)
		logger.V(log.LIBDEBUG).Infof("%s: enqueued %q for retry - # of times re-queued: %d", q.name, key, q.NumRequeues(key))
	} else {
		q.Forget(key)
	}

	return true
}

func (q *queueType) NumRequeues(key string) int {
	return q.TypedRateLimitingInterface.NumRequeues(key)
}

func (q *queueType) ShutDownWithDrain() {
	done := make(chan struct{})

	// ShutDownWithDrain waits for all in-flight work to complete and thus could block indefinitely so put a deadline on it.
	go func() {
		q.TypedRateLimitingInterface.ShutDownWithDrain()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		logger.Warningf("%s: timed out draining the queue on shut down", q.name)
	}
}
