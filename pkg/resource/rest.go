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

package resource

import (
	"context"
	"crypto/x509"
	"encoding/base64"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var NewDynamicClient = func(config *rest.Config) (dynamic.Interface, error) {
	c, err := dynamic.NewForConfig(config)
	return c, err //nolint:wrapcheck // No need to wrap
}

var NewSPDYExecutor = remotecommand.NewSPDYExecutor

func GetAuthorizedRestConfigFromData(ctx context.Context, apiServer, apiServerToken, caData string, tls *rest.TLSClientConfig,
	gvr schema.GroupVersionResource, namespace string,
) (*rest.Config, bool, error) {
	// Always load the CA when provided to avoid startup race conditions where the API server
	// may be temporarily unreachable, causing the no-CA attempt to fail with a non-x509 error.
	restConfig, err := BuildRestConfigFromData(apiServer, apiServerToken, caData, tls)
	if err != nil {
		return nil, false, err
	}

	authorized, err := IsAuthorizedFor(ctx, restConfig, gvr, namespace)

	return restConfig, authorized, err
}

func GetAuthorizedRestConfigFromFiles(ctx context.Context, apiServer, apiServerTokenFile, caFile string, tls *rest.TLSClientConfig,
	gvr schema.GroupVersionResource, namespace string,
) (*rest.Config, bool, error) {
	// Always load the CA when provided to avoid startup race conditions where the API server
	// may be temporarily unreachable, causing the no-CA attempt to fail with a non-x509 error.
	restConfig := BuildRestConfigFromFiles(apiServer, apiServerTokenFile, caFile, tls)
	authorized, err := IsAuthorizedFor(ctx, restConfig, gvr, namespace)

	return restConfig, authorized, err
}

func BuildRestConfigFromData(apiServer, apiServerToken, caData string, tls *rest.TLSClientConfig) (*rest.Config, error) {
	if tls == nil {
		tls = &rest.TLSClientConfig{}
	}

	if !tls.Insecure && caData != "" {
		caDecoded, err := base64.StdEncoding.DecodeString(caData)
		if err != nil {
			return nil, errors.Wrap(err, "error decoding CA data")
		}

		tls.CAData = caDecoded
	}

	return &rest.Config{
		Host:            "https://" + apiServer,
		TLSClientConfig: *tls,
		BearerToken:     apiServerToken,
	}, nil
}

func BuildRestConfigFromFiles(apiServer, apiServerTokenFile, caFile string, tls *rest.TLSClientConfig) *rest.Config {
	if tls == nil {
		tls = &rest.TLSClientConfig{}
	}

	if !tls.Insecure && caFile != "" {
		tls.CAFile = caFile
	}

	return &rest.Config{
		Host:            "https://" + apiServer,
		TLSClientConfig: *tls,
		BearerTokenFile: apiServerTokenFile,
	}
}

func IsAuthorizedFor(ctx context.Context, restConfig *rest.Config, gvr schema.GroupVersionResource, namespace string) (bool, error) {
	client, err := NewDynamicClient(restConfig)
	if err != nil {
		return false, errors.Wrap(err, "error creating dynamic client")
	}

	_, err = client.Resource(gvr).Namespace(namespace).Get(ctx, "any", metav1.GetOptions{})
	if IsUnknownAuthorityError(err) {
		return false, errors.Wrapf(err, "cannot access the API server %q", restConfig.Host)
	}

	if apierrors.IsNotFound(err) {
		err = nil
	}

	return true, err
}

func IsUnknownAuthorityError(err error) bool {
	return errors.As(err, &x509.UnknownAuthorityError{})
}
