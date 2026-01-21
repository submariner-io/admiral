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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	fakereactor "github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/federate"
	resourceutils "github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	fakeClient "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/cache"
)

func testWithMissingNamespace() {
	const (
		transformedNamespace = "transformed-ns"
		noTransform          = "no-transform"
	)

	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	namespaceClient := func() dynamic.ResourceInterface {
		return t.config.SourceClient.Resource(corev1.SchemeGroupVersion.WithResource("namespaces")).Namespace(metav1.NamespaceNone)
	}

	createNamespace := func(name string) {
		test.CreateResource(namespaceClient(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		})
	}

	BeforeEach(func() {
		t.config.Transform = func(obj runtime.Object, _ int, _ syncer.Operation) (runtime.Object, bool) {
			if resourceutils.MustToMeta(obj).GetName() == noTransform {
				return nil, false
			} else if resourceutils.MustToMeta(obj).GetNamespace() == test.LocalNamespace {
				obj = obj.DeepCopyObject()
				resourceutils.MustToMeta(obj).SetNamespace(transformedNamespace)
			}

			return obj, false
		}

		t.config.NamespaceInformer = cache.NewSharedInformer(cache.ToListWatcherWithWatchListSemantics(&cache.ListWatch{
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				return namespaceClient().List(ctx, options)
			},
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				return namespaceClient().Watch(ctx, options)
			},
		}, fakeClient.NewSimpleDynamicClient(scheme.Scheme)), resourceutils.MustToUnstructured(&corev1.Namespace{}), 0)
	})

	JustBeforeEach(func() {
		t.federator.SetDelegator(federate.NewCreateFederator(t.config.SourceClient, t.config.RestMapper, transformedNamespace))

		createNamespace(test.LocalNamespace)

		fakereactor.AddVerifyNamespaceReactor(&t.config.SourceClient.(*fakeClient.FakeDynamicClient).Fake, "pods")
	})

	Specify("distribute should eventually succeed when the namespace is created", func() {
		resource := test.CreateResource(t.sourceClient, t.resource)
		t.federator.VerifyNoDistribute()

		By("Creating namespace")

		createNamespace(transformedNamespace)

		resource.SetNamespace(transformedNamespace)
		t.federator.VerifyDistribute(resource)
	})

	Context("and no namespace informer specified", func() {
		BeforeEach(func() {
			t.config.NamespaceInformer = nil
		})

		It("should not retry", func() {
			test.CreateResource(t.sourceClient, t.resource)
			t.federator.VerifyNoDistribute()
		})
	})

	Context("after a namespace is created and distribute succeeds", func() {
		const otherNS = "other-ns"

		JustBeforeEach(func() {
			createNamespace(transformedNamespace)
			createNamespace(otherNS)
		})

		It("should eventually redistribute when the namespace is recreated", func() {
			resource := test.CreateResource(t.sourceClient, t.resource)
			resource.SetNamespace(transformedNamespace)
			t.federator.VerifyDistribute(resource)

			other := t.resource.DeepCopy()
			other.Name = noTransform
			test.CreateResource(t.sourceClient, other)

			other = t.resource.DeepCopy()
			other.Namespace = otherNS
			test.CreateResource(t.config.SourceClient.Resource(
				*test.GetGroupVersionResourceFor(t.config.RestMapper, other)).Namespace(otherNS), other)

			By("Deleting namespace")

			err := namespaceClient().Delete(ctx, transformedNamespace, metav1.DeleteOptions{})
			Expect(err).To(Succeed())

			By("Recreating namespace")

			createNamespace(transformedNamespace)
			t.federator.VerifyDistribute(resource)
		})
	})
}
