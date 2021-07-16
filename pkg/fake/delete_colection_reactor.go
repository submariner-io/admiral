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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/testing"
)

type DeleteCollectionReactor struct {
	gvk      schema.GroupVersionKind
	reactors []testing.Reactor
}

func AddDeleteCollectionReactor(f *testing.Fake, gvk schema.GroupVersionKind) {
	r := &DeleteCollectionReactor{gvk: gvk, reactors: f.ReactionChain[0:]}
	chain := []testing.Reactor{&testing.SimpleReactor{Verb: "delete-collection", Resource: "*", Reaction: r.react}}
	f.ReactionChain = append(chain, f.ReactionChain...)
}

func (r *DeleteCollectionReactor) react(action testing.Action) (bool, runtime.Object, error) {
	switch dc := action.(type) {
	case testing.DeleteCollectionActionImpl:
		defer GinkgoRecover()

		obj, err := r.invoke(testing.NewListAction(action.GetResource(), r.gvk, action.GetNamespace(), metav1.ListOptions{
			LabelSelector: dc.ListRestrictions.Labels.String(),
			FieldSelector: dc.ListRestrictions.Fields.String(),
		}))

		if err != nil {
			return true, nil, err
		}

		raw := test.ToUnstructured(obj)
		items, found, err := unstructured.NestedSlice(raw.Object, "items")
		Expect(err).To(Succeed())
		Expect(found).To(BeTrue())

		for _, m := range items {
			item := unstructured.Unstructured{Object: m.(map[string]interface{})}
			if dc.ListRestrictions.Labels.Matches(labels.Set(item.GetLabels())) {
				_, err = r.invoke(testing.NewDeleteAction(action.GetResource(), action.GetNamespace(), item.GetName()))
				if err != nil {
					return true, nil, err
				}
			}
		}

		return true, nil, nil
	default:
		return false, nil, fmt.Errorf("invalid action: %#v", action)
	}
}

func (r *DeleteCollectionReactor) invoke(action testing.Action) (runtime.Object, error) {
	for _, reactor := range r.reactors {
		if !reactor.Handles(action) {
			continue
		}

		handled, ret, err := reactor.React(action)
		if !handled {
			continue
		}

		return ret, err
	}

	return nil, errors.New("action not handled")
}
