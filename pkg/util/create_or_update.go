/*
© 2020 Red Hat, Inc.

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
	Steps:    10,
	Duration: 500 * time.Millisecond,
	Factor:   1.5,
	Cap:      20 * time.Second,
}

func CreateOrUpdate(ctx context.Context, client resource.Interface, obj runtime.Object, mutate MutateFn) (OperationResult, error) {
	var result OperationResult = OperationResultNone

	objMeta := resource.ToMeta(obj)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := client.Get(ctx, objMeta.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			klog.V(log.LIBTRACE).Infof("Creating resource: %#v", obj)

			_, err := client.Create(ctx, obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				klog.V(log.LIBDEBUG).Infof("Resource %q already exists - retrying", objMeta.GetName())
				return apierrors.NewConflict(schema.GroupResource{}, objMeta.GetName(), err)
			}

			if err != nil {
				return errors.WithMessagef(err, "error creating %#v", obj)
			}

			result = OperationResultCreated
			return nil
		}

		if err != nil {
			return errors.WithMessagef(err, "error retrieving %q", objMeta.GetName())
		}

		copy := existing.DeepCopyObject()
		resourceVersion := resource.ToMeta(existing).GetResourceVersion()

		toUpdate, err := mutate(existing)
		if err != nil {
			return err
		}

		resource.ToMeta(toUpdate).SetResourceVersion(resourceVersion)

		if equality.Semantic.DeepEqual(toUpdate, copy) {
			return nil
		}

		klog.V(log.LIBTRACE).Infof("Updating resource: %#v", obj)

		result = OperationResultUpdated
		_, err = client.Update(ctx, toUpdate, metav1.UpdateOptions{})

		return err
	})

	if err != nil {
		return OperationResultNone, err
	}

	return result, nil
}

// CreateAnew creates a resource, first deleting an existing instance if one exists.
// If the delete options specify that deletion should be propagated in the foreground,
// this will wait for the deletion to be complete before creating the new object:
// with foreground propagation, Get will continue to return the object being deleted
// and Create will fail with “already exists” until deletion is complete.
func CreateAnew(ctx context.Context, client resource.Interface, obj runtime.Object,
	createOptions metav1.CreateOptions, deleteOptions metav1.DeleteOptions) error {
	name := resource.ToMeta(obj).GetName()

	return wait.ExponentialBackoff(backOff, func() (bool, error) {
		_, err := client.Create(ctx, obj, createOptions)
		if !apierrors.IsAlreadyExists(err) {
			return true, err
		}

		err = client.Delete(ctx, name, deleteOptions)
		if apierrors.IsNotFound(err) {
			err = nil
		}

		return false, errors.WithMessagef(err, "failed to delete pre-existing instance %q", name)
	})
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
