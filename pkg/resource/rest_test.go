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

package resource_test

import (
	"crypto/x509"
	"encoding/base64"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/submariner-io/admiral/pkg/fake"
	"github.com/submariner-io/admiral/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

const apiServer = "host"

var gvr = corev1.SchemeGroupVersion.WithResource("pods")

var _ = Describe("GetAuthorizedRestConfigFromData", func() {
	apiServerToken := base64.StdEncoding.EncodeToString([]byte("token"))
	caData := []byte("ca_data")
	caDataEncoded := base64.StdEncoding.EncodeToString(caData)

	d := newDynamicClientInfo()

	When("a CA trust chain is not needed", func() {
		It("should succeed when authorized", func() {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerToken).To(Equal(apiServerToken))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeTrue())
		})

		It("should fail when an API server error occurs", func() {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})
	})

	When("a CA trust chain is needed", func() {
		BeforeEach(func() {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)
		})

		It("should succeed when authorized", func() {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerToken).To(Equal(apiServerToken))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeFalse())
			Expect(restConfig.TLSClientConfig.CAData).To(Equal(caData))
		})

		It("should fail when not authorized", func() {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeFalse())
		})

		It("should fail when an API server error occurs", func() {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded, nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})

		It("should fail when the CA data is invalid", func() {
			_, _, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, "=@#$=%^", nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
		})
	})

	It("should fail when client creation fails", func() {
		resource.NewDynamicClient = func(config *rest.Config) (dynamic.Interface, error) {
			return nil, errors.New("error creating client")
		}

		_, _, err := resource.GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caDataEncoded, nil, gvr, "test-ns")
		Expect(err).ToNot(Succeed())
	})
})

var _ = Describe("GetAuthorizedRestConfigFromFiles", func() {
	caFile := "ca_data_file"
	apiServerTokenFile := "token_file"

	d := newDynamicClientInfo()

	When("a CA trust chain is not needed", func() {
		It("should succeed when authorized", func() {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile,
				&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerTokenFile).To(Equal(apiServerTokenFile))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeTrue())
		})

		It("should fail when an API server error occurs", func() {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile,
				nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})
	})

	When("a CA trust chain is needed", func() {
		BeforeEach(func() {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)
		})

		It("should succeed when authorized", func() {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerTokenFile).To(Equal(apiServerTokenFile))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeFalse())
			Expect(restConfig.TLSClientConfig.CAFile).To(Equal(caFile))
		})

		It("should fail when not authorized", func() {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeFalse())
		})

		It("should fail when an API server error occurs", func() {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile, nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})
	})
})

type dynamicClientSetup struct {
	clientWithCAData    *dynamicfake.FakeDynamicClient
	clientWithoutCAData *dynamicfake.FakeDynamicClient
}

func newDynamicClientInfo() *dynamicClientSetup {
	d := &dynamicClientSetup{}

	BeforeEach(func() {
		d.clientWithCAData = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)
		d.clientWithoutCAData = dynamicfake.NewSimpleDynamicClient(scheme.Scheme)

		resource.NewDynamicClient = func(config *rest.Config) (dynamic.Interface, error) {
			if len(config.TLSClientConfig.CAData) == 0 && config.TLSClientConfig.CAFile == "" {
				return d.clientWithoutCAData, nil
			}

			return d.clientWithCAData, nil
		}
	})

	return d
}
