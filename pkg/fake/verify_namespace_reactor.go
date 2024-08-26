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
	"sync"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/testing"
)

func AddVerifyNamespaceReactor(f *testing.Fake, resources ...string) {
	f.Lock()
	defer f.Unlock()

	reactors := f.ReactionChain[0:]

	seenGVRs := sync.Map{}

	react := func(a testing.Action) (bool, runtime.Object, error) {
		seenGVRs.Store(a.GetResource(), true)

		namespace := a.GetNamespace()
		if namespace != metav1.NamespaceNone {
			_, err := invokeReactors(testing.NewGetAction(corev1.SchemeGroupVersion.WithResource("namespaces"), "", namespace),
				reactors)
			if err != nil {
				return true, nil, err
			}
		}

		return false, nil, nil
	}

	for _, res := range resources {
		f.PrependReactor("*", res, react)
	}

	deleteCollectionReactor := newDeleteCollectionReactor(reactors)

	f.PrependReactor("delete", "namespaces", func(action testing.Action) (bool, runtime.Object, error) {
		name := action.(testing.DeleteAction).GetName()

		var err error

		seenGVRs.Range(func(key, _ any) bool {
			gvr := key.(schema.GroupVersionResource)

			_, _, err = deleteCollectionReactor.react(testing.NewDeleteCollectionActionWithOptions(gvr, name,
				metav1.DeleteOptions{}, metav1.ListOptions{}))
			err = errors.Wrapf(err, "VerifyNamespaceReactor: error deleting %q resources in namespace %q", gvr, name)

			return true
		})

		return err != nil, nil, err
	})
}
