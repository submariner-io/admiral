/*
© 2020 Red Hat, Inc.

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
	"k8s.io/klog"
)

type ProcessFunc func(key, name, namespace string) (bool, error)

type Interface interface {
	Enqueue(obj interface{})

	NumRequeues(key string) int

	Run(stopCh <-chan struct{}, process ProcessFunc)

	ShutDown()
}

type queueType struct {
	workqueue.RateLimitingInterface

	name string
}

func New(name string) Interface {
	return &queueType{
		RateLimitingInterface: workqueue.NewNamedRateLimitingQueue(workqueue.NewMaxOfRateLimiter(
			// exponential per-item rate limiter
			workqueue.NewItemExponentialFailureRateLimiter(5*time.Millisecond, 30*time.Second),
			// overall rate limiter (not per item)
			&workqueue.BucketRateLimiter{Limiter: rate.NewLimiter(rate.Limit(10), 100)},
		), name),
		name: name,
	}
}

func (q *queueType) Enqueue(obj interface{}) {
	var key string
	var err error
	if key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}

	klog.V(log.LIBTRACE).Infof("%s: enqueueing key %q for %T object", q.name, key, obj)
	q.AddRateLimited(key)
}

func (q *queueType) Run(stopCh <-chan struct{}, process ProcessFunc) {
	go wait.Until(func() {
		for q.processNextWorkItem(process) {
		}
	}, time.Second, stopCh)
}

func (q *queueType) processNextWorkItem(process ProcessFunc) bool {
	obj, shutdown := q.Get()
	if shutdown {
		return false
	}

	key := obj.(string)

	defer q.Done(key)

	requeue, err := func() (bool, error) {
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			return false, err
		}

		return process(key, name, ns)
	}()

	if err != nil {
		utilruntime.HandleError(fmt.Errorf("%s: Failed to process object with key %q: %v", q.name, key, err))
	}

	if requeue {
		q.AddRateLimited(key)
		klog.V(log.LIBDEBUG).Infof("%s: enqueued %q for retry - # of times re-queued: %d", q.name, key, q.NumRequeues(key))
	} else {
		q.Forget(key)
	}

	return true
}

func (q *queueType) NumRequeues(key string) int {
	return q.RateLimitingInterface.NumRequeues(key)
}
