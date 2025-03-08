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

package configmap_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/configmap"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Get", func() {
	var (
		clientSet *k8sfake.Clientset
		client    resource.Interface[*corev1.ConfigMap]
	)

	BeforeEach(func() {
		clientSet = k8sfake.NewClientset()
		client = resource.ForConfigMap(clientSet, test.LocalNamespace)
	})

	When("the ConfigMap exists", func() {
		It("should return it", func() {
			expected, err := client.Create(context.TODO(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cm",
				},
				Data: map[string]string{"foo": "bar"},
			}, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			actual, err := configmap.Get(context.TODO(), client, expected.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})
	})

	When("the ConfigMap does not exist", func() {
		It("should return an empty ConfigMap", func() {
			cm, err := configmap.Get(context.TODO(), client, "test-cm")
			Expect(err).ToNot(HaveOccurred())
			Expect(cm.Name).To(Equal("test-cm"))
			Expect(cm.Data).To(BeEmpty())
		})
	})

	When("ConfigMap retrieval fails", func() {
		It("should return an error", func() {
			fake.FailOnAction(&clientSet.Fake, "configmaps", "get", nil, false)
			_, err := configmap.Get(context.TODO(), client, "test-cm")
			Expect(err).To(HaveOccurred())
		})
	})
})

func TestConfigmap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ConfigMap Suite")
}
