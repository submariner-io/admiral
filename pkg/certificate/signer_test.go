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

package certificate_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/certificate"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	assert "github.com/submariner-io/admiral/pkg/test"
	"github.com/submariner-io/admiral/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

var _ = Describe("Signer", func() {
	const namespace = "broker1"

	var (
		dynClient *dynamicfake.FakeDynamicClient
		signer    certificate.Signer
	)

	BeforeEach(func() {
		dynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

		resource.NewDynamicClient = func(_ *rest.Config) (dynamic.Interface, error) {
			return dynClient, nil
		}

		util.BuildRestMapper = func(_ *rest.Config) (meta.RESTMapper, error) {
			return test.GetRESTMapperFor(&corev1.Secret{}), nil
		}
	})

	JustBeforeEach(func() {
		var err error

		signer, err = certificate.NewSigner(certificate.SignerConfig{
			RestConfig: &rest.Config{
				Host: "https://local",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(signer.Start(namespace)).To(Succeed())
	})

	AfterEach(func() {
		signer.Stop(namespace)
	})

	When("a CSR Secret is created and updated", func() {
		It("should sign it", func() {
			// Starting again should be a no-op.
			Expect(signer.Start(namespace)).To(Succeed())

			client := secretClient(dynClient, namespace)

			secret := newCSR()
			test.CreateResource(client, secret)

			Eventually(func(g Gomega) {
				secret = resource.MustFromUnstructured(test.AwaitResource(client, secret.Name), &corev1.Secret{})
				g.Expect(secret.Data).To(HaveKeyWithValue(certificate.TLSDataKey, Not(BeEmpty())))
				g.Expect(secret.Data).To(HaveKeyWithValue(certificate.CADataKey, Not(BeEmpty())))
				g.Expect(secret.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			// It should not try to re-sign and update again.
			Consistently(func() []string {
				return assert.GetOccurredActionVerbs(&dynClient.Fake, "secrets", "update")
			}).Should(HaveLen(1))

			By("Updating the signed Secret")

			secret.Data[certificate.CSRDataKey] = []byte("csr-data-updated")
			delete(secret.Annotations, certificate.RequestSignedLabelKey)

			test.UpdateResource(client, secret)

			Eventually(func(g Gomega) {
				s := resource.MustFromUnstructured(test.AwaitResource(client, secret.Name), &corev1.Secret{})
				g.Expect(s.Data).To(HaveKeyWithValue(certificate.TLSDataKey, Not(Equal(secret.Data[certificate.TLSDataKey]))))
				g.Expect(s.Data).To(HaveKeyWithValue(certificate.CADataKey, Not(Equal(secret.Data[certificate.TLSDataKey]))))
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
				secret = s
			}).To(Succeed())
		})
	})

	Context("Stop", func() {
		It("should cease signing activity", func() {
			signer.Stop(namespace)

			client := secretClient(dynClient, namespace)

			secret := newCSR()
			test.CreateResource(client, secret)

			Consistently(func(g Gomega) {
				secret := resource.MustFromUnstructured(test.AwaitResource(client, secret.Name), &corev1.Secret{})
				g.Expect(secret.Data).NotTo(HaveKey(certificate.TLSDataKey))
				g.Expect(secret.Data).NotTo(HaveKey(certificate.CADataKey))
				g.Expect(secret.Annotations).NotTo(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())
		})
	})

	Context("with multiple namespaces", func() {
		const namespace2 = "broker2"

		It("should not interfere with each other", func() {
			client1 := secretClient(dynClient, namespace)
			client2 := secretClient(dynClient, namespace2)

			Expect(signer.Start(namespace2)).To(Succeed())

			secret := newCSR()

			By("Create Secret in first namespace")

			test.CreateResource(client1, secret)

			Eventually(func(g Gomega) {
				s := resource.MustFromUnstructured(test.AwaitResource(client1, secret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			assert.EnsureNoResource(resource.ForDynamic(client2), secret.Name)

			By("Create Secret in second namespace")

			test.CreateResource(client2, secret)

			Eventually(func(g Gomega) {
				s := resource.MustFromUnstructured(test.AwaitResource(client2, secret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			By("Stop second signer and create new Secret in first namespace")

			signer.Stop(namespace2)

			newSecret := newCSR()
			newSecret.Name += "2"

			test.CreateResource(client1, newSecret)

			Eventually(func(g Gomega) {
				s := resource.MustFromUnstructured(test.AwaitResource(client1, newSecret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())
		})
	})
})

func newCSR() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ipsec",
			Labels: map[string]string{
				certificate.SigningRequestLabelKey: "east",
			},
		},
		Data: map[string][]byte{certificate.CSRDataKey: []byte("csr-data")},
	}
}
