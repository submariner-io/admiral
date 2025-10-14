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

	. "github.com/onsi/gomega"
	"github.com/submariner-io/admiral/pkg/certificate"
)

type SigningRequestor struct {
	issuedCh    chan []string
	onUninstall chan struct{}
}

var _ certificate.SigningRequestor = &SigningRequestor{}

func NewSigningRequestor() *SigningRequestor {
	return &SigningRequestor{
		issuedCh:    make(chan []string, 10),
		onUninstall: make(chan struct{}, 1),
	}
}

func (s *SigningRequestor) Issue(_ context.Context, _ string, sanIPs []string, onSigned certificate.OnSignedFn) error {
	s.issuedCh <- sanIPs

	certData := map[string][]byte{
		certificate.TLSDataKey:        []byte("mock-tls-cert"),
		certificate.PrivateKeyDataKey: []byte("mock-tls-key"),
		certificate.CADataKey:         []byte("mock-ca-cert"),
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
