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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

type OperationResult string

const (
	OperationResultNone    OperationResult = "unchanged"
	OperationResultCreated OperationResult = "created"
	OperationResultUpdated OperationResult = "updated"
)

type MutateFn func(existing runtime.Object) (runtime.Object, error)

type opType int

const (
	opCreate     opType = 1
	opUpdate     opType = 2
	opMustUpdate opType = 3
)

var backOff wait.Backoff = wait.Backoff{
	Steps:    20,
	Duration: time.Second,
	Factor:   1.3,
	Cap:      40 * time.Second,
}

var logger = log.Logger{Logger: logf.Log}

func CreateOrUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) (OperationResult, error) {
	return maybeCreateOrUpdate(ctx, client, obj, mutate, opCreate)
}

func Update(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) error {
	_, err := maybeCreateOrUpdate(ctx, client, obj, mutate, opUpdate)
	return err
}

func MustUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) error {
	_, err := maybeCreateOrUpdate(ctx, client, obj, mutate, opMustUpdate)
	return err
}

func maybeCreateOrUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn,
	op opType,
) (OperationResult, error) {
	result := OperationResultNone

	objMeta := resource.MustToMeta(obj)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := client.Get(ctx, objMeta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if op != opCreate {
				logger.V(log.LIBTRACE).Infof("Resource %q does not exist - not updating", objMeta.GetName())

				if op == opMustUpdate {
					return err //nolint:wrapcheck // No need to wrap
				}

				return nil
			}

			logger.V(log.LIBTRACE).Infof("Creating resource: %#v", obj)

			result = OperationResultCreated
			return createResource(ctx, client, obj)
		}

		if err != nil {
			return errors.Wrapf(err, "error retrieving %q", objMeta.GetName())
		}

		origObj := resource.MustToUnstructured(existing)

		toUpdate, err := mutate(existing)
		if err != nil {
			return err
		}

		resource.MustToMeta(toUpdate).SetResourceVersion(origObj.GetResourceVersion())

		newObj := resource.MustToUnstructured(toUpdate)

		origStatus := GetNestedField(origObj, StatusField)
		newStatus, ok := GetNestedField(newObj, StatusField).(map[string]interface{})

		if !ok || len(newStatus) == 0 {
			unstructured.RemoveNestedField(origObj.Object, StatusField)
			unstructured.RemoveNestedField(newObj.Object, StatusField)
		} else if !equality.Semantic.DeepEqual(origStatus, newStatus) {
			logger.V(log.LIBTRACE).Infof("Updating resource status: %s", resource.ToJSON(newStatus))

			result = OperationResultUpdated

			unstructured.RemoveNestedField(origObj.Object, StatusField)
			unstructured.RemoveNestedField(newObj.Object, StatusField)

			// UpdateStatus for generic clients (eg dynamic client) will return NotFound error if the resource CRD
			// doesn't have the status subresource so we'll ignore it.
			updated, err := client.UpdateStatus(ctx, toUpdate, metav1.UpdateOptions{})
			if err == nil {
				resource.MustToMeta(toUpdate).SetResourceVersion(resource.MustToMeta(updated).GetResourceVersion())
			} else if !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "error updating status %s", resource.ToJSON(toUpdate))
			}
		}

		if equality.Semantic.DeepEqual(origObj, newObj) {
			return nil
		}

		logger.V(log.LIBTRACE).Infof("Updating resource: %s", resource.ToJSON(obj))

		result = OperationResultUpdated
		_, err = client.Update(ctx, toUpdate, metav1.UpdateOptions{})

		return errors.Wrapf(err, "error updating %s", resource.ToJSON(toUpdate))
	})
	if err != nil {
		return OperationResultNone, errors.Wrap(err, "error creating or updating resource")
	}

	return result, nil
}

func createResource(ctx context.Context, client resource.Interface, obj runtime.Object) error {
	objMeta := resource.MustToMeta(obj)

	created, err := client.Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		logger.V(log.LIBDEBUG).Infof("Resource %q already exists - retrying", objMeta.GetName())
		return apierrors.NewConflict(schema.GroupResource{}, objMeta.GetName(), err)
	}

	if err != nil {
		return errors.Wrapf(err, "error creating %#v", obj)
	}

	status, ok := GetNestedField(resource.MustToUnstructured(obj), StatusField).(map[string]interface{})
	if ok && len(status) > 0 {
		// If the resource CRD has the status subresource the Create won't set the status field so we need to
		// do a separate UpdateStatus call.
		objMeta.SetResourceVersion(resource.MustToMeta(created).GetResourceVersion())
		objMeta.SetUID(resource.MustToMeta(created).GetUID())

		_, err := client.UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "error updating status for %#v", obj)
		}
	}

	return nil
}

// CreateAnew creates a resource, first deleting an existing instance if one exists.
// If the delete options specify that deletion should be propagated in the foreground,
// this will wait for the deletion to be complete before creating the new object:
// with foreground propagation, Get will continue to return the object being deleted
// and Create will fail with “already exists” until deletion is complete.
func CreateAnew(ctx context.Context, client resource.Interface, obj runtime.Object,
	createOptions metav1.CreateOptions, //nolint:gocritic // hugeParam - we're matching K8s API
	deleteOptions metav1.DeleteOptions, //nolint:gocritic // hugeParam - we're matching K8s API
) (runtime.Object, error) {
	name := resource.MustToMeta(obj).GetName()

	var retObj runtime.Object

	err := wait.ExponentialBackoff(backOff, func() (bool, error) {
		var err error

		retObj, err = client.Create(ctx, obj, createOptions)
		if !apierrors.IsAlreadyExists(err) {
			return true, errors.Wrapf(err, "error creating %#v", obj)
		}

		retObj, err = client.Get(ctx, resource.MustToMeta(obj).GetName(), metav1.GetOptions{})
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
	existingU := resource.MustToUnstructured(existingObj)
	newU := resource.MustToUnstructured(newObj)

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
