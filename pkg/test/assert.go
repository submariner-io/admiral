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
	"strings"
	"time"

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/testing"
	"k8s.io/utils/set"
)

func EnsureNoActionsForResource(f *testing.Fake, resourceType string, expectedVerbs ...string) {
	Consistently(func() []string {
		return GetOccurredActionVerbs(f, resourceType, expectedVerbs...)
	}).Should(BeEmpty())
}

func EnsureActionsForResource(f *testing.Fake, resourceType string, expectedVerbs ...string) {
	Eventually(func() []string {
		return GetOccurredActionVerbs(f, resourceType, expectedVerbs...)
	}).ShouldNot(BeEmpty())
}

func GetOccurredActionVerbs(f *testing.Fake, resourceType string, expectedVerbs ...string) []string {
	expSet := set.New(expectedVerbs...)
	verbs := []string{}

	subresource := ""
	if index := strings.Index(resourceType, "/"); index != -1 {
		subresource = resourceType[index+1:]
		resourceType = resourceType[:index]
	}

	actualActions := f.Actions()
	for i := range actualActions {
		verb := actualActions[i].GetVerb()
		if actualActions[i].GetResource().Resource == resourceType && actualActions[i].GetSubresource() == subresource && expSet.Has(verb) {
			verbs = append(verbs, verb)
		}
	}

	return verbs
}

func AwaitFinalizer[T runtime.Object](client resource.Interface[T], name, finalizer string) {
	Eventually(func() []string {
		return GetFinalizers(client, name)
	}).Should(ContainElement(finalizer))
}

func AwaitNoFinalizer[T runtime.Object](client resource.Interface[T], name, finalizer string) {
	Eventually(func() []string {
		return GetFinalizers(client, name)
	}).ShouldNot(ContainElement(finalizer))
}

func AssertFinalizers[T runtime.Object](client resource.Interface[T], name string, finalizers ...string) {
	if finalizers == nil {
		finalizers = []string{}
	}

	Expect(GetFinalizers(client, name)).To(Equal(finalizers))
}

func AwaitStatusCondition(expCond *metav1.Condition, get func() ([]metav1.Condition, error)) {
	var found *metav1.Condition

	Eventually(func() bool {
		conditions, err := get()
		Expect(err).To(Succeed())

		found = meta.FindStatusCondition(conditions, expCond.Type)
		if found == nil {
			return false
		}

		return found.Status == expCond.Status && found.Reason == expCond.Reason
	}, 5*time.Second, 50*time.Millisecond).Should(BeTrue(), "Status condition not found: %s", resource.ToJSON(expCond))

	Expect(found.LastTransitionTime).To(Not(BeNil()))
}

func AwaitResource[T runtime.Object](client resource.Interface[T], name string) runtime.Object {
	var obj runtime.Object

	Eventually(func() error {
		var err error

		obj, err = client.Get(context.TODO(), name, metav1.GetOptions{})
		return err
	}, 3).Should(Succeed())

	return obj
}

func AwaitNoResource[T runtime.Object](client resource.Interface[T], name string) {
	Eventually(func() bool {
		_, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 3).Should(BeTrue(), "Found unexpected resource %q", name)
}

func EnsureNoResource[T runtime.Object](client resource.Interface[T], name string) {
	Consistently(func() bool {
		_, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, 300*time.Millisecond).Should(BeTrue(), "Found unexpected resource %q", name)
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
