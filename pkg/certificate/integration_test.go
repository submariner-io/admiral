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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/certificate"
	"github.com/submariner-io/admiral/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var _ = Describe("Integration", func() {
	tSR := newSigningRequestorTestDriver()
	tS := newSignerTestDriver()

	BeforeEach(func() {
		tS.dynClient = tSR.brokerDynClient

		resource.NewDynamicClient = func(_ *rest.Config) (dynamic.Interface, error) {
			return tS.dynClient, nil
		}

		tS.config.CertValidity = time.Second * 3

		tSR.certCheckInterval = time.Millisecond * 200
		tSR.certRenewBefore = time.Second
	})

	Specify("an issued request should get signed", func() {
		Expect(tSR.signingRequestor.Issue(ctx, secretName, ips, tSR.onSigned)).To(Succeed())

		var localSecret *corev1.Secret

		Eventually(func(g Gomega) {
			localSecret = awaitSecret(tSR.localSecretClient())
			g.Expect(localSecret.Annotations).To(HaveKey(certificate.RequestSignedLabelKey))
		}).To(Succeed())

		Eventually(tSR.signedDataCh).Should(Receive(Equal(localSecret.Data)))

		// Should expire and be re-signed.
		Eventually(func(g Gomega) {
			s := awaitSecret(tSR.localSecretClient())
			g.Expect(s.Data[certificate.TLSDataKey]).NotTo(Equal(localSecret.Data[certificate.TLSDataKey]))
			localSecret = s
		}).Within(tS.config.CertValidity + time.Second).To(Succeed())

		Eventually(tSR.signedDataCh).Should(Receive(Equal(localSecret.Data)))
	})
})
