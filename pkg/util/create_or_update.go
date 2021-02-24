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
	"time"

	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"
)

type OperationResult string

const (
	OperationResultNone    OperationResult = "unchanged"
	OperationResultCreated OperationResult = "created"
	OperationResultUpdated OperationResult = "updated"
)

type MutateFn func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error)

var backOff wait.Backoff = wait.Backoff{
	Steps:    10,
	Duration: 500 * time.Millisecond,
	Factor:   1.5,
	Cap:      20 * time.Second,
}

func CreateOrUpdate(client dynamic.ResourceInterface, obj *unstructured.Unstructured, mutate MutateFn) (OperationResult, error) {
	var result OperationResult = OperationResultNone

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := client.Get(obj.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			klog.V(log.LIBTRACE).Infof("Creating resource: %#v", obj)

			_, err := client.Create(obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				klog.V(log.LIBDEBUG).Infof("Resource %q already exists - retrying", obj.GetName())
				return apierrors.NewConflict(schema.GroupResource{Resource: obj.GetKind()}, obj.GetName(), err)
			}

			if err != nil {
				return errors.WithMessagef(err, "error creating %#v", obj)
			}

			result = OperationResultCreated
			return nil
		}

		if err != nil {
			return errors.WithMessagef(err, "error retrieving %q", obj.GetName())
		}

		copy := existing.DeepCopyObject()
		resourceVersion := existing.GetResourceVersion()

		toUpdate, err := mutate(existing)
		if err != nil {
			return err
		}

		toUpdate.SetResourceVersion(resourceVersion)

		if equality.Semantic.DeepEqual(toUpdate, copy) {
			return nil
		}

		klog.V(log.LIBTRACE).Infof("Updating resource: %#v", obj)

		result = OperationResultUpdated
		_, err = client.Update(toUpdate, metav1.UpdateOptions{})
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
func CreateAnew(client dynamic.ResourceInterface, obj runtime.Object, deleteOptions *metav1.DeleteOptions) error {
	toCreate := &unstructured.Unstructured{}
	err := scheme.Scheme.Convert(obj, toCreate, nil)
	if err != nil {
		return err
	}

	return wait.ExponentialBackoff(backOff, func() (bool, error) {
		_, err := client.Create(toCreate, metav1.CreateOptions{})
		if !apierrors.IsAlreadyExists(err) {
			return true, err
		}

		err = client.Delete(toCreate.GetName(), deleteOptions)
		if apierrors.IsNotFound(err) {
			err = nil
		}

		return false, errors.WithMessagef(err, "failed to delete pre-existing instance %q", toCreate.GetName())
	})
}

func SetBackoff(b wait.Backoff) wait.Backoff {
	prev := backOff
	backOff = b

	return prev
}

func Replace(with *unstructured.Unstructured) MutateFn {
	return func(existing *unstructured.Unstructured) (*unstructured.Unstructured, error) {
		return with, nil
	}
}
