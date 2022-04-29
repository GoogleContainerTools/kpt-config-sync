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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"kpt.dev/configsync/pkg/api/configmanagement"
)

// Prometheus metrics
var (
	APICallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Help:      "Distribution of durations of API server calls",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: "syncer",
			Name:      "api_duration_seconds",
			Buckets:   []float64{.001, .01, .1, 1},
		},
		// operation: create, patch, update, delete
		// type: resource kind
		// status: success, error
		[]string{"operation", "type", "status"},
	)
	ControllerRestarts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Help:      "Total restart count for the NamespaceConfig and ClusterConfig controllers",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: "syncer",
			Name:      "controller_restarts_total",
		},
		// source: sync, crd, retry
		[]string{"source"},
	)
	Operations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Help:      "Total operations that have been performed to sync resources to source of truth",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: "syncer",
			Name:      "operations_total",
		},
		// operation: create, update, delete
		// type: resource kind
		// status: success, error
		[]string{"operation", "type", "status"},
	)
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Help:      "Distribution of syncer reconciliation durations",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: "syncer",
			Name:      "reconcile_duration_seconds",
			Buckets:   []float64{.001, .01, .1, 1, 10, 100},
		},
		// type: cluster, crd, namespace, repo, sync
		// status: success, error
		[]string{"type", "status"},
	)
	ReconcileEventTimes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Help:      "Timestamps when syncer reconcile events occurred",
			Namespace: configmanagement.MetricsNamespace,
			Subsystem: "syncer",
			Name:      "reconcile_event_timestamps",
		},
		// type: cluster, crd, namespace, repo, sync
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(
		APICallDuration,
		ControllerRestarts,
		Operations,
		ReconcileDuration,
		ReconcileEventTimes,
	)
}

// StatusLabel returns a string representation of the given error appropriate for the status label
// of a Prometheus metric.
func StatusLabel(err error) string {
	if err == nil {
		return "success"
	}
	return "error"
}
