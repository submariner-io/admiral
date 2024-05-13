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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
)

func AddVerifyNamespaceReactor(f *testing.Fake, resources ...string) {
	f.Lock()
	defer f.Unlock()

	reactors := f.ReactionChain[0:]

	react := func(a testing.Action) (bool, runtime.Object, error) {
		action := a.(testing.CreateAction)

		namespace := action.GetNamespace()

		_, err := invokeReactors(testing.NewGetAction(corev1.SchemeGroupVersion.WithResource("namespaces"), "", namespace),
			reactors)
		if err != nil {
			return true, nil, err
		}

		return false, nil, nil
	}

	for _, res := range resources {
		f.PrependReactor("create", res, react)
	}
}
