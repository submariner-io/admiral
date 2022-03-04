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

package finalizer

import (
	"context"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Add(ctx context.Context, client resource.Interface, obj runtime.Object, finalizerName string) (bool, error) {
	objMeta := resource.ToMeta(obj)
	if !objMeta.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	if IsPresent(objMeta, finalizerName) {
		return false, nil
	}

	err := util.Update(ctx, client, obj, func(existing runtime.Object) (runtime.Object, error) {
		objMeta := resource.ToMeta(existing)
		objMeta.SetFinalizers(append(objMeta.GetFinalizers(), finalizerName))

		return existing, nil
	})

	return err == nil, errors.Wrapf(err, "error adding finalizer %q to %q", finalizerName, objMeta.GetName())
}

func Remove(ctx context.Context, client resource.Interface, obj runtime.Object, finalizerName string) error {
	objMeta := resource.ToMeta(obj)
	if !IsPresent(objMeta, finalizerName) {
		return nil
	}

	err := util.Update(ctx, client, obj, func(existing runtime.Object) (runtime.Object, error) {
		objMeta := resource.ToMeta(existing)

		newFinalizers := []string{}
		for _, f := range objMeta.GetFinalizers() {
			if f == finalizerName {
				continue
			}

			newFinalizers = append(newFinalizers, f)
		}

		objMeta.SetFinalizers(newFinalizers)

		return existing, nil
	})

	return errors.Wrapf(err, "error removing finalizer %q from %q", finalizerName, objMeta.GetName())
}

func IsPresent(objMeta metav1.Object, finalizerName string) bool {
	for _, f := range objMeta.GetFinalizers() {
		if f == finalizerName {
			return true
		}
	}

	return false
}
