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
	"strconv"

	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/testing"
)

type updateReactor struct {
	reactors []testing.Reactor
}

// AddUpdateReactor adds a reactor to mimic real K8s update behavior to handle ResourceVersion et al.
func AddUpdateReactor(f *testing.Fake) {
	r := &updateReactor{reactors: f.ReactionChain[0:]}
	f.PrependReactor("update", "*", r.react)
}

func (r *updateReactor) react(a testing.Action) (bool, runtime.Object, error) {
	action := a.(testing.UpdateAction)

	target := resource.MustToMeta(action.GetObject())

	if target.GetName() == "" {
		return true, nil, apierrors.NewInvalid(
			action.GetObject().GetObjectKind().GroupVersionKind().GroupKind(), "",
			field.ErrorList{field.Required(field.NewPath("metadata.name"), "name is required")})
	}

	o, err := invokeReactors(testing.NewGetAction(action.GetResource(), action.GetNamespace(),
		target.GetName()), r.reactors)
	if err != nil {
		return true, nil, err
	}

	existing := resource.MustToMeta(o)
	if existing.GetResourceVersion() != target.GetResourceVersion() {
		return true, nil, apierrors.NewConflict(schema.GroupResource{}, target.GetName(),
			fmt.Errorf("resource version %q does not match expected %q", target.GetResourceVersion(),
				existing.GetResourceVersion()))
	}

	v, err := strconv.Atoi(existing.GetResourceVersion())
	utilruntime.Must(err)
	target.SetResourceVersion(strconv.Itoa(v + 1))

	obj, err := invokeReactors(action, r.reactors)

	return true, obj, err
}
