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
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

	When("TLS is insecure", func() {
		It("should succeed when authorized", func(ctx context.Context) {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerToken).To(Equal(apiServerToken))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeTrue())
		})

		It("should fail when an API server error occurs", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})
	})

	When("CA data is provided", func() {
		It("should always load the CA and succeed when authorized", func(ctx context.Context) {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerToken).To(Equal(apiServerToken))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeFalse())
			Expect(restConfig.TLSClientConfig.CAData).To(Equal(caData))
		})

		It("should fail when not authorized", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeFalse())
		})

		It("should fail when an API server error occurs", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded, nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})

		It("should fail when the CA data is invalid", func(ctx context.Context) {
			_, _, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, "=@#$=%^", nil, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
		})
	})

	It("should fail when client creation fails", func(ctx context.Context) {
		resource.NewDynamicClient = func(_ *rest.Config) (dynamic.Interface, error) {
			return nil, errors.New("error creating client")
		}

		_, _, err := resource.GetAuthorizedRestConfigFromData(ctx, apiServer, apiServerToken, caDataEncoded, nil, gvr, "test-ns")
		Expect(err).ToNot(Succeed())
	})
})

var _ = Describe("GetAuthorizedRestConfigFromFiles", func() {
	caFile := "ca_data_file"
	apiServerTokenFile := "token_file"

	d := newDynamicClientInfo()

	testInsecureTLS := func(ctx context.Context) (*rest.Config, bool, error) {
		return resource.GetAuthorizedRestConfigFromFiles(ctx, apiServer, apiServerTokenFile, caFile,
			&rest.TLSClientConfig{Insecure: true}, gvr, "test-ns")
	}

	When("TLS is insecure", func() {
		It("should succeed when authorized", func(ctx context.Context) {
			restConfig, authorized, err := testInsecureTLS(ctx)
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerTokenFile).To(Equal(apiServerTokenFile))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeTrue())
		})

		It("should fail when an API server error occurs", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithoutCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := testInsecureTLS(ctx)
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeTrue())
		})
	})

	When("CA file is provided", func() {
		It("should always load the CA and succeed when authorized", func(ctx context.Context) {
			restConfig, authorized, err := resource.GetAuthorizedRestConfigFromFiles(ctx, apiServer, apiServerTokenFile, caFile,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).To(Succeed())
			Expect(authorized).To(BeTrue())
			Expect(restConfig.Host).To(ContainSubstring(apiServer))
			Expect(restConfig.BearerTokenFile).To(Equal(apiServerTokenFile))
			Expect(restConfig.TLSClientConfig.Insecure).To(BeFalse())
			Expect(restConfig.TLSClientConfig.CAFile).To(Equal(caFile))
		})

		It("should fail when not authorized", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", x509.UnknownAuthorityError{}, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromFiles(ctx, apiServer, apiServerTokenFile, caFile,
				&rest.TLSClientConfig{Insecure: false}, gvr, "test-ns")
			Expect(err).ToNot(Succeed())
			Expect(authorized).To(BeFalse())
		})

		It("should fail when an API server error occurs", func(ctx context.Context) {
			fake.FailOnAction(&d.clientWithCAData.Fake, gvr.Resource, "*", nil, false)

			_, authorized, err := resource.GetAuthorizedRestConfigFromFiles(ctx, apiServer, apiServerTokenFile, caFile, nil, gvr, "test-ns")
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
