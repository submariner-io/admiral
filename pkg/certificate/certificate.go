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

package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	"github.com/pkg/errors"
)

func CreateCAKeyAndCertificate(commonName string, validFor time.Duration) (*rsa.PrivateKey, *x509.Certificate, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, RSABitSize)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to generate RSA key")
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to generate serial number")
	}

	cert := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"submariner.io"},
		},
		NotBefore:             time.Now().Add(-5 * time.Minute),
		NotAfter:              time.Now().Add(validFor),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	return privateKey, cert, nil
}

func CreatePEMEncodedKeyAndCertificate(commonName string, validFor time.Duration) ([]byte, []byte, error) {
	privateKey, certTemplate, err := CreateCAKeyAndCertificate(commonName, validFor)
	if err != nil {
		return nil, nil, err
	}

	certDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create CA certificate")
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	return keyPEM, certPEM, nil
}

func CreatePEMEncodedKeyAndCertificateRequest(commonName string, ips []string) ([]byte, []byte, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, RSABitSize)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to generate RSA key")
	}

	ipAddresses := []net.IP{}

	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil {
			return nil, nil, errors.New("invalid IP address in SAN: " + ip)
		}

		ipAddresses = append(ipAddresses, parsed)
	}

	csrTemplate := x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"submariner.io"},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
		IPAddresses:        ipAddresses,
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &csrTemplate, privateKey)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create certificate request")
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	return keyPEM, csrPEM, nil
}

func ParseCertificateFromPEM(pemBytes []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.Errorf("failed to decode PEM")
	}

	return x509.ParseCertificate(block.Bytes) //nolint:wrapcheck // Let the caller wrap
}

func ParseCertificateRequestFromPEM(pemBytes []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.Errorf("failed to decode PEM")
	}

	return x509.ParseCertificateRequest(block.Bytes) //nolint:wrapcheck // Let the caller wrap
}

func ParsePKCS1PrivateKeyFromPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.Errorf("failed to decode PEM")
	}

	return x509.ParsePKCS1PrivateKey(block.Bytes) //nolint:wrapcheck // Let the caller wrap
}

func ExtractIPsFromCertificateRequestPEM(csrPEM []byte) ([]string, error) {
	csr, err := ParseCertificateRequestFromPEM(csrPEM)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse CSR")
	}

	ips := make([]string, 0, len(csr.IPAddresses))
	for _, ip := range csr.IPAddresses {
		ips = append(ips, ip.String())
	}

	return ips, nil
}
