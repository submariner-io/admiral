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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/certificate"
)

var _ = Describe("CreatePEMEncodedKeyAndCertificateRequest", func() {
	When("an IP is invalid", func() {
		It("should return an error", func() {
			_, _, err := certificate.CreatePEMEncodedKeyAndCertificateRequest("test", []string{"invalid"})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("ParseCertificateFromPEM", func() {
	When("the PEM is valid", func() {
		It("should parse it", func() {
			cert, err := certificate.ParseCertificateFromPEM(generateTestCertificate(24 * time.Hour))
			Expect(err).NotTo(HaveOccurred())
			Expect(cert).NotTo(BeNil())
		})
	})

	When("the PEM is invalid", func() {
		It("should return an error", func() {
			_, err := certificate.ParseCertificateFromPEM(nil)
			Expect(err).To(HaveOccurred())

			_, err = certificate.ParseCertificateFromPEM([]byte{})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("ParseCertificateRequestFromPEM", func() {
	When("the PEM is valid", func() {
		It("should parse it", func() {
			cert, err := certificate.ParseCertificateRequestFromPEM(generateTestCSR())
			Expect(err).NotTo(HaveOccurred())
			Expect(cert).NotTo(BeNil())
		})
	})

	When("the PEM is invalid", func() {
		It("should return an error", func() {
			_, err := certificate.ParseCertificateRequestFromPEM(nil)
			Expect(err).To(HaveOccurred())

			_, err = certificate.ParseCertificateRequestFromPEM([]byte{})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("ParsePKCS1PrivateKeyFromPEM", func() {
	When("the PEM is valid", func() {
		It("should parse it", func() {
			privateKey, err := rsa.GenerateKey(rand.Reader, certificate.RSABitSize)
			Expect(err).NotTo(HaveOccurred())

			cert, err := certificate.ParsePKCS1PrivateKeyFromPEM(
				pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}))
			Expect(err).NotTo(HaveOccurred())
			Expect(cert).NotTo(BeNil())
		})
	})

	When("the PEM is invalid", func() {
		It("should return an error", func() {
			_, err := certificate.ParsePKCS1PrivateKeyFromPEM(nil)
			Expect(err).To(HaveOccurred())

			_, err = certificate.ParsePKCS1PrivateKeyFromPEM([]byte{})
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("ExtractIPsFromCertificateRequestPEM", func() {
	When("the PEM is valid", func() {
		It("should extract the IPs", func() {
			expIPs := []string{"192.168.1.1", "170.19.1.1"}
			_, csrPEM, err := certificate.CreatePEMEncodedKeyAndCertificateRequest("test-cert", expIPs)
			Expect(err).NotTo(HaveOccurred())

			actualIPs, err := certificate.ExtractIPsFromCertificateRequestPEM(csrPEM)
			Expect(err).NotTo(HaveOccurred())
			Expect(actualIPs).To(Equal(expIPs))
		})
	})

	When("the PEM is invalid", func() {
		It("should return an error", func() {
			_, err := certificate.ExtractIPsFromCertificateRequestPEM(nil)
			Expect(err).To(HaveOccurred())
		})
	})
})
