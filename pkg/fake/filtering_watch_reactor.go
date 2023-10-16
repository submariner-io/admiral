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
	"errors"

	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/testing"
)

type filteringWatchReactor struct {
	reactors []testing.WatchReactor
}

func AddFilteringWatchReactor(f *testing.Fake) {
	r := &filteringWatchReactor{reactors: f.WatchReactionChain[0:]}
	f.PrependWatchReactor("*", r.react)
}

func filterEvent(event watch.Event, restrictions *testing.WatchRestrictions) (watch.Event, bool) {
	if restrictions.Labels != nil && !restrictions.Labels.Matches(labels.Set(resource.MustToMeta(event.Object).GetLabels())) {
		return event, false
	}

	return event, true
}

func (r *filteringWatchReactor) react(action testing.Action) (bool, watch.Interface, error) {
	switch w := action.(type) {
	case testing.WatchActionImpl:
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

			return true, watcher, err
		}

		return true, nil, errors.New("action not handled")
	default:
	}

	return false, nil, nil
}
