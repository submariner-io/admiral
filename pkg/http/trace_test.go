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

package http_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	model "github.com/prometheus/client_model/go"
	"github.com/submariner-io/admiral/pkg/global"
	admiralhttp "github.com/submariner-io/admiral/pkg/http"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
)

var _ = Describe("HTTP Trace Metrics", func() {
	var (
		httpServer  *httptest.Server
		restConfig  *rest.Config
		httpClient  *http.Client
		testCounter int
		compName    string
	)

	BeforeEach(func() {
		testCounter++
		compName = fmt.Sprintf("test%d", testCounter)

		restConfig = &rest.Config{}
		httpServer = nil

		global.Init(&corev1.ConfigMap{
			Data: map[string]string{
				admiralhttp.EnableHTTPTraceLogging:  "true",
				admiralhttp.DisableHTTPTraceMetrics: "false",
			},
		})
	})

	JustBeforeEach(func() {
		if httpServer == nil {
			httpServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("test response"))
			}))
		}

		restConfig.Host = httpServer.URL

		admiralhttp.AddTraceMetrics(restConfig, compName)

		var err error
		httpClient, err = rest.HTTPClientFor(restConfig)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if httpServer != nil {
			httpServer.Close()
		}
	})

	doHttpGet := func(url string) {
		resp, err := httpClient.Get(url + "/test")
		Expect(err).NotTo(HaveOccurred())

		defer resp.Body.Close()

		_, _ = io.ReadAll(resp.Body)
	}

	It("should trace successful HTTP request", func() {
		doHttpGet(httpServer.URL)

		metrics := collectMetrics(compName)
		Expect(findMetric(metrics, admiralhttp.ConnectionsTotalMetric)).To(Equal(1))
		Expect(findMetric(metrics, admiralhttp.FirstResponseByteDurationSecondsMetric)).To(Equal(1))
		Expect(findMetric(metrics, admiralhttp.ConnectionEstablishDurationSecondsMetric, "status", "success")).To(Equal(1))
	})

	It("should record connection reuse metrics", func() {
		// Make first request
		doHttpGet(httpServer.URL)

		// Make second request (should reuse connection)
		doHttpGet(httpServer.URL)

		metrics := collectMetrics(compName)
		Expect(findMetric(metrics, admiralhttp.ConnectionsTotalMetric, "reused", "true")).To(Equal(1))
		Expect(findMetric(metrics, admiralhttp.ConnectionsTotalMetric, "reused", "false")).To(Equal(1))
	})

	It("should trace DNS lookup", func() {
		// Replace the IP-based URL with hostname-based URL
		doHttpGet(strings.Replace(httpServer.URL, "127.0.0.1", "localhost", 1))

		metrics := collectMetrics(compName)
		Expect(findMetric(metrics, admiralhttp.DNSLookupDurationSecondsMetric)).To(Equal(1))
	})

	It("should record connection errors", func() {
		// Try to connect to a port that's not listening
		_, err := httpClient.Get("http://127.0.0.1:1/test")
		Expect(err).To(HaveOccurred())

		metrics := collectMetrics(compName)
		Expect(findMetric(metrics, admiralhttp.ConnectionErrorsTotalMetric, "error_type", "connect")).To(Equal(1))
		Expect(findMetric(metrics, admiralhttp.ConnectionEstablishDurationSecondsMetric, "status", "error")).To(Equal(1))
	})

	Context("with TLS", func() {
		BeforeEach(func() {
			httpServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("tls test response"))
			}))

			certPool := x509.NewCertPool()
			certPool.AddCert(httpServer.Certificate())

			restConfig.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:    certPool,
					MinVersion: tls.VersionTLS12,
				},
			}
		})

		It("should trace TLS handshake", func() {
			doHttpGet(httpServer.URL)

			metrics := collectMetrics(compName)
			Expect(findMetric(metrics, admiralhttp.TLSHandshakeDurationSecondsMetric)).To(Equal(1))
			Expect(findMetric(metrics, admiralhttp.HTTPProtocolTotalMetric)).To(Equal(1))
		})

		Context("and no certificate", func() {
			BeforeEach(func() {
				// Don't add the server's certificate to trust store - this will cause TLS error
				restConfig.Transport = &http.Transport{
					TLSClientConfig: &tls.Config{
						InsecureSkipVerify: false, // Must verify, but we won't provide cert
						MinVersion:         tls.VersionTLS12,
					},
				}
			})

			It("should record TLS handshake error", func() {
				_, err := httpClient.Get(httpServer.URL + "/test")
				Expect(err).To(HaveOccurred())

				metrics := collectMetrics(compName)
				Expect(findMetric(metrics, admiralhttp.ConnectionErrorsTotalMetric, "error_type", "tls")).To(Equal(1))
			})
		})
	})

	Context("with metrics disabled", func() {
		BeforeEach(func() {
			global.Init(&corev1.ConfigMap{
				Data: map[string]string{
					admiralhttp.DisableHTTPTraceMetrics: "true",
				},
			})
		})

		It("should not record any metrics", func() {
			doHttpGet(httpServer.URL)

			Expect(collectMetrics(compName)).To(BeEmpty())
		})
	})
})

func collectMetrics(compName string) map[string][]*model.Metric {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	Expect(err).NotTo(HaveOccurred())

	metricNameStr := fmt.Sprintf("_%s_", compName)

	metrics := map[string][]*model.Metric{}

	for _, mf := range metricFamilies {
		if !strings.Contains(mf.GetName(), metricNameStr) {
			continue
		}

		parts := strings.Split(mf.GetName(), metricNameStr)
		metricName := parts[1]

		metrics[metricName] = append(metrics[metricName], mf.GetMetric()...)
	}

	return metrics
}

func findMetric(metrics map[string][]*model.Metric, name string, labels ...string) int {
	for _, m := range metrics[name] {
		matches := true
		for i := 0; i < len(labels); i += 2 {
			matches = matches && slices.ContainsFunc(m.GetLabel(), func(pair *model.LabelPair) bool {
				return labels[i] == pair.GetName() && labels[i+1] == pair.GetValue()
			})
		}

		if matches {
			if m.Counter != nil {
				return int(m.Counter.GetValue())
			} else if m.Gauge != nil {
				return int(m.Gauge.GetValue())
			} else if m.Histogram != nil {
				// Use sample count as the value - this is number of observations
				return int(float64(m.Histogram.GetSampleCount()))
			}
		}
	}

	Fail(fmt.Sprintf("Metric not found: %s, %v", name, labels))

	return 0
}
