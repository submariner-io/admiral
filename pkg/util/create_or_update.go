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
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"github.com/submariner-io/admiral/pkg/resource"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
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

type MutateFn[T runtime.Object] func(existing T) (T, error)

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

type CreateOrUpdateOptions[T runtime.Object] struct {
	Client         resource.Interface[T]
	Obj            T
	MutateOnUpdate MutateFn[T]
	MutateOnCreate MutateFn[T]
}

func CreateOrUpdateWithOptions[T runtime.Object](ctx context.Context, options CreateOrUpdateOptions[T]) (OperationResult, T, error) {
	return maybeCreateOrUpdate(ctx, options, opCreate)
}

// CreateOrUpdate tries to obtain an existing resource and, if not found, creates 'obj' otherwise updates it. The existing resource
// is normally retrieved via 'obj's Name field but if it's empty and the GenerateName field is non-empty, it will try to retrieve it
// via the List method using 'obj's Labels. This assumes that the labels uniquely identify the resource. If more than one resource is
// found, an error is returned.
func CreateOrUpdate[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T, mutate MutateFn[T],
) (OperationResult, error) {
	r, _, err := CreateOrUpdateWithOptions(ctx, CreateOrUpdateOptions[T]{
		Client:         client,
		Obj:            obj,
		MutateOnUpdate: mutate,
	})

	return r, err
}

// Update tries to obtain an existing resource and, if found, updates it. If not found, no error is returned.
func Update[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T, mutate MutateFn[T]) error {
	_, _, err := maybeCreateOrUpdate(ctx, CreateOrUpdateOptions[T]{
		Client:         client,
		Obj:            obj,
		MutateOnUpdate: mutate,
	}, opUpdate)

	return err
}

// Update tries to obtain an existing resource and, if found, updates it. If not found, a NotFound error is returned.
func MustUpdate[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T, mutate MutateFn[T]) error {
	_, _, err := maybeCreateOrUpdate(ctx, CreateOrUpdateOptions[T]{
		Client:         client,
		Obj:            obj,
		MutateOnUpdate: mutate,
	}, opMustUpdate)

	return err
}

func maybeCreateOrUpdate[T runtime.Object](ctx context.Context, options CreateOrUpdateOptions[T], op opType) (OperationResult, T, error) {
	var returnObj T

	result := OperationResultNone

	objMeta := resource.MustToMeta(options.Obj)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := getResource(ctx, options.Client, options.Obj)
		if apierrors.IsNotFound(err) {
			if op != opCreate {
				logger.V(log.LIBTRACE).Infof("Resource %q does not exist - not updating", objMeta.GetName())

				if op == opMustUpdate {
					return err
				}

				return nil
			}

			result = OperationResultCreated

			returnObj, err = createResource(ctx, options.Client, options.Obj, options.MutateOnCreate)

			return err
		}

		if err != nil {
			return errors.Wrapf(err, "error retrieving %q", objMeta.GetName())
		}

		if options.MutateOnUpdate == nil {
			return nil
		}

		origObj := resource.MustToUnstructuredUsingDefaultConverter(existing)

		toUpdate, err := options.MutateOnUpdate(existing)
		if err != nil {
			return err
		}

		objMeta := resource.MustToMeta(toUpdate)
		objMeta.SetResourceVersion(origObj.GetResourceVersion())
		objMeta.SetName(origObj.GetName())
		objMeta.SetGenerateName("")

		newObj := resource.MustToUnstructuredUsingDefaultConverter(toUpdate)

		origStatus := GetNestedField(origObj, StatusField)
		newStatus, ok := GetNestedField(newObj, StatusField).(map[string]interface{})

		if !ok || DeeplyEmpty(newStatus) {
			unstructured.RemoveNestedField(origObj.Object, StatusField)
			unstructured.RemoveNestedField(newObj.Object, StatusField)
		} else if !equality.Semantic.DeepEqual(origStatus, newStatus) {
			logger.V(log.LIBTRACE).Infof("Updating resource status: %s", resource.ToJSON(newStatus))

			result = OperationResultUpdated

			// UpdateStatus for generic clients (eg dynamic client) will return NotFound error if the resource CRD
			// doesn't have the status subresource so we'll ignore it.
			updated, err := options.Client.UpdateStatus(ctx, toUpdate, metav1.UpdateOptions{})
			if err == nil {
				unstructured.RemoveNestedField(origObj.Object, StatusField)
				unstructured.RemoveNestedField(newObj.Object, StatusField)
				resource.MustToMeta(toUpdate).SetResourceVersion(resource.MustToMeta(updated).GetResourceVersion())
			} else if !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "error updating status %s", resource.ToJSON(toUpdate))
			}
		}

		if equality.Semantic.DeepEqual(origObj, newObj) {
			return nil
		}

		logger.V(log.LIBTRACE).Infof("Updating resource: %s", resource.ToJSON(options.Obj))

		result = OperationResultUpdated
		returnObj, err = options.Client.Update(ctx, toUpdate, metav1.UpdateOptions{})

		return errors.Wrapf(err, "error updating %s", resource.ToJSON(toUpdate))
	})
	if err != nil {
		return OperationResultNone, *new(T), errors.Wrap(err, "error creating or updating resource")
	}

	return result, returnObj, nil
}

