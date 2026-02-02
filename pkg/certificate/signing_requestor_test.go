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
	"fmt"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/certificate"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/resource"
	"github.com/submariner-io/admiral/pkg/syncer/broker"
	"github.com/submariner-io/admiral/pkg/syncer/test"
	assert "github.com/submariner-io/admiral/pkg/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

const (
	localNamespace  = "submariner-operator"
	localClusterID  = "east"
	brokerNamespace = "submariner-k8s-broker"
	secretName      = "ipsec"
)

var ips = []string{"10.253.4.6", "172.19.22.8"}

var _ = Describe("SigningRequestor", func() {
	t := newSigningRequestorTestDriver()

	Context("Issue", t.testIssue)
	Context("", t.testSigned)
	Context("Expiration", t.testExpiration)
	Context("Remove", t.testRemove)
	Context("Uninstall", t.testUninstall)
})

type signingRequestorTestDriver struct {
	signingRequestor  certificate.SigningRequestor
	localDynClient    *dynamicfake.FakeDynamicClient
	brokerDynClient   *dynamicfake.FakeDynamicClient
	onSigned          certificate.OnSignedFn
	signedDataCh      chan map[string][]byte
	certRenewBefore   time.Duration
	certCheckInterval time.Duration
}

func (t *signingRequestorTestDriver) testIssue() {
	It("should create a CSR Secret and sync to the broker", func(ctx SpecContext) {
		Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())

		localSecret := awaitSecret(t.localSecretClient())
		Expect(localSecret.Labels).To(HaveKeyWithValue(certificate.SigningRequestLabelKey, localClusterID))
		Expect(localSecret.Data).To(HaveKey(certificate.CSRDataKey))
		Expect(localSecret.Data).To(HaveKey(certificate.PrivateKeyDataKey))

		brokerSecret := awaitSecret(t.brokerSecretClient())
		Expect(brokerSecret.Labels).To(HaveKeyWithValue(certificate.SigningRequestLabelKey, localClusterID))
		Expect(brokerSecret.Data).To(HaveKeyWithValue(certificate.CSRDataKey, localSecret.Data[certificate.CSRDataKey]))
		Expect(brokerSecret.Data).NotTo(HaveKey(certificate.PrivateKeyDataKey))

		assert.EnsureNoActionsForResource(&t.localDynClient.Fake, "secrets", "update")

		Consistently(func() map[string][]byte {
			return awaitSecret(t.localSecretClient()).Data
		}).Should(HaveKey(certificate.PrivateKeyDataKey))

		// Re-issuing the same request should be a no-op.
		Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
		assert.EnsureNoActionsForResource(&t.brokerDynClient.Fake, "secrets", "update")

		// Re-issuing the request with different IPs should-re-sync it.
		Expect(t.signingRequestor.Issue(ctx, secretName, []string{"120.67.1.2"}, t.onSigned)).To(Succeed())
		Eventually(func(g Gomega) {
			s := awaitSecret(t.brokerSecretClient())
			g.Expect(s.Data[certificate.CSRDataKey]).NotTo(Equal(brokerSecret.Data[certificate.CSRDataKey]))
		}).To(Succeed())
	})

	When("a nil OnSigned function passed", func() {
		It("should return an error", func(ctx SpecContext) {
			Expect(t.signingRequestor.Issue(ctx, secretName, ips, nil)).NotTo(Succeed())
		})
	})

	When("empty IPs are passed", func() {
		It("should return an error", func(ctx SpecContext) {
			Expect(t.signingRequestor.Issue(ctx, secretName, nil, t.onSigned)).NotTo(Succeed())
		})
	})
}

