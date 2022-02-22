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
	"k8s.io/apimachinery/pkg/fields"
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

		obj, err := invokeReactors(testing.NewListAction(action.GetResource(), r.gvk, action.GetNamespace(), metav1.ListOptions{
			LabelSelector: dc.ListRestrictions.Labels.String(),
			FieldSelector: dc.ListRestrictions.Fields.String(),
		}), r.reactors)
		if err != nil {
			return true, nil, err
		}

		raw := test.ToUnstructured(obj)
		items, found, err := unstructured.NestedSlice(raw.Object, "items")
		Expect(err).To(Succeed())
		Expect(found).To(BeTrue())

		for _, m := range items {
			item := unstructured.Unstructured{Object: m.(map[string]interface{})}

			fieldSet := fields.Set{"metadata.namespace": item.GetNamespace(), "metadata.name": item.GetName()}
			if dc.ListRestrictions.Labels.Matches(labels.Set(item.GetLabels())) &&
				dc.ListRestrictions.Fields.Matches(fieldSet) {
				_, err = invokeReactors(testing.NewDeleteAction(action.GetResource(), item.GetNamespace(), item.GetName()), r.reactors)
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

func invokeReactors(action testing.Action, reactors []testing.Reactor) (runtime.Object, error) {
	for _, reactor := range reactors {
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
