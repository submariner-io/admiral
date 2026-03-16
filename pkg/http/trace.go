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

package http

import (
	"crypto/tls"
	nethttp "net/http"
	"net/http/httptrace"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/submariner-io/admiral/pkg/global"
	"github.com/submariner-io/admiral/pkg/log"
	"k8s.io/client-go/rest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	metricsNamespace = "k8s"

	// DisableHTTPTraceMetrics is the global flag to disable HTTP trace metrics collection.
	DisableHTTPTraceMetrics = "disable-http-trace-metrics"

	// EnableHTTPTraceLogging is the global flag to enable HTTP trace logging.
	EnableHTTPTraceLogging = "enable-http-trace-logging"

	// Metric name constants for HTTP trace metrics.

	// ConnectionsTotalMetric is the name for the total connections counter metric.
	ConnectionsTotalMetric = "connections_total"

	// ConnectionIdleSecondsMetric is the name for the connection idle time histogram metric.
	ConnectionIdleSecondsMetric = "connection_idle_seconds"

	// ConnectionErrorsTotalMetric is the name for the connection errors counter metric.
	ConnectionErrorsTotalMetric = "connection_errors_total"

	// TLSHandshakeDurationSecondsMetric is the name for the TLS handshake duration histogram metric.
	TLSHandshakeDurationSecondsMetric = "tls_handshake_duration_seconds"

	// ConnectionEstablishDurationSecondsMetric is the name for the connection establishment duration histogram metric.
	ConnectionEstablishDurationSecondsMetric = "connection_establish_duration_seconds"

	// DNSLookupDurationSecondsMetric is the name for the DNS lookup duration histogram metric.
	DNSLookupDurationSecondsMetric = "dns_lookup_duration_seconds"

	// FirstResponseByteDurationSecondsMetric is the name for the time to first response byte histogram metric.
	FirstResponseByteDurationSecondsMetric = "first_response_byte_duration_seconds"

	// HTTPProtocolTotalMetric is the name for the HTTP protocol versions counter metric.
	HTTPProtocolTotalMetric = "http_protocol_total"
)

type httpTraceRoundTripper struct {
	wrapped                        nethttp.RoundTripper
	enableLogging                  bool
	logger                         log.Logger
	k8sConnectionsTotal            *prometheus.CounterVec
	k8sConnectionIdleTime          *prometheus.HistogramVec
	k8sConnectionErrors            *prometheus.CounterVec
	k8sTLSHandshakeDuration        *prometheus.HistogramVec
	k8sConnectionEstablishDuration *prometheus.HistogramVec
	k8sDNSLookupDuration           *prometheus.HistogramVec
	k8sFirstResponseByteDuration   *prometheus.HistogramVec
	k8sHTTPProtocol                *prometheus.CounterVec
}

func AddTraceMetrics(restConfig *rest.Config, compName string) {
	if global.Get(DisableHTTPTraceMetrics, false) {
		logger.Info("HTTP connection tracing and metrics disabled")
		return
	}

	rt := &httpTraceRoundTripper{
		enableLogging: global.Get(EnableHTTPTraceLogging, false),
		logger:        log.Logger{Logger: logf.Log.WithName("http-trace")},
	}

	rt.k8sConnectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      ConnectionsTotalMetric,
			Help:      "Total number of HTTP connections to Kubernetes API server",
		},
		[]string{"host", "reused"},
	)
	prometheus.MustRegister(rt.k8sConnectionsTotal)

	rt.k8sConnectionIdleTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      ConnectionIdleSecondsMetric,
			Help:      "Time connections spent idle before reuse",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
		},
		[]string{"host"},
	)
	prometheus.MustRegister(rt.k8sConnectionIdleTime)

	rt.k8sConnectionErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      ConnectionErrorsTotalMetric,
			Help:      "Total number of Kubernetes API connection errors",
		},
		[]string{"host", "error_type"},
	)
	prometheus.MustRegister(rt.k8sConnectionErrors)

	rt.k8sTLSHandshakeDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      TLSHandshakeDurationSecondsMetric,
			Help:      "Duration of TLS handshakes with Kubernetes API server",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
		},
		[]string{"host"},
	)
	prometheus.MustRegister(rt.k8sTLSHandshakeDuration)

	rt.k8sConnectionEstablishDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      ConnectionEstablishDurationSecondsMetric,
			Help:      "Duration to establish TCP connection to Kubernetes API server",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
		},
		[]string{"host", "network", "status"},
	)
	prometheus.MustRegister(rt.k8sConnectionEstablishDuration)

	rt.k8sDNSLookupDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      DNSLookupDurationSecondsMetric,
			Help:      "Duration of DNS lookups for Kubernetes API server",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12), // 1ms to ~4s
		},
		[]string{"host"},
	)
	prometheus.MustRegister(rt.k8sDNSLookupDuration)

	rt.k8sFirstResponseByteDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      FirstResponseByteDurationSecondsMetric,
			Help:      "Time to first byte from Kubernetes API server",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms to ~16s
		},
		[]string{"host", "method"},
	)
	prometheus.MustRegister(rt.k8sFirstResponseByteDuration)

	rt.k8sHTTPProtocol = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Subsystem: compName,
			Name:      HTTPProtocolTotalMetric,
			Help:      "HTTP protocol versions negotiated with Kubernetes API server",
		},
		[]string{"host", "protocol"},
	)
	prometheus.MustRegister(rt.k8sHTTPProtocol)

	restConfig.Wrap(func(wrapped nethttp.RoundTripper) nethttp.RoundTripper {
		rt.wrapped = wrapped
		return rt
	})

	logger.Info("HTTP connection tracing and metrics enabled")
}

