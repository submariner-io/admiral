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
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/certificate"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	assert "github.com/submariner-io/admiral/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

var _ = Describe("Signer", func() {
	t := newSignerTestDriver()

	Context("Start", t.testStart)
	Context("Stop", t.testStop)
	Context("CA expiration", t.testExpiration)
})

type signerTestDriver struct {
	dynClient *dynamicfake.FakeDynamicClient
	signer    certificate.Signer
	config    certificate.SignerConfig
}

func (t *signerTestDriver) testStart() {
	When("a CSR Secret is created and updated", func() {
		It("should sign it", func(ctx SpecContext) {
			secret := newCSR()
			test.CreateResource(ctx, t.secretClient(), secret)

			Eventually(ctx, func(g Gomega, ctx context.Context) {
				secret = resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), secret.Name), &corev1.Secret{})
				g.Expect(secret.Data).To(HaveKeyWithValue(certificate.TLSDataKey, Not(BeEmpty())))
				g.Expect(secret.Data).To(HaveKeyWithValue(certificate.CADataKey, Not(BeEmpty())))
				g.Expect(secret.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			// It should not try to re-sign and update again.
			Consistently(func() []string {
				return assert.GetOccurredActionVerbs(&t.dynClient.Fake, "secrets", "update")
			}).Should(HaveLen(1))

			By("Updating the signed Secret")

			secret.Data[certificate.CSRDataKey] = generateTestCSR()
			delete(secret.Annotations, certificate.RequestSignedLabelKey)

			test.UpdateResource(ctx, t.secretClient(), secret)

			Eventually(ctx, func(g Gomega, ctx context.Context) {
				s := resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), secret.Name), &corev1.Secret{})
				g.Expect(s.Data).To(HaveKeyWithValue(certificate.TLSDataKey, Not(Equal(secret.Data[certificate.TLSDataKey]))))
				g.Expect(s.Data).To(HaveKeyWithValue(certificate.CADataKey, Not(Equal(secret.Data[certificate.TLSDataKey]))))
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
				secret = s
			}).To(Succeed())

			t.dynClient.Fake.ClearActions()

			// Starting again should be a no-op.
			Expect(t.signer.Start(ctx, brokerNamespace)).To(Succeed())

			assert.EnsureNoActionsForResource(&t.dynClient.Fake, "secrets", "update")
		})
	})

	Context("with multiple namespaces", func() {
		const namespace2 = "broker2"

		It("should not interfere with each other", func(ctx SpecContext) {
			client1 := secretClient(t.dynClient, brokerNamespace)
			client2 := secretClient(t.dynClient, namespace2)

			Expect(t.signer.Start(ctx, namespace2)).To(Succeed())

			secret := newCSR()

			By("Create Secret in first namespace")

			test.CreateResource(ctx, client1, secret)

			Eventually(ctx, func(g Gomega, ctx context.Context) {
				s := resource.MustFromUnstructured(test.AwaitResource(ctx, client1, secret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			assert.EnsureNoResource(ctx, resource.ForDynamic(client2), secret.Name)

			By("Create Secret in second namespace")

			test.CreateResource(ctx, client2, secret)

			Eventually(ctx, func(g Gomega, ctx context.Context) {
				s := resource.MustFromUnstructured(test.AwaitResource(ctx, client2, secret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())

			By("Stop second signer and create new Secret in first namespace")

			t.signer.Stop(namespace2)

			newSecret := newCSR()
			newSecret.Name += "2"

			test.CreateResource(ctx, client1, newSecret)

			Eventually(ctx, func(g Gomega, ctx context.Context) {
				s := resource.MustFromUnstructured(test.AwaitResource(ctx, client1, newSecret.Name), &corev1.Secret{})
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
			}).To(Succeed())
		})
	})
}

func (t *signerTestDriver) testStop() {
	Context("Stop", func() {
		It("should cease signing activity", func(ctx context.Context) {
			t.signer.Stop(brokerNamespace)

			client := secretClient(t.dynClient, brokerNamespace)

			secret := newCSR()
			test.CreateResource(ctx, client, secret)

			Consistently(ctx, func(g Gomega, ctx context.Context) {
				secret := resource.MustFromUnstructured(test.AwaitResource(ctx, client, secret.Name), &corev1.Secret{})
				g.Expect(secret.Data).NotTo(HaveKey(certificate.TLSDataKey))
				g.Expect(secret.Data).NotTo(HaveKey(certificate.CADataKey))
				g.Expect(secret.Annotations).NotTo(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())
		})
	})
}

func (t *signerTestDriver) testExpiration() {
	var (
		caSecret  *corev1.Secret
		csrSecret *corev1.Secret
	)

	BeforeEach(func() {
		t.config.CACheckInterval = time.Millisecond * 20
	})

	JustBeforeEach(func(ctx context.Context) {
		caSecret = resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), certificate.CASecretName), &corev1.Secret{})

		csrSecret = newCSR()
		test.CreateResource(ctx, t.secretClient(), csrSecret)

		Eventually(ctx, func(g Gomega, ctx context.Context) {
			csrSecret = resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), csrSecret.Name), &corev1.Secret{})
			g.Expect(csrSecret.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))
		}).To(Succeed())
	})

	When("the CA certificate has not expired", func() {
		It("should not rotate it", func(ctx context.Context) {
			Consistently(ctx, func(g Gomega, ctx context.Context) {
				s := test.AwaitResource(ctx, t.secretClient(), csrSecret.Name)
				g.Expect(s.GetAnnotations()).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, Not(BeEmpty())))

				s = test.AwaitResource(ctx, t.secretClient(), certificate.CASecretName)
				g.Expect(s.GetAnnotations()).To(Equal(caSecret.Annotations))
				g.Expect(resource.MustFromUnstructured(s, &corev1.Secret{}).Data).To(Equal(caSecret.Data))
			}).Within(time.Millisecond * 200).To(Succeed())
		})
	})

	When("the CA certificate expires", func() {
		BeforeEach(func() {
			t.config.CACertValidity = time.Second * 2
			t.config.RotateBefore = time.Second
		})

		It("should rotate it and re-sign all CSRs", func(ctx context.Context) {
			Eventually(ctx, func(g Gomega, ctx context.Context) {
				s := resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), csrSecret.Name), &corev1.Secret{})
				g.Expect(s.Data[certificate.TLSDataKey]).NotTo(Equal(csrSecret.Data[certificate.TLSDataKey]))

				s = resource.MustFromUnstructured(test.AwaitResource(ctx, t.secretClient(), certificate.CASecretName), &corev1.Secret{})
				g.Expect(s.Annotations).NotTo(Equal(caSecret.Annotations))
				g.Expect(s.Data).NotTo(Equal(caSecret.Data))
			}).Within(t.config.CACertValidity + time.Second).To(Succeed())
		})
	})
}

