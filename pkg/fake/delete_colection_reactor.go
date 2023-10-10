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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/testing"
)

type DeleteCollectionReactor struct {
	gvrToGVK map[schema.GroupVersionResource]schema.GroupVersionKind
	reactors []testing.Reactor
}

func AddDeleteCollectionReactor(f *testing.Fake) {
	f.Lock()
	r := &DeleteCollectionReactor{gvrToGVK: map[schema.GroupVersionResource]schema.GroupVersionKind{}, reactors: f.ReactionChain[0:]}
	f.PrependReactor("delete-collection", "*", r.react)
	f.Unlock()

	for gvk := range scheme.Scheme.AllKnownTypes() {
		if !strings.HasSuffix(gvk.Kind, "List") {
			continue
		}

		nonListGVK := gvk.GroupVersion().WithKind(gvk.Kind[:len(gvk.Kind)-4])
		plural, _ := meta.UnsafeGuessKindToResource(nonListGVK)
		r.gvrToGVK[plural] = nonListGVK
	}
}

func (r *DeleteCollectionReactor) react(action testing.Action) (bool, runtime.Object, error) {
	switch dc := action.(type) {
	case testing.DeleteCollectionActionImpl:
		defer GinkgoRecover()

		obj, err := invokeReactors(testing.NewListAction(action.GetResource(), r.gvrToGVK[action.GetResource()],
			action.GetNamespace(), metav1.ListOptions{
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
