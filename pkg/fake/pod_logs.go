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
	"fmt"
	"io"
	"net/http"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	fakerest "k8s.io/client-go/rest/fake"
)

// WrapClientWithPodLogs wraps a fake Kubernetes client to return custom pod logs.
func WrapClientWithPodLogs(client kubernetes.Interface, logs map[string]string) kubernetes.Interface {
	return &clientWithPodLogs{
		Interface: client,
		logs:      logs,
	}
}

type clientWithPodLogs struct {
	kubernetes.Interface
	logs map[string]string
}

func (c *clientWithPodLogs) CoreV1() typedcorev1.CoreV1Interface {
	return &coreV1WithPodLogs{
		CoreV1Interface: c.Interface.CoreV1(),
		logs:            c.logs,
	}
}

type coreV1WithPodLogs struct {
	typedcorev1.CoreV1Interface
	logs map[string]string
}

func (c *coreV1WithPodLogs) Pods(namespace string) typedcorev1.PodInterface {
	return &podsWithCustomLogs{
		PodInterface: c.CoreV1Interface.Pods(namespace),
		logs:         c.logs,
		namespace:    namespace,
	}
}

type podsWithCustomLogs struct {
	typedcorev1.PodInterface
	logs      map[string]string
	namespace string
}

func (p *podsWithCustomLogs) GetLogs(name string, opts *v1.PodLogOptions) *restclient.Request {
	fakeClient := &fakerest.RESTClient{
		Client: fakerest.CreateHTTPClient(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(p.logs[name])),
			}, nil
		}),
		NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		GroupVersion:         v1.SchemeGroupVersion,
		VersionedAPIPath:     fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log", p.namespace, name),
	}

	return fakeClient.Request()
}