func (t *signerTestDriver) secretClient() dynamic.ResourceInterface {
	return secretClient(t.dynClient, brokerNamespace)
}

func newSignerTestDriver() *signerTestDriver {
	t := &signerTestDriver{}

	BeforeEach(func() {
		t.config = certificate.SignerConfig{
			RestConfig: &rest.Config{
				Host: "https://local",
			},
		}

		t.dynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

		resource.NewDynamicClient = func(_ *rest.Config) (dynamic.Interface, error) {
			return t.dynClient, nil
		}
	})

	JustBeforeEach(func(ctx SpecContext) {
		var err error

		t.signer, err = certificate.NewSigner(t.config)
		Expect(err).NotTo(HaveOccurred())

		Expect(t.signer.Start(ctx, brokerNamespace)).To(Succeed())
	})

	AfterEach(func() {
		t.signer.Stop(brokerNamespace)
	})

	return t
}

func newCSR() *corev1.Secret {
	// Generate valid CSR data
	csrPEM := generateTestCSR()

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ipsec",
			Labels: map[string]string{
				certificate.SigningRequestLabelKey: "east",
			},
		},
		Data: map[string][]byte{certificate.CSRDataKey: csrPEM},
	}
}

func generateTestCSR() []byte {
	_, csrPEM, err := certificate.CreatePEMEncodedKeyAndCertificateRequest("test-cert", []string{"192.168.1.1"})
	Expect(err).NotTo(HaveOccurred())

	return csrPEM
}
