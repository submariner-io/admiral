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

package syncer

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricsConfig configures optional Prometheus metrics for syncer operations.
// For each metric, either pass Opts to let the syncer create and register it,
// or pass a pre-created collector to share it across syncers.
//
// When a MetricsConfig is shared between multiple syncers (e.g. the local and
// remote syncers created by broker/syncer.go), callers should pre-resolve
// once and pass the live collectors.
type MetricsConfig struct {
	// SyncCounterOpts if specified, used to create a gauge to record sync counter metrics.
	// Alternatively the gauge can be created directly and passed via SyncCounter,
	// in which case SyncCounterOpts is ignored.
	SyncCounterOpts *prometheus.GaugeOpts

	// SyncCounter if specified, used to record counter metrics.
	SyncCounter *prometheus.GaugeVec

	// Federation duration in ms (Distribute/Delete).
	FederationDurationMsOpts *prometheus.HistogramOpts
	FederationDurationMs     *prometheus.HistogramVec

	// Transform function duration in ms.
	TransformDurationMsOpts *prometheus.HistogramOpts
	TransformDurationMs     *prometheus.HistogramVec

	// Time items spend waiting in the work queue, in ms.
	QueueWaitDurationMsOpts *prometheus.HistogramOpts
	QueueWaitDurationMs     *prometheus.HistogramVec

	// Queue depth sampled on each enqueue/dequeue. Histogram captures the
	// distribution so you can query max via histogram_quantile(1.0, ...).
	QueueLengthOpts *prometheus.HistogramOpts
	QueueLength     *prometheus.HistogramVec
}

var (
	defaultDurationMsBuckets  = []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}
	defaultQueueLengthBuckets = []float64{0, 1, 2, 3, 5, 8, 10, 15, 20, 50, 100, 500}
)

type syncerMetrics struct {
	MetricsConfig
	enqueueTimes sync.Map
}

// resolveHistogram creates and registers a HistogramVec from opts if the collector is nil.
// Returns the existing or newly created collector, and nils out the opts pointer field on creation.
func resolveHistogram(collector **prometheus.HistogramVec, opts **prometheus.HistogramOpts,
	defaultBuckets []float64, labels []string,
) {
	if *collector != nil || *opts == nil {
		return
	}

	o := **opts
	if o.Buckets == nil {
		o.Buckets = defaultBuckets
	}

	*collector = prometheus.NewHistogramVec(o, labels)
	prometheus.MustRegister(*collector)

	*opts = nil
}

// ResolveMetricsConfig pre-resolves any Opts-based fields in the MetricsConfig
// into live HistogramVec collectors. The returned config has live collectors set
// and Opts fields niled out. This is useful when sharing a MetricsConfig between
// multiple syncers (e.g. broker local and remote syncers) to avoid duplicate
// registration.
//
//nolint:gocritic // hugeParam: this function is used in setup so copying MetricsConfig is negligible.
func ResolveMetricsConfig(cfg MetricsConfig) MetricsConfig {
	resolved := cfg

	metricLabels := []string{DirectionLabel, OperationLabel, SyncerNameLabel}
	queueLabels := []string{SyncerNameLabel}

	if resolved.SyncCounter == nil && resolved.SyncCounterOpts != nil {
		resolved.SyncCounter = prometheus.NewGaugeVec(
			*resolved.SyncCounterOpts,
			metricLabels,
		)
		prometheus.MustRegister(resolved.SyncCounter)
		resolved.SyncCounterOpts = nil
	}

	resolveHistogram(&resolved.FederationDurationMs, &resolved.FederationDurationMsOpts, defaultDurationMsBuckets, metricLabels)
	resolveHistogram(&resolved.TransformDurationMs, &resolved.TransformDurationMsOpts, defaultDurationMsBuckets, metricLabels)
	resolveHistogram(&resolved.QueueWaitDurationMs, &resolved.QueueWaitDurationMsOpts, defaultDurationMsBuckets, queueLabels)
	resolveHistogram(&resolved.QueueLength, &resolved.QueueLengthOpts, defaultQueueLengthBuckets, queueLabels)

	return resolved
}

