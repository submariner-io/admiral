package framework

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
)

// Arbitrary numbers. Could be moved to be configurable if needed.
var (
	interval = 50 * time.Millisecond
	timeout  = 60 * time.Second
)

// WaitForObject fetches the resource using `objGetter` using `namespace`
// and `name` as the keys. If found it compares it to the `desired` using the
// `equivalent` function and given they are "equivalent" the function returns
// without error. Otherwise, an error upon expiration of the polling timeout
// is returned.
func WaitForObject(namespace, name string, objGetter func(namespace, name string) (runtime.Object, error), desired runtime.Object, equivalent func(actual, desired runtime.Object) bool) error {
	var actual runtime.Object
	err := wait.PollImmediate(interval, timeout, func() (exists bool, err error) {
		actual, err = objGetter(namespace, name)
		if err != nil {
			if apierrors.IsNotFound(err) || IsTransientError(err) {
				return false, nil
			}
			return false, err
		}

		return equivalent(actual, desired), nil
	})

	if err != nil {
		return fmt.Errorf("Timed out waiting for desired state, \ndesired: %#v\nactual:  %#v\n\nOriginal error: %s", desired, actual, err)
	}

	return nil
}

// WaitForObjectToNotExist fetches the resource using `objGetter` using `namespace`
// and `name` as the keys. If found it tries again until it can confirm it
// no longer exists. If it can confirm it within the polling timeout it
// returns without error. Otherwise, an error is returned.
func WaitForObjectToNotExist(namespace, name string, objGetter func(namespace, name string) (runtime.Object, error)) error {
	err := wait.PollImmediate(interval, timeout, func() (missing bool, err error) {
		_, err = objGetter(namespace, name)
		if errors.IsNotFound(err) {
			return true, nil
		}
		if IsTransientError(err) {
			return false, nil
		}
		return false, err
	})
	if err != nil {
		return fmt.Errorf("Timed out trying to confirm removal of %s in namespace %s\n\nOriginal error: %s", name, namespace, err)
	}
	return nil
}
