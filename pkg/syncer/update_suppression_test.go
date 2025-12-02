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
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func testUpdateSuppression() {
	t := newTestDriver(test.LocalNamespace, "", syncer.LocalToRemote)

	BeforeEach(func() {
		t.addInitialResource(t.resource)
	})

	JustBeforeEach(func() {
		t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
		test.UpdateResource(t.sourceClient, t.resource)
	})

	When("no equivalence function is specified", func() {
		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})

		Context("and the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.ObjectMeta.Finalizers = []string{"test"}
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})
	})

	When("the default equivalence function is specified", func() {
		BeforeEach(func() {
			t.config.ResourcesEquivalent = syncer.DefaultResourcesEquivalent
		})

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.Status.Phase = corev1.PodRunning
			})

			It("should not distribute it", func() {
				t.federator.VerifyNoDistribute()
			})
		})

		Context("and the resource's ObjectMeta is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.ObjectMeta.Finalizers = []string{"test"}
			})

			It("should not distribute it", func() {
				t.federator.VerifyNoDistribute()
			})
		})

		Context("and the resource's Labels are updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.SetLabels(map[string]string{"new-label": "value"})
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})

		Context("and the resource's Annotations are updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.SetAnnotations(map[string]string{"new-annotations": "value"})
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})
	})

	When("a custom equivalence function is specified that compares Status", func() {
		BeforeEach(func() {
			t.config.ResourcesEquivalent = func(obj1, obj2 *unstructured.Unstructured) bool {
				return equality.Semantic.DeepEqual(util.GetNestedField(obj1, "status"),
					util.GetNestedField(obj2, "status"))
			}
		})

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.Status.Phase = corev1.PodRunning
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})
	})

	When("the Spec equivalence function is specified ", func() {
		BeforeEach(func() {
			t.config.ResourcesEquivalent = syncer.AreSpecsEquivalent
		})

		Context("and the resource's Spec is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.Spec.Hostname = "newHost"
			})

			It("should distribute it", func() {
				t.federator.VerifyDistribute(test.GetResource(t.sourceClient, t.resource))
			})
		})

		Context("and the resource's Status is updated in the datastore", func() {
			BeforeEach(func() {
				t.resource.Status.Phase = corev1.PodRunning
			})

			It("should not distribute it", func() {
				t.federator.VerifyNoDistribute()
			})
		})
	})
}