//nolint:gocritic // hugeParam: this function is used in setup so copying MetricsConfig is negligible.
func newSyncerMetrics(cfg MetricsConfig) *syncerMetrics {
	resolved := ResolveMetricsConfig(cfg)
	markOwnership(&resolved, &cfg)

	return &syncerMetrics{
		MetricsConfig: resolved,
	}
}

// markOwnership resets Opts fields on the resolved config for collectors that
// we created ourselves using these fields so we know to unregister them later.
func markOwnership(resolved, original *MetricsConfig) {
	if original.SyncCounterOpts != nil && original.SyncCounter == nil {
		resolved.SyncCounterOpts = original.SyncCounterOpts
	}

	if original.FederationDurationMsOpts != nil && original.FederationDurationMs == nil {
		resolved.FederationDurationMsOpts = original.FederationDurationMsOpts
	}

	if original.TransformDurationMsOpts != nil && original.TransformDurationMs == nil {
		resolved.TransformDurationMsOpts = original.TransformDurationMsOpts
	}

	if original.QueueWaitDurationMsOpts != nil && original.QueueWaitDurationMs == nil {
		resolved.QueueWaitDurationMsOpts = original.QueueWaitDurationMsOpts
	}

	if original.QueueLengthOpts != nil && original.QueueLength == nil {
		resolved.QueueLengthOpts = original.QueueLengthOpts
	}
}

func (m *syncerMetrics) unregister() {
	if m.SyncCounterOpts != nil {
		prometheus.Unregister(m.SyncCounter)
	}

	if m.FederationDurationMsOpts != nil {
		prometheus.Unregister(m.FederationDurationMs)
	}

	if m.TransformDurationMsOpts != nil {
		prometheus.Unregister(m.TransformDurationMs)
	}

	if m.QueueWaitDurationMsOpts != nil {
		prometheus.Unregister(m.QueueWaitDurationMs)
	}

	if m.QueueLengthOpts != nil {
		prometheus.Unregister(m.QueueLength)
	}
}

func (m *syncerMetrics) incSyncCounter(direction SyncDirection, op Operation, syncerName string) {
	if m.SyncCounter == nil {
		return
	}

	m.SyncCounter.With(prometheus.Labels{
		DirectionLabel:  direction.String(),
		OperationLabel:  op.String(),
		SyncerNameLabel: syncerName,
	}).Inc()
}

func (m *syncerMetrics) observeFederationDurationMs(direction SyncDirection, op Operation, syncerName string, duration time.Duration) {
	if m.FederationDurationMs == nil {
		return
	}

	m.FederationDurationMs.With(prometheus.Labels{
		DirectionLabel:  direction.String(),
		OperationLabel:  op.String(),
		SyncerNameLabel: syncerName,
	}).Observe(float64(duration.Milliseconds()))
}

func (m *syncerMetrics) observeTransformDurationMs(direction SyncDirection, op Operation, syncerName string, duration time.Duration) {
	if m.TransformDurationMs == nil {
		return
	}

	m.TransformDurationMs.With(prometheus.Labels{
		DirectionLabel:  direction.String(),
		OperationLabel:  op.String(),
		SyncerNameLabel: syncerName,
	}).Observe(float64(duration.Milliseconds()))
}

func (m *syncerMetrics) observeQueueWaitDurationMs(syncerName string, duration time.Duration) {
	if m.QueueWaitDurationMs == nil {
		return
	}

	m.QueueWaitDurationMs.With(prometheus.Labels{
		SyncerNameLabel: syncerName,
	}).Observe(float64(duration.Milliseconds()))
}

func (m *syncerMetrics) observeQueueLength(syncerName string, length int) {
	if m.QueueLength == nil {
		return
	}

	m.QueueLength.With(prometheus.Labels{
		SyncerNameLabel: syncerName,
	}).Observe(float64(length))
}

func (m *syncerMetrics) trackEnqueue(key string) {
	if m.QueueWaitDurationMs == nil {
		return
	}

	m.enqueueTimes.Store(key, time.Now())
}

func (m *syncerMetrics) recordDequeue(syncerName, key string) {
	if m.QueueWaitDurationMs == nil {
		return
	}

	if enqueueTime, ok := m.enqueueTimes.LoadAndDelete(key); ok {
		m.observeQueueWaitDurationMs(syncerName, time.Since(enqueueTime.(time.Time)))
	}
}