//nolint:gocognit,gocyclo // High complexity is inherent to httptrace.ClientTrace callback structure
func (h *httpTraceRoundTripper) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	// Extract host for metrics labels
	host := req.URL.Host
	if host == "" {
		host = "unknown"
	}

	// Track timing for various phases
	var (
		dnsStart     time.Time
		connectStart time.Time
		tlsStart     time.Time
		requestStart = time.Now()
	)

	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()

			if h.enableLogging {
				h.logger.Infof("DNS lookup starting for %s", info.Host)
			}
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			if !dnsStart.IsZero() {
				duration := time.Since(dnsStart)
				h.k8sDNSLookupDuration.WithLabelValues(host).Observe(duration.Seconds())

				if h.enableLogging {
					if info.Err != nil {
						h.logger.Errorf(info.Err, "DNS lookup failed for %s (took %v)", host, duration)
					} else {
						h.logger.Infof("DNS lookup completed for %s: %v addresses (took %v)", host, len(info.Addrs), duration)
					}
				}
			}

			if info.Err != nil {
				h.k8sConnectionErrors.WithLabelValues(host, "dns").Inc()
			}
		},
		GetConn: func(hostPort string) {
			if h.enableLogging {
				h.logger.Infof("Getting connection to %s", hostPort)
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			reused := "false"
			if info.Reused {
				reused = "true"
			}

			h.k8sConnectionsTotal.WithLabelValues(host, reused).Inc()

			if info.WasIdle && info.IdleTime > 0 {
				h.k8sConnectionIdleTime.WithLabelValues(host).Observe(info.IdleTime.Seconds())
			}

			if h.enableLogging {
				h.logger.Infof("Got connection to %s: reused=%v, idle=%v, idleTime=%v",
					host, info.Reused, info.WasIdle, info.IdleTime)
			}
		},
		PutIdleConn: func(err error) {
			if err != nil {
				h.k8sConnectionErrors.WithLabelValues(host, "idle_pool").Inc()

				if h.enableLogging {
					h.logger.Errorf(err, "Failed to return connection to %s to idle pool", host)
				}
			} else if h.enableLogging {
				h.logger.Infof("Connection to %s returned to idle pool", host)
			}
		},
		ConnectStart: func(network, addr string) {
			connectStart = time.Now()

			if h.enableLogging {
				h.logger.Infof("Connecting to %s (%s)", addr, network)
			}
		},
		ConnectDone: func(network, addr string, err error) {
			if !connectStart.IsZero() {
				duration := time.Since(connectStart)

				status := "success"
				if err != nil {
					status = "error"
				}

				h.k8sConnectionEstablishDuration.WithLabelValues(host, network, status).Observe(duration.Seconds())

				if h.enableLogging {
					if err != nil {
						h.logger.Errorf(err, "Connection to %s (%s) failed (took %v)", addr, network, duration)
					} else {
						h.logger.Infof("Connected to %s (%s) (took %v)", addr, network, duration)
					}
				}
			}

			if err != nil {
				h.k8sConnectionErrors.WithLabelValues(host, "connect").Inc()
			}
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()

			if h.enableLogging {
				h.logger.Infof("TLS handshake starting for %s", host)
			}
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			if !tlsStart.IsZero() {
				duration := time.Since(tlsStart)
				if err == nil {
					h.k8sTLSHandshakeDuration.WithLabelValues(host).Observe(duration.Seconds())

					protocol := state.NegotiatedProtocol
					if protocol == "" {
						protocol = "http/1.1" // Default if not set
					}

					h.k8sHTTPProtocol.WithLabelValues(host, protocol).Inc()

					if h.enableLogging {
						h.logger.Infof("TLS handshake to %s complete: version=%#x, protocol=%s (took %v)",
							host, state.Version, protocol, duration)
					}
				} else if h.enableLogging {
					h.logger.Errorf(err, "TLS handshake to %s failed (took %v)", host, duration)
				}
			}

			if err != nil {
				h.k8sConnectionErrors.WithLabelValues(host, "tls").Inc()
			}
		},
		GotFirstResponseByte: func() {
			duration := time.Since(requestStart)
			h.k8sFirstResponseByteDuration.WithLabelValues(host, req.Method).Observe(duration.Seconds())

			if h.enableLogging {
				h.logger.Infof("Received first response byte from %s after %v", host, duration)
			}
		},
	}

	return h.wrapped.RoundTrip(req.WithContext(httptrace.WithClientTrace(req.Context(), trace))) //nolint:wrapcheck // No need to wrap
}
