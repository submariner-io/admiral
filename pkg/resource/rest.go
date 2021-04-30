/*
© 2021 Red Hat, Inc.

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
	"net/url"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

func GetAuthorizedRestConfig(apiServer, apiServerToken, caData string, tls rest.TLSClientConfig,
	gvr schema.GroupVersionResource, namespace string) (restConfig *rest.Config, authorized bool, err error) {
	// First try a REST config without the CA trust chain
	restConfig, err = BuildRestConfig(apiServer, apiServerToken, "", tls)
	if err != nil {
		return
	}

	authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)
	if !authorized {
		// Now try with the trust chain
		restConfig, err = BuildRestConfig(apiServer, apiServerToken, caData, tls)
		if err != nil {
			return
		}

		authorized, err = IsAuthorizedFor(restConfig, gvr, namespace)
	}

	return
}

func BuildRestConfig(apiServer, apiServerToken, caData string, tls rest.TLSClientConfig) (*rest.Config, error) {
	if !tls.Insecure && caData != "" {
		caDecoded, err := base64.StdEncoding.DecodeString(caData)
		if err != nil {
			return nil, fmt.Errorf("error decoding CA data: %v", err)
		}

		tls.CAData = caDecoded
	}

	return &rest.Config{
		Host:            fmt.Sprintf("https://%s", apiServer),
		TLSClientConfig: tls,
		BearerToken:     apiServerToken,
	}, nil
}

func IsAuthorizedFor(restConfig *rest.Config, gvr schema.GroupVersionResource, namespace string) (bool, error) {
	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return false, err
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
	if urlError, ok := err.(*url.Error); ok {
		if _, ok := urlError.Unwrap().(x509.UnknownAuthorityError); ok {
			return true
		}
	}

	return false
}
