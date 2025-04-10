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
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"golang.org/x/time/rate"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
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
	Run(process ProcessFunc)
	ShutDown()
	ShutDownWithDrain(ctx context.Context) error
}

type EnqueueOpts struct {
	RateLimited bool
	Priority    int
}

type queueType struct {
	workqueue.TypedRateLimitingInterface[string]
	priorityQueue *PriorityQueue
	name          string
}

var logger = log.Logger{Logger: logf.Log.WithName("WorkQueue")}

func New(name string) Interface {
	return NewWithConfig(name, DefaultConfig())
}

func NewWithConfig(name string, config Config) Interface {
	priorityQueue := NewPriorityQueue()

	var rateLimiters []workqueue.TypedRateLimiter[string]

	if config.ItemRateLimiterBaseDelay > 0 && config.ItemRateLimiterMaxDelay > 0 {
		// exponential per-item rate limiter
		rateLimiters = append(rateLimiters, workqueue.NewTypedItemExponentialFailureRateLimiter[string](
			config.ItemRateLimiterBaseDelay, config.ItemRateLimiterMaxDelay))
	}

	if config.BucketRateLimiterItemsPerSec > 0 && config.BucketRateLimiterMaxBurst > 0 {
		// overall rate limiter (not per item)
		rateLimiters = append(rateLimiters, &workqueue.TypedBucketRateLimiter[string]{
			Limiter: rate.NewLimiter(rate.Limit(config.BucketRateLimiterItemsPerSec), config.BucketRateLimiterMaxBurst),
		})
	}

	return &queueType{
		priorityQueue: priorityQueue,
		TypedRateLimitingInterface: workqueue.NewTypedRateLimitingQueueWithConfig(
			// caps the maximum wait
			workqueue.NewTypedWithMaxWaitRateLimiter(
				workqueue.NewTypedMaxOfRateLimiter(rateLimiters...), config.OverallRateLimiterMaxDelay),
			workqueue.TypedRateLimitingQueueConfig[string]{
				Name: name,
				DelayingQueue: workqueue.NewTypedDelayingQueueWithConfig(workqueue.TypedDelayingQueueConfig[string]{
					Name: name,
					Queue: workqueue.NewTypedWithConfig(workqueue.TypedQueueConfig[string]{
						Name:  name,
						Queue: &priorityWorkQueue[string]{priorityQueue: priorityQueue},
					}),
				}),
			}),
		name: name,
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

	q.priorityQueue.SetPriority(key, opts.Priority)

	if opts.RateLimited {
		q.AddRateLimited(key)
	} else {
		q.Add(key)
	}
}

func (q *queueType) Run(process ProcessFunc) {
	go func() {
		for q.processNextWorkItem(process) {
		}
	}()
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

func (q *queueType) ShutDownWithDrain(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		for {
			q.TypedRateLimitingInterface.ShutDownWithDrain()

			// The queue should be empty after ShutDownWithDrain returns, but sometimes it isn't so ensure it is.
			if q.Len() == 0 {
				break
			}
		}

		done <- struct{}{}
	}()

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, time.Second*5)
		defer cancel()
	}

	select {
	case <-done:
	case <-ctx.Done():
		// Calling ShutDown causes ShutDownWithDrain to return.
		q.TypedRateLimitingInterface.ShutDown()
		return errors.Wrapf(ctx.Err(), "%s: did not complete draining the queue on shut down", q.name)
	}

	return nil
}
