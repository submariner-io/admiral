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
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/testing"
)

type WatchReactor struct {
	sync.Mutex
	watches  map[string]*watchDelegator
	reactors []testing.WatchReactor
}

func NewWatchReactor(f *testing.Fake) *WatchReactor {
	r := &WatchReactor{watches: map[string]*watchDelegator{}, reactors: f.WatchReactionChain[0:]}
	chain := []testing.WatchReactor{&testing.SimpleWatchReactor{Resource: "*", Reaction: r.react}}
	f.WatchReactionChain = append(chain, f.WatchReactionChain...)

	return r
}

func filterEvent(event watch.Event, restrictions *testing.WatchRestrictions) (watch.Event, bool) {
	if restrictions.Labels != nil && !restrictions.Labels.Matches(labels.Set(resource.ToMeta(event.Object).GetLabels())) {
		return event, false
	}

	return event, true
}

func (r *WatchReactor) react(action testing.Action) (bool, watch.Interface, error) {
	switch w := action.(type) {
	case testing.WatchActionImpl:
		r.Lock()
		defer r.Unlock()

		if r.watches[w.Resource.Resource] != nil {
			return true, nil, fmt.Errorf("watch for %q was already started", w.Resource.Resource)
		}

		for _, reactor := range r.reactors {
			if !reactor.Handles(action) {
				continue
			}

			handled, watcher, err := reactor.React(action)
			if !handled {
				continue
			}

			watcher = watch.Filter(watcher, func(in watch.Event) (out watch.Event, keep bool) {
				return filterEvent(in, &w.WatchRestrictions)
			})

			delegator := &watchDelegator{Interface: watcher, stopped: make(chan struct{})}
			r.watches[w.Resource.Resource] = delegator

			return true, delegator, err
		}

		return true, nil, errors.New("action not handled")
	default:
	}

	return false, nil, nil
}

func (r *WatchReactor) getDelegator(forResource string) *watchDelegator {
	r.Lock()
	defer r.Unlock()

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
