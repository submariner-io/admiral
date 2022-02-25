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

package util

import (
	"context"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

type OperationResult string

const (
	OperationResultNone    OperationResult = "unchanged"
	OperationResultCreated OperationResult = "created"
	OperationResultUpdated OperationResult = "updated"
)

type MutateFn func(existing runtime.Object) (runtime.Object, error)

var backOff wait.Backoff = wait.Backoff{
	Steps:    20,
	Duration: time.Second,
	Factor:   1.3,
	Cap:      40 * time.Second,
}

func CreateOrUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) (OperationResult, error) {
	return maybeCreateOrUpdate(ctx, client, obj, mutate, true)
}

func Update(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) error {
	_, err := maybeCreateOrUpdate(ctx, client, obj, mutate, false)
	return err
}

func maybeCreateOrUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn,
	doCreate bool) (OperationResult, error) {
	result := OperationResultNone

	objMeta := resource.ToMeta(obj)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := client.Get(ctx, objMeta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if !doCreate {
				klog.V(log.LIBTRACE).Infof("Resource %q does not exist - not updating", objMeta.GetName())
				return nil
			}

			klog.V(log.LIBTRACE).Infof("Creating resource: %#v", obj)

			_, err := client.Create(ctx, obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				klog.V(log.LIBDEBUG).Infof("Resource %q already exists - retrying", objMeta.GetName())
				return apierrors.NewConflict(schema.GroupResource{}, objMeta.GetName(), err)
			}

			if err != nil {
				return errors.Wrapf(err, "error creating %#v", obj)
			}

			result = OperationResultCreated
			return nil
		}

		if err != nil {
			return errors.Wrapf(err, "error retrieving %q", objMeta.GetName())
		}

		orig := existing.DeepCopyObject()
		resourceVersion := resource.ToMeta(existing).GetResourceVersion()

		toUpdate, err := mutate(existing)
		if err != nil {
			return err
		}

		resource.ToMeta(toUpdate).SetResourceVersion(resourceVersion)

		if equality.Semantic.DeepEqual(toUpdate, orig) {
			return nil
		}

		klog.V(log.LIBTRACE).Infof("Updating resource: %#v", obj)

		result = OperationResultUpdated
		_, err = client.Update(ctx, toUpdate, metav1.UpdateOptions{})

		return errors.Wrapf(err, "error updating %#v", toUpdate)
	})
	if err != nil {
		return OperationResultNone, errors.Wrap(err, "error creating or updating resource")
	}

	return result, nil
}

// CreateAnew creates a resource, first deleting an existing instance if one exists.
// If the delete options specify that deletion should be propagated in the foreground,
// this will wait for the deletion to be complete before creating the new object:
// with foreground propagation, Get will continue to return the object being deleted
// and Create will fail with “already exists” until deletion is complete.
func CreateAnew(ctx context.Context, client resource.Interface, obj runtime.Object,
	createOptions metav1.CreateOptions,
	deleteOptions metav1.DeleteOptions) (runtime.Object, error, // nolint:gocritic // Match K8s API
) {
	name := resource.ToMeta(obj).GetName()

	var retObj runtime.Object

	err := wait.ExponentialBackoff(backOff, func() (bool, error) {
		var err error

		retObj, err = client.Create(ctx, obj, createOptions)
		if !apierrors.IsAlreadyExists(err) {
			return true, errors.Wrapf(err, "error creating %#v", obj)
		}

		retObj, err = client.Get(ctx, resource.ToMeta(obj).GetName(), metav1.GetOptions{})
		if !apierrors.IsNotFound(err) {
			if err != nil {
				return false, errors.Wrapf(err, "failed to retrieve pre-existing instance %q", name)
			}

			if mutableFieldsEqual(retObj, obj) {
				return true, nil
			}
		}

		err = client.Delete(ctx, name, deleteOptions)
		if apierrors.IsNotFound(err) {
			err = nil
		}

		return false, errors.Wrapf(err, "failed to delete pre-existing instance %q", name)
	})

	return retObj, errors.Wrap(err, "error creating resource anew")
}

func mutableFieldsEqual(existingObj, newObj runtime.Object) bool {
	existingU, err := resource.ToUnstructured(existingObj)
	if err != nil {
		panic(err)
	}

	newU, err := resource.ToUnstructured(newObj)
	if err != nil {
		panic(err)
	}

	newU = CopyImmutableMetadata(existingU, newU)

	// Also ignore the Status fields.
	unstructured.RemoveNestedField(existingU.Object, StatusField)
	unstructured.RemoveNestedField(newU.Object, StatusField)

	return equality.Semantic.DeepEqual(existingU, newU)
}

func SetBackoff(b wait.Backoff) wait.Backoff {
	prev := backOff
	backOff = b

	return prev
}

func Replace(with runtime.Object) MutateFn {
	return func(existing runtime.Object) (runtime.Object, error) {
		return with, nil
	}
}
