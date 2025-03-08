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

package configmap

import (
	"context"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Global defines the name of the ConfigMap shared by all Submariner components.
const Global = "submariner-global"

// Get retrieves the ConfigMap for the given name or, if not found, returns an empty ConfigMap.
func Get(ctx context.Context, client resource.Interface[*corev1.ConfigMap], name string) (*corev1.ConfigMap, error) {
	cm, err := client.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return cm, nil
	}

	if resource.IsNotFoundErr(err) {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string]string{},
		}, nil
	}

	return nil, errors.Wrapf(err, "error retrieving ConfigMap %q", name)
}
