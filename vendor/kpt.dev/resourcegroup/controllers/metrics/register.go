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
	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/stats/view"
)

// RegisterOCAgentExporter creates the OC Agent metrics exporter.
func RegisterOCAgentExporter() (*ocagent.Exporter, error) {
	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	view.RegisterExporter(oce)
	return oce, nil
}

// RegisterReconcilerMetricsViews registers the views so that recorded metrics can be exported in the reconcilers.
func RegisterReconcilerMetricsViews() error {
	return view.Register(
		ReconcileDurationView,
		ResourceGroupTotalView,
		ResourceCountView,
		ReadyResourceCountView,
		NamespaceCountView,
		ClusterScopedResourceCountView,
		CRDCountView,
		KCCResourceCountView,
		PipelineErrorView,
	)
}