func (t *signingRequestorTestDriver) testSigned() {
	When("a local Secret is signed on the broker", func() {
		JustBeforeEach(func(ctx SpecContext) {
			Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
		})

		It("should be synced locally", func(ctx SpecContext) {
			brokerSecret := t.awaitAndSignBrokerSecret()

			var localSecret *corev1.Secret

			Eventually(func(g Gomega) {
				localSecret = awaitSecret(t.localSecretClient())
				g.Expect(localSecret.Data).To(HaveKeyWithValue(certificate.TLSDataKey, brokerSecret.Data[certificate.TLSDataKey]))
				g.Expect(localSecret.Data).To(HaveKeyWithValue(certificate.CADataKey, brokerSecret.Data[certificate.CADataKey]))
				g.Expect(localSecret.Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())

			Eventually(t.signedDataCh).Should(Receive(Equal(localSecret.Data)))

			// The local private data key should still remain after it's signed.
			Consistently(func() map[string][]byte {
				return awaitSecret(t.localSecretClient()).Data
			}).Should(HaveKey(certificate.PrivateKeyDataKey))

			// It should not try to update on the broker again.
			Consistently(func() []string {
				return assert.GetOccurredActionVerbs(&t.brokerDynClient.Fake, "secrets", "update")
			}).Should(HaveLen(1))
			t.brokerDynClient.Fake.ClearActions()

			// Re-issuing the same request should leave it as signed.

			By("Re-issue the same request")

			Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
			Eventually(func(g Gomega) {
				local := awaitSecret(t.localSecretClient())
				g.Expect(local.Data).To(HaveKeyWithValue(certificate.TLSDataKey, brokerSecret.Data[certificate.TLSDataKey]))
				g.Expect(local.Data).To(HaveKeyWithValue(certificate.CADataKey, brokerSecret.Data[certificate.CADataKey]))
				g.Expect(local.Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())
			assert.EnsureNoActionsForResource(&t.brokerDynClient.Fake, "secrets", "update")
			Consistently(t.signedDataCh).ShouldNot(Receive())

			// Re-issuing the signed request with different IPs should-re-sync it.

			By("Re-issue the request with different IPs")

			Expect(t.signingRequestor.Issue(ctx, secretName, []string{"120.67.1.2"}, t.onSigned)).To(Succeed())
			Eventually(func(g Gomega) {
				s := awaitSecret(t.brokerSecretClient())
				g.Expect(s.Data[certificate.CSRDataKey]).NotTo(Equal(brokerSecret.Data[certificate.CSRDataKey]))
				g.Expect(s.Annotations).NotTo(HaveKey(certificate.RequestSignedLabelKey))
				brokerSecret = s
			}).To(Succeed())
			Consistently(t.signedDataCh).ShouldNot(Receive())

			By("Re-sign the request on the broker")

			t.signBrokerSecret(brokerSecret, 24*time.Hour)

			Eventually(func(g Gomega) {
				localSecret = awaitSecret(t.localSecretClient())
				g.Expect(localSecret.Data).To(HaveKeyWithValue(certificate.TLSDataKey, brokerSecret.Data[certificate.TLSDataKey]))
				g.Expect(localSecret.Data).To(HaveKeyWithValue(certificate.CADataKey, brokerSecret.Data[certificate.CADataKey]))
				g.Expect(localSecret.Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())

			Eventually(t.signedDataCh).Should(Receive(Equal(localSecret.Data)))
		})

		Context("but the signed certificate data is invalid", func() {
			It("should not invoke the OnSigned callback", func() {
				brokerSecret := awaitSecret(t.brokerSecretClient())

				brokerSecret.Annotations = map[string]string{certificate.RequestSignedLabelKey: "true"}
				brokerSecret.Data[certificate.TLSDataKey] = []byte("invalid")

				test.UpdateResource(t.brokerSecretClient(), brokerSecret)

				Consistently(t.signedDataCh).ShouldNot(Receive())
			})
		})
	})

	When("the OnSigned callback fails", func() {
		It("should retry", func(ctx SpecContext) {
			var onSignedErr atomic.Value

			onSignedErr.Store("mock OnSigned error")

			Expect(t.signingRequestor.Issue(ctx, secretName, ips, func(secretData map[string][]byte) error {
				msg := onSignedErr.Swap("").(string)
				if msg != "" {
					return errors.New(msg)
				}

				return t.onSigned(secretData)
			})).To(Succeed())

			t.awaitAndSignBrokerSecret()

			Eventually(t.signedDataCh).Should(Receive())
		})
	})

	When("a existing request is signed just prior to re-issuing the request", func() {
		It("should notify the OnSigned function", func(ctx SpecContext) {
			test.CreateResource(t.localSecretClient(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%s", secretName, localClusterID),
					Labels: map[string]string{
						certificate.SigningRequestLabelKey: localClusterID,
					},
				},
				Data: map[string][]byte{
					certificate.PrivateKeyDataKey: {1},
					certificate.CSRDataKey:        generateTestCSR(),
				},
			})

			t.awaitAndSignBrokerSecret()

			Eventually(func(g Gomega) {
				g.Expect(awaitSecret(t.localSecretClient()).Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
			}).To(Succeed())

			Consistently(func(g Gomega) {
				g.Expect(awaitSecret(t.localSecretClient()).Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
			}).Within(time.Millisecond * 100).To(Succeed())

			Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
			Eventually(t.signedDataCh).Within(5 * time.Second).Should(Receive())
		})
	})

	When("an existing request was already signed prior to startup and the request is re-issued", func() {
		BeforeEach(func() {
			t.localDynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme, resource.MustToUnstructured(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%s", secretName, localClusterID),
					Namespace: localNamespace,
					Labels: map[string]string{
						certificate.SigningRequestLabelKey: localClusterID,
					},
					Annotations: map[string]string{
						certificate.RequestSignedLabelKey: "true",
					},
				},
				Data: map[string][]byte{
					certificate.PrivateKeyDataKey: []byte("private-data"),
					certificate.CSRDataKey:        generateTestCSR(),
					certificate.TLSDataKey:        generateTestCertificate(24 * time.Hour),
					certificate.CADataKey:         []byte("ca-data"),
				},
			}))
		})

		It("should notify the OnSigned function", func(ctx SpecContext) {
			time.Sleep(500 * time.Millisecond)
			Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
			Eventually(t.signedDataCh).Within(5 * time.Second).Should(Receive())
		})
	})

	When("a Secret from another cluster is created/updated on the broker", func() {
		var secret *corev1.Secret

		BeforeEach(func() {
			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ipsec-west",
					Namespace: brokerNamespace,
					Labels: map[string]string{
						certificate.SigningRequestLabelKey: "west",
					},
				},
			}

			t.brokerDynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme, resource.MustToUnstructured(secret))
		})

		It("should not be synced locally", func() {
			assert.EnsureNoResource(resource.ForDynamic(t.localSecretClient()), secret.Name)

			secret.Annotations = map[string]string{certificate.RequestSignedLabelKey: "true"}
			test.UpdateResource(t.brokerSecretClient(), secret)

			assert.EnsureNoResource(resource.ForDynamic(t.localSecretClient()), secret.Name)
		})
	})
}

func (t *signingRequestorTestDriver) testExpiration() {
	BeforeEach(func() {
		t.certCheckInterval = time.Millisecond * 20
		t.certRenewBefore = certificate.CertRenewBefore
	})

	JustBeforeEach(func(ctx SpecContext) {
		Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())
	})

	When("the signed certificate has not expired", func() {
		It("should not re-issue the request", func() {
			t.awaitAndSignBrokerSecret()

			Consistently(func(g Gomega) {
				s := awaitSecret(t.brokerSecretClient())
				g.Expect(s.Annotations).To(HaveKeyWithValue(certificate.RequestSignedLabelKey, "true"))
			}).Should(Succeed())
		})
	})

	When("the signed certificate expires", func() {
		It("should re-issue the request to be re-signed", func() {
			t.signBrokerSecret(awaitSecret(t.brokerSecretClient()), time.Hour)

			Eventually(func(g Gomega) {
				s := awaitSecret(t.brokerSecretClient())
				g.Expect(s.Annotations).NotTo(HaveKey(certificate.RequestSignedLabelKey))
			}).Within(3 * time.Second).Should(Succeed())
		})
	})
}

