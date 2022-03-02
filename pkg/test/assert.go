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

package test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/testing"
)

func EnsureNoActionsForResource(f *testing.Fake, resourceType string, expectedVerbs ...string) {
	Consistently(func() []string {
		expSet := sets.NewString(expectedVerbs...)
		verbs := []string{}

		actualActions := f.Actions()
		for i := range actualActions {
			if actualActions[i].GetResource().Resource == resourceType && expSet.Has(actualActions[i].GetVerb()) {
				verbs = append(verbs, actualActions[i].GetVerb())
			}
		}

		return verbs
	}).Should(BeEmpty())
}

func AwaitFinalizer(client resource.Interface, name, finalizer string) {
	Eventually(func() []string {
		return GetFinalizers(client, name)
	}).Should(ContainElement(finalizer))
}

func AwaitNoFinalizer(client resource.Interface, name, finalizer string) {
	Eventually(func() []string {
		return GetFinalizers(client, name)
	}).ShouldNot(ContainElement(finalizer))
}

func AssertFinalizers(client resource.Interface, name string, finalizers ...string) {
	if finalizers == nil {
		finalizers = []string{}
	}

	Expect(GetFinalizers(client, name)).To(Equal(finalizers))
}

func AwaitStatusCondition(expCond *metav1.Condition, get func() ([]metav1.Condition, error)) {
	var found *metav1.Condition

	err := wait.PollImmediate(50*time.Millisecond, 5*time.Second, func() (bool, error) {
		conditions, err := get()
		if err != nil {
			return false, err
		}

		found = meta.FindStatusCondition(conditions, expCond.Type)
		if found == nil {
			return false, nil
		}

		return found.Status == expCond.Status && found.Reason == expCond.Reason, nil
	})

	if errors.Is(err, wait.ErrWaitTimeout) {
		Expect(found).ToNot(BeNil(), "Status condition not found")
		Expect(found.Type).To(Equal(expCond.Type))
		Expect(found.Status).To(Equal(expCond.Status))
		Expect(found.LastTransitionTime).To(Not(BeNil()))
		Expect(found.Message).To(Not(BeEmpty()))
		Expect(found.Reason).To(Equal(expCond.Reason))
	} else {
		Expect(err).To(Succeed())
	}
}

func AwaitResource(client resource.Interface, name string) runtime.Object {
	var obj runtime.Object

	Eventually(func() error {
		var err error

		obj, err = client.Get(context.TODO(), name, metav1.GetOptions{})
		return err
	}, 3).Should(Succeed())

	return obj
}

func AwaitNoResource(client resource.Interface, name string) {
	Eventually(func() bool {
		_, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 3).Should(BeTrue(), "Found unexpected resource %q", name)
}

func EnsureNoResource(client resource.Interface, name string) {
	Consistently(func() bool {
		_, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 3).Should(BeTrue(), "Found unexpected resource %q", name)
}

func AwaitUpdateAction(f *testing.Fake, resourceType, name string) runtime.Object {
	var retObj runtime.Object

	Eventually(func() bool {
		actions := f.Actions()
		for i := range actions {
			if actions[i].GetVerb() == "update" && actions[i].GetResource().Resource == resourceType {
				update := actions[i].(testing.UpdateAction)
				obj, err := meta.Accessor(update.GetObject())
				Expect(err).To(Succeed())

				if obj.GetName() == name {
					retObj = update.GetObject()
					return true
				}
			}
		}

		return false
	}).Should(BeTrue(), "Expected update action for resource %q of type %q", name, resourceType)

	return retObj
}
