package util

import (
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
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

func CreateOrUpdate(client dynamic.ResourceInterface, obj *unstructured.Unstructured, mutate MutateFn) (OperationResult, error) {
	var result OperationResult = OperationResultNone
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := client.Get(obj.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			klog.V(log.DEBUG).Infof("Creating resource: %#v", obj)

			_, err := client.Create(obj, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				klog.V(log.DEBUG).Infof("Resource %q already exists - retrying", obj.GetName())
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

		toUpdate, err := mutate(existing)
		if err != nil {
			return err
		}

		if equality.Semantic.DeepEqual(toUpdate, copy) {
			return nil
		}

		klog.V(log.DEBUG).Infof("Updating resource: %#v", obj)

		result = OperationResultUpdated
		_, err = client.Update(toUpdate, metav1.UpdateOptions{})
		return err
	})

	if err != nil {
		return OperationResultNone, err
	}

	return result, nil
}