func (t *signingRequestorTestDriver) testRemove() {
	It("should remove a previously issued request", func(ctx SpecContext) {
		Expect(t.signingRequestor.Issue(ctx, secretName, ips, t.onSigned)).To(Succeed())

		secret := awaitSecret(t.localSecretClient())
		awaitSecret(t.brokerSecretClient())

		Expect(t.signingRequestor.Remove(ctx, secretName)).To(Succeed())

		test.AwaitNoResource(t.localSecretClient(), secret.Name)
		test.AwaitNoResource(t.brokerSecretClient(), secret.Name)
	})

	It("should not return an error if not previously issued", func(ctx SpecContext) {
		Expect(t.signingRequestor.Remove(ctx, secretName)).To(Succeed())
	})
}

func (t *signingRequestorTestDriver) testUninstall() {
	It("should remove all local Secret requests", func(ctx SpecContext) {
		Expect(t.signingRequestor.Issue(ctx, "secret1", ips, t.onSigned)).To(Succeed())
		Expect(t.signingRequestor.Issue(ctx, "secret2", ips, t.onSigned)).To(Succeed())

		Eventually(ctx, func(g Gomega, ctx context.Context) {
			list, err := t.brokerSecretClient().List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(list.Items).To(HaveLen(2))
		}).To(Succeed())

		otherSecret := test.CreateResource(t.brokerSecretClient(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ipsec-west",
				Labels: map[string]string{
					certificate.SigningRequestLabelKey: "west",
				},
			},
		})

		Expect(t.signingRequestor.Uninstall(ctx)).To(Succeed())

		Eventually(ctx, func(g Gomega, ctx context.Context) {
			list, err := t.localSecretClient().List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(list.Items).To(BeEmpty())
		}).To(Succeed())

		Eventually(ctx, func(g Gomega, ctx context.Context) {
			list, err := t.brokerSecretClient().List(ctx, metav1.ListOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(list.Items).To(HaveLen(1))
			g.Expect(list.Items[0].GetName()).To(Equal(otherSecret.Name))
		}).To(Succeed())
	})
}

