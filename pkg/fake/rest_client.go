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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	restfake "k8s.io/client-go/rest/fake"
)

type fakeCoreV1 struct {
	typedcorev1.CoreV1Interface
	fakeRESTClient restfake.RESTClient
}
type fakeWithRESTClient struct {
	*k8sfake.Clientset
	coreV1 *fakeCoreV1
}

func WithRESTClient(fakeK8sClient *k8sfake.Clientset, fakeRESTClient *restfake.RESTClient) kubernetes.Interface {
	if fakeRESTClient == nil {
		fakeRESTClient = &restfake.RESTClient{}
	}

	coreV1 := &fakeCoreV1{
		CoreV1Interface: fakeK8sClient.CoreV1(),
		fakeRESTClient:  *fakeRESTClient,
	}

	if coreV1.fakeRESTClient.NegotiatedSerializer == nil {
		coreV1.fakeRESTClient.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	}

	if coreV1.fakeRESTClient.GroupVersion.String() == "" {
		coreV1.fakeRESTClient.GroupVersion = corev1.SchemeGroupVersion
	}

	return &fakeWithRESTClient{Clientset: fakeK8sClient, coreV1: coreV1}
}

func (c *fakeWithRESTClient) CoreV1() typedcorev1.CoreV1Interface {
	return c.coreV1
}

func (c *fakeCoreV1) RESTClient() rest.Interface {
	return &c.fakeRESTClient
}
