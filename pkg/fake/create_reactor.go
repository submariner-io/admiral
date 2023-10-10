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
	"k8s.io/apimachinery/pkg/runtime"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/testing"
)

type createReactor struct {
	reactors []testing.Reactor
}

// AddCreateReactor adds a reactor to mimic real K8s create behavior to handle GenerateName, validation et al.
func AddCreateReactor(f *testing.Fake) {
	f.Lock()
	defer f.Unlock()

	r := &createReactor{reactors: f.ReactionChain[0:]}
	f.PrependReactor("create", "*", r.react)
}

func (r *createReactor) react(a testing.Action) (bool, runtime.Object, error) {
	action := a.(testing.CreateAction)

	target := resource.MustToMeta(action.GetObject())

	if target.GetName() == "" && target.GetGenerateName() != "" {
		target.SetName(fmt.Sprintf("%s%s", target.GetGenerateName(), utilrand.String(5)))
	}

	if target.GetName() == "" {
		return true, nil, apierrors.NewInvalid(
			action.GetObject().GetObjectKind().GroupVersionKind().GroupKind(), "",
			field.ErrorList{field.Required(field.NewPath("metadata.name"), "name is required")})
	}

	if target.GetResourceVersion() != "" {
		return true, nil, apierrors.NewBadRequest("resourceVersion can not be set for Create requests")
	}

	target.SetResourceVersion("1")

	if !target.GetDeletionTimestamp().IsZero() {
		target.SetDeletionTimestamp(nil)
	}

	target.SetUID(uuid.NewUUID())

	obj, err := invokeReactors(action, r.reactors)

	return true, obj, err
}