func newSigningRequestorTestDriver() *signingRequestorTestDriver {
	t := &signingRequestorTestDriver{}

	BeforeEach(func() {
		t.localDynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
		t.brokerDynClient = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

		fake.AddDeleteCollectionReactor(&t.localDynClient.Fake)
		fake.AddDeleteCollectionReactor(&t.brokerDynClient.Fake)

		t.signedDataCh = make(chan map[string][]byte, 10)
		t.onSigned = func(data map[string][]byte) error {
			t.signedDataCh <- data
			return nil
		}
	})

	JustBeforeEach(func() {
		var err error

		stopCh := make(chan struct{})

		syncerConfig := broker.SyncerConfig{
			LocalNamespace:  localNamespace,
			LocalClusterID:  localClusterID,
			LocalClient:     t.localDynClient,
			BrokerNamespace: brokerNamespace,
			BrokerClient:    t.brokerDynClient,
			RestMapper:      test.GetRESTMapperFor(&corev1.Secret{}),
		}

		if t.certCheckInterval > 0 {
			t.signingRequestor, err = certificate.StartSigningRequestorWithOpts(syncerConfig, stopCh, t.certCheckInterval, t.certRenewBefore)
		} else {
			t.signingRequestor, err = certificate.StartSigningRequestor(syncerConfig, stopCh)
		}

		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			close(stopCh)
		})
	})

	return t
}

func (t *signingRequestorTestDriver) awaitAndSignBrokerSecret() *corev1.Secret {
	return t.signBrokerSecret(awaitSecret(t.brokerSecretClient()), certificate.CertValidity)
}

func (t *signingRequestorTestDriver) signBrokerSecret(brokerSecret *corev1.Secret, validFor time.Duration) *corev1.Secret {
	brokerSecret.Annotations = map[string]string{certificate.RequestSignedLabelKey: "true"}
	brokerSecret.Data[certificate.TLSDataKey] = generateTestCertificate(validFor)
	brokerSecret.Data[certificate.CADataKey] = []byte("ca-data")

	test.UpdateResource(t.brokerSecretClient(), brokerSecret)

	return brokerSecret
}

func (t *signingRequestorTestDriver) brokerSecretClient() dynamic.ResourceInterface {
	return secretClient(t.brokerDynClient, brokerNamespace)
}

func (t *signingRequestorTestDriver) localSecretClient() dynamic.ResourceInterface {
	return secretClient(t.localDynClient, localNamespace)
}

func awaitSecret(client dynamic.ResourceInterface) *corev1.Secret {
	return resource.MustFromUnstructured(test.AwaitResource(client, fmt.Sprintf("%s-%s", secretName, localClusterID)),
		&corev1.Secret{})
}

func generateTestCertificate(validFor time.Duration) []byte {
	_, certPEM, err := certificate.CreatePEMEncodedKeyAndCertificate("test-cert", validFor)
	Expect(err).NotTo(HaveOccurred())

	return certPEM
}

func secretClient(c dynamic.Interface, ns string) dynamic.ResourceInterface {
	return c.Resource(corev1.SchemeGroupVersion.WithResource("secrets")).Namespace(ns)
}
