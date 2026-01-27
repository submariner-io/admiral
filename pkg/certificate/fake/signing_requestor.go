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

package fake

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	_ "embed"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/certificate"
)

type SigningRequestor struct {
	caKey       *rsa.PrivateKey
	caCert      *x509.Certificate
	caPEM       []byte
	issuedCh    chan []string
	onUninstall chan struct{}
}

var _ certificate.SigningRequestor = &SigningRequestor{}

func NewSigningRequestor() *SigningRequestor {
	caKey, caCert, err := certificate.CreateCAKeyAndCertificate("CA", 24*365*10*time.Hour)
	Expect(err).ToNot(HaveOccurred())
	caDER, err := x509.CreateCertificate(rand.Reader, caCert, caCert, &caKey.PublicKey, caKey)
	Expect(err).ToNot(HaveOccurred())

	return &SigningRequestor{
		caKey:       caKey,
		caCert:      caCert,
		caPEM:       pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		issuedCh:    make(chan []string, 10),
		onUninstall: make(chan struct{}, 1),
	}
}

func (s *SigningRequestor) Issue(_ context.Context, name string, sanIPs []string, onSigned certificate.OnSignedFn) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, certificate.RSABitSize)
	if err != nil {
		return errors.Wrap(err, "failed to generate RSA key")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return errors.Wrap(err, "failed to generate serial number")
	}

	ipAddresses := []net.IP{}

	for _, ip := range sanIPs {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return errors.New("invalid IP address in SAN: " + ip)
		}

		ipAddresses = append(ipAddresses, parsed)
	}

	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   name,
			Organization: []string{"submariner.io"},
		},
		IPAddresses: ipAddresses,
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(10, 0, 0),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, cert, s.caCert, &privateKey.PublicKey, s.caKey)
	if err != nil {
		return err
	}

	s.issuedCh <- sanIPs

	certData := map[string][]byte{
		certificate.TLSDataKey:        pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		certificate.PrivateKeyDataKey: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)}),
		certificate.CADataKey:         s.caPEM,
	}

	return onSigned(certData)
}

func (s *SigningRequestor) Uninstall(_ context.Context) error {
	close(s.onUninstall)
	return nil
}

func (s *SigningRequestor) Remove(_ context.Context, name string) error {
	return nil
}

func (s *SigningRequestor) AwaitUninstall() {
	Eventually(s.onUninstall, 5).Should(BeClosed(), "Uninstall was not invoked")
}

func (s *SigningRequestor) AwaitIssued(ips []string) {
	Expect(s.issuedCh).To(Receive(ContainElements(ips)))
}
