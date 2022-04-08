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
	"fmt"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func GetAuthorizedRestConfigFromData(apiServer, apiServerToken, caData string, tls *rest.TLSClientConfig,
	gvr schema.GroupVersionResource, namespace string,
) (restConfig *rest.Config, authorized bool, err error) {
	// First try a REST config without the CA trust chain
	restConfig, err = BuildRestConfigFromData(apiServer, apiServerToken, "", tls)
	if err != nil {
		return
	}

	authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)
	if !authorized {
		// Now try with the trust chain
		restConfig, err = BuildRestConfigFromData(apiServer, apiServerToken, caData, tls)
		if err != nil {
			return
		}

		authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)
	}

	return
}

func GetAuthorizedRestConfigFromFiles(apiServer, apiServerTokenFile, caFile string, tls *rest.TLSClientConfig,
	gvr schema.GroupVersionResource, namespace string,
) (restConfig *rest.Config, authorized bool, err error) {
	// First try a REST config without the CA trust chain
	restConfig = BuildRestConfigFromFiles(apiServer, apiServerTokenFile, "", tls)
	authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)

	if !authorized {
		// Now try with the trust chain
		restConfig = BuildRestConfigFromFiles(apiServer, apiServerTokenFile, caFile, tls)
		authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)
	}

	return
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
		Host:            fmt.Sprintf("https://%s", apiServer),
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
		Host:            fmt.Sprintf("https://%s", apiServer),
		TLSClientConfig: *tls,
		BearerTokenFile: apiServerTokenFile,
	}
}

func IsAuthorizedFor(restConfig *rest.Config, gvr schema.GroupVersionResource, namespace string) (bool, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return false, errors.Wrap(err, "error creating dynamic client")
	}

	_, err = client.Resource(gvr).Namespace(namespace).Get(context.TODO(), "any", metav1.GetOptions{})
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
