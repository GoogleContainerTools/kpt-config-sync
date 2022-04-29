// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package state

import (
	"github.com/prometheus/client_golang/prometheus"
	"kpt.dev/configsync/pkg/api/configmanagement"
)

// MonitorName is the name of the monitor Deployment.
const MonitorName = "monitor"

// Metrics contains the Prometheus metrics for the monitor state.
var metrics = struct {
	Configs     *prometheus.GaugeVec
	Errors      *prometheus.GaugeVec
	LastImport  prometheus.Gauge
	LastSync    prometheus.Gauge
	SyncLatency prometheus.Histogram
}{
	prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Help:      "Current number of configs (cluster and namespace) grouped by their sync status",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: MonitorName,
			Name:      "configs",
		},
		// status: synced, stale, error
		[]string{"status"},
	),
	prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Help:      "Current number of errors in the config repo, grouped by the component where they occurred",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: MonitorName,
			Name:      "errors",
		},
		// component: source, importer, syncer
		[]string{"component"},
	),
	prometheus.NewGauge(
		prometheus.GaugeOpts{
			Help:      "Timestamp of the most recent import",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: MonitorName,
			Name:      "last_import_timestamp",
		},
	),
	prometheus.NewGauge(
		prometheus.GaugeOpts{
			Help:      "Timestamp of the most recent sync",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: MonitorName,
			Name:      "last_sync_timestamp",
		},
	),
	prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Help:      "Distribution of the latencies between importing and syncing each config",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: MonitorName,
			Name:      "sync_latency_seconds",
			Buckets:   prometheus.DefBuckets,
		},
	),
}

func init() {
	prometheus.MustRegister(
		metrics.Configs,
		metrics.Errors,
		metrics.LastImport,
		metrics.LastSync,
		metrics.SyncLatency,
	)
}