//nolint:wrapcheck // No need to wrap errors
func getResource[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T) (T, error) {
	objMeta := resource.MustToMeta(obj)

	if objMeta.GetName() != "" || objMeta.GetGenerateName() == "" {
		obj, err := client.Get(ctx, objMeta.GetName(), metav1.GetOptions{})
		return obj, err
	}

	list, err := client.List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(objMeta.GetLabels()).String(),
	})
	if err != nil {
		return *new(T), err
	}

	count := len(list)
	if count == 0 {
		return *new(T), apierrors.NewNotFound(schema.GroupResource{}, "")
	}

	if count != 1 {
		return *new(T), fmt.Errorf("found %d resources with labels %#v, expected 1", count, objMeta.GetLabels())
	}

	return list[0], nil
}

func createResource[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T, mutate MutateFn[T]) (T, error) {
	if mutate != nil {
		mutated, err := mutate(obj)
		if err != nil {
			return *new(T), err
		}

		obj = mutated
	}

	logger.V(log.LIBTRACE).Infof("Creating resource: %#v", obj)

	objMeta := resource.MustToMeta(obj)

	created, err := client.Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		logger.V(log.LIBDEBUG).Infof("Resource %q already exists - retrying", objMeta.GetName())
		return *new(T), apierrors.NewConflict(schema.GroupResource{}, objMeta.GetName(), err)
	}

	if err != nil {
		return *new(T), errors.Wrapf(err, "error creating %#v", obj)
	}

	status, ok := GetNestedField(resource.MustToUnstructuredUsingDefaultConverter(obj), StatusField).(map[string]interface{})
	if ok && !DeeplyEmpty(status) {
		// If the resource CRD has the status subresource the Create won't set the status field so we need to
		// do a separate UpdateStatus call.
		objMeta.SetResourceVersion(resource.MustToMeta(created).GetResourceVersion())
		objMeta.SetUID(resource.MustToMeta(created).GetUID())
		objMeta.SetCreationTimestamp(resource.MustToMeta(created).GetCreationTimestamp())

		created, err = client.UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return *new(T), errors.Wrapf(err, "error updating status for %#v", obj)
		}
	}

	return created, nil
}

// CreateAnew creates a resource, first deleting an existing instance if one exists.
// If the delete options specify that deletion should be propagated in the foreground,
// this will wait for the deletion to be complete before creating the new object:
// with foreground propagation, Get will continue to return the object being deleted
// and Create will fail with “already exists” until deletion is complete.
func CreateAnew[T runtime.Object](ctx context.Context, client resource.Interface[T], obj T,
	createOptions metav1.CreateOptions, //nolint:gocritic // hugeParam - we're matching K8s API
	deleteOptions metav1.DeleteOptions, //nolint:gocritic // hugeParam - we're matching K8s API
) (T, error) {
	name := resource.MustToMeta(obj).GetName()

	var retObj T

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
	existingU := resource.MustToUnstructuredUsingDefaultConverter(existingObj)
	newU := resource.MustToUnstructuredUsingDefaultConverter(newObj)

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

func Replace[T runtime.Object](with T) MutateFn[T] {
	return func(_ T) (T, error) {
		return with, nil
	}
}
