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

package syncer_test

import (
	. "github.com/onsi/ginkgo/v2"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func testReconcileLocalToRemote() {
	t := newTestDriver(metav1.NamespaceAll, "local", syncer.LocalToRemote)

	BeforeEach(func() {
		test.SetClusterIDLabel(t.resource, "local")
		t.resource.Labels[syncer.OrigNamespaceLabelKey] = test.LocalNamespace
	})

	JustBeforeEach(func() {
		t.resource.Namespace = test.RemoteNamespace
		obj := t.resource.DeepCopyObject()
		t.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile is local and does not exist", func() {
		It("should invoke delete", func() {
			t.resource.Namespace = test.LocalNamespace
			t.federator.VerifyDelete(test.SetClusterIDLabel(t.resource, ""))
		})
	})

	When("the resource to reconcile is local and does exist", func() {
		BeforeEach(func() {
			t.resource.Namespace = test.LocalNamespace
			t.addInitialResource(t.resource)
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})

	When("the resource to reconcile is not local", func() {
		BeforeEach(func() {
			test.SetClusterIDLabel(t.resource, "remote")
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})
}

func testReconcileRemoteToLocal() {
	t := newTestDriver(test.RemoteNamespace, "local", syncer.RemoteToLocal)

	BeforeEach(func() {
		t.resource.Namespace = test.RemoteNamespace
		test.SetClusterIDLabel(t.resource, "remote")
	})

	JustBeforeEach(func() {
		obj := t.resource.DeepCopyObject()
		t.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile is remote and does not exist", func() {
		It("should invoke delete", func() {
			t.federator.VerifyDelete(t.resource)
		})
	})

	When("the resource to reconcile is remote and does exist", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})

	When("the resource to reconcile is not remote", func() {
		BeforeEach(func() {
			test.SetClusterIDLabel(t.resource, "local")
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})
}

func testReconcileNoDirection() {
	t := newTestDriver(test.LocalNamespace, "local", syncer.None)

	var toReconcile runtime.Object

	BeforeEach(func() {
		toReconcile = t.resource
		t.resource.Namespace = test.LocalNamespace
	})

	JustBeforeEach(func() {
		obj := toReconcile.DeepCopyObject()

		t.syncer.Reconcile(func() []runtime.Object {
			return []runtime.Object{obj}
		})
	})

	When("the resource to reconcile does not exist", func() {
		It("should invoke delete", func() {
			t.federator.VerifyDelete(t.resource)
		})
	})

	When("the resource to reconcile does exist", func() {
		BeforeEach(func() {
			t.addInitialResource(t.resource)
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})

	When("the originating namespace of the resource to reconcile cannot be determined", func() {
		BeforeEach(func() {
			t.resource.Namespace = ""
		})

		It("should not invoke delete", func() {
			t.federator.VerifyNoDelete()
		})
	})

	When("the type of the resource to reconcile is invalid", func() {
		BeforeEach(func() {
			toReconcile = &corev1.Namespace{}
		})

		It("should ignore it", func() {
			t.federator.VerifyNoDelete()
		})
	})
}
