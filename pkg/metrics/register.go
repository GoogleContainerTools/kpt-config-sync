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
	"fmt"
	"os"

	"contrib.go.opencensus.io/exporter/ocagent"
	"go.opencensus.io/stats/view"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// RegisterOCAgentExporter creates the OC Agent metrics exporter.
func RegisterOCAgentExporter() (*ocagent.Exporter, error) {
	// Update OC_RESOURCE_LABELS defined in go.opencensus.io/resource/resource.go
	// So that each OC agent will have corresponding resource labels
	// Adding pod name and namespace name can have metrics identified as container_pod
	// Cluster name & cluster location & project name are attached automatically
	reconcilerName := os.Getenv(reconcilermanager.ReconcilerNameKey)
	namespace := os.Getenv(reconcilermanager.NamespaceNameKey)
	err := os.Setenv("OC_RESOURCE_LABELS", fmt.Sprintf("k8s.namespace.name=%q,k8s.pod.name=%q", namespace, reconcilerName))
	if err != nil {
		return nil, err
	}
	oce, err := ocagent.NewExporter(
		ocagent.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	view.RegisterExporter(oce)
	return oce, nil
}

// RegisterReconcilerManagerMetricsViews registers the views so that recorded metrics can be exported in the reconciler manager.
func RegisterReconcilerManagerMetricsViews() error {
	return view.Register(ReconcileDurationView)
}

// RegisterReconcilerMetricsViews registers the views so that recorded metrics can be exported in the reconcilers.
func RegisterReconcilerMetricsViews() error {
	return view.Register(
		APICallDurationView,
		ReconcilerErrorsView,
		ParserDurationView,
		LastApplyTimestampView,
		LastSyncTimestampView,
		DeclaredResourcesView,
		ApplyOperationsView,
		ApplyDurationView,
		ResourceFightsView,
		RemediateDurationView,
		ResourceConflictsView,
		InternalErrorsView,
		RenderingCountView,
		SkipRenderingCountView,
		ResourceOverrideCountView,
		GitSyncDepthOverrideCountView,
		NoSSLVerifyCountView,
		PipelineErrorView,
	)
}
