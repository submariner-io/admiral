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

	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

type deleteReactor struct {
	reactors []testing.Reactor
}

// AddDeleteReactor adds a reactor to mimic real K8s delete behavior to handle ResourceVersion, DeletionTimestamp et al.
func AddDeleteReactor(f *testing.Fake) {
	f.Lock()
	defer f.Unlock()

	r := &deleteReactor{reactors: f.ReactionChain[0:]}
	f.PrependReactor("delete", "*", r.react)
}

func (r *deleteReactor) react(a testing.Action) (bool, runtime.Object, error) {
	action := a.(testing.DeleteAction)

	existingObj, err := invokeReactors(testing.NewGetAction(action.GetResource(), action.GetNamespace(),
		action.GetName()), r.reactors)
	if err != nil {
		return true, nil, err
	}

	existing := resource.MustToMeta(existingObj)

	if action.GetDeleteOptions().Preconditions != nil && action.GetDeleteOptions().Preconditions.ResourceVersion != nil {
		if existing.GetResourceVersion() != *action.GetDeleteOptions().Preconditions.ResourceVersion {
			return true, nil, apierrors.NewConflict(action.GetResource().GroupResource(), action.GetName(),
				fmt.Errorf("the ResourceVersion in the precondition (%s) does not match the ResourceVersion in record (%s). "+
					"The object might have been modified",
					*action.GetDeleteOptions().Preconditions.ResourceVersion, existing.GetResourceVersion()))
		}
	}

	if len(existing.GetFinalizers()) > 0 {
		now := metav1.Now()
		existing.SetDeletionTimestamp(&now)

		obj, err := invokeReactors(testing.NewUpdateAction(action.GetResource(), action.GetNamespace(),
			existingObj), r.reactors)

		return true, obj, err
	}

	obj, err := invokeReactors(action, r.reactors)

	return true, obj, err
}
