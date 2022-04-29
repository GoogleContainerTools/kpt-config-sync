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

package importer

import (
	"github.com/prometheus/client_golang/prometheus"
	"kpt.dev/configsync/pkg/api/configmanagement"
)

// Name is the name of the importer Deployment.
const Name = "importer"

// Metrics contains the Prometheus metrics for the Importer.
var Metrics = struct {
	CycleDuration    *prometheus.HistogramVec
	NamespaceConfigs prometheus.Gauge
	Violations       prometheus.Counter
}{
	CycleDuration: prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Help:      "Distribution of durations of cycles that the importer has attempted to complete",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: Name,
			Name:      "cycle_duration_seconds",
		},
		// status: success, error
		[]string{"status"},
	),
	NamespaceConfigs: prometheus.NewGauge(
		prometheus.GaugeOpts{
			Help:      "Number of namespace configs present in current state",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: Name,
			Name:      "namespace_configs",
		},
	),
	Violations: prometheus.NewCounter(
		prometheus.CounterOpts{
			Help:      "Total number of safety violations that the importer has encountered.",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: Name,
			Name:      "violations_total",
		}),
}

func init() {
	prometheus.MustRegister(
		Metrics.CycleDuration,
		Metrics.NamespaceConfigs,
		Metrics.Violations,
	)
}
