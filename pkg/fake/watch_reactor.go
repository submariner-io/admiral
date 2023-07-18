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

package fake

import (
	"fmt"
	"sync"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/testing"
)

type WatchReactor struct {
	mutex            sync.Mutex
	watches          map[string]*watchDelegator
	filteringReactor filteringWatchReactor
}

func NewWatchReactor(f *testing.Fake) *WatchReactor {
	r := &WatchReactor{watches: map[string]*watchDelegator{}, filteringReactor: filteringWatchReactor{reactors: f.WatchReactionChain[0:]}}
	f.PrependWatchReactor("*", r.react)

	return r
}

func (r *WatchReactor) react(action testing.Action) (bool, watch.Interface, error) {
	handled, watcher, err := r.filteringReactor.react(action)
	if !handled || err != nil {
		return handled, nil, err
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	if r.watches[action.GetResource().Resource] != nil {
		return true, nil, fmt.Errorf("watch for %q was already started", action.GetResource().Resource)
	}

	delegator := &watchDelegator{Interface: watcher, stopped: make(chan struct{})}
	r.watches[action.GetResource().Resource] = delegator

	return true, delegator, nil
}

func (r *WatchReactor) getDelegator(forResource string) *watchDelegator {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	return r.watches[forResource]
}

func (r *WatchReactor) AwaitWatchStarted(forResource string) {
	Eventually(func() *watchDelegator {
		return r.getDelegator(forResource)
	}).ShouldNot(BeNil(), "Watch for %q was not started", forResource)
}

func (r *WatchReactor) AwaitNoWatchStarted(forResource string) {
	Consistently(func() *watchDelegator {
		return r.getDelegator(forResource)
	}).Should(BeNil(), "Watch for %q was started", forResource)
}

func (r *WatchReactor) AwaitWatchStopped(forResource string) {
	r.AwaitWatchStarted(forResource)
	Eventually(r.getDelegator(forResource).stopped).Should(BeClosed(), "Watch for %q was not stopped", forResource)
}

func (r *WatchReactor) AwaitNoWatchStopped(forResource string) {
	r.AwaitWatchStarted(forResource)
	Consistently(r.getDelegator(forResource).stopped).ShouldNot(BeClosed(), "Watch for %q was stopped", forResource)
}

type watchDelegator struct {
	watch.Interface
	stopped chan struct{}
}

func (w *watchDelegator) Stop() {
	w.Interface.Stop()
	close(w.stopped)
}
