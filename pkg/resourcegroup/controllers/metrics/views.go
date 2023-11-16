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
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// ReconcileDurationView aggregates the ReconcileDuration metric measurements.
	ReconcileDurationView = &view.View{
		Name:        ReconcileDuration.Name(),
		Measure:     ReconcileDuration,
		Description: "The distribution of time taken to reconcile a ResourceGroup CR",
		TagKeys:     []tag.Key{KeyStallReason},
		Aggregation: view.Distribution(0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
	}

	// ResourceGroupTotalView aggregates the ResourceGroupTotal metric measurements.
	ResourceGroupTotalView = &view.View{
		Name:        ResourceGroupTotal.Name(),
		Measure:     ResourceGroupTotal,
		Description: "The current number of ResourceGroup CRs",
		Aggregation: view.LastValue(),
	}

	// ResourceCountView aggregates the ResourceCount metric measurements.
	ResourceCountView = &view.View{
		Name:        ResourceCount.Name(),
		Measure:     ResourceCount,
		Description: "The total number of resources tracked by a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// ReadyResourceCountView aggregates the resources ready in a ResourceGroup
	ReadyResourceCountView = &view.View{
		Name:        ReadyResourceCount.Name(),
		Measure:     ReadyResourceCount,
		Description: "The total number of ready resources in a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// NamespaceCountView counts number of namespaces in a ResourceGroup
	NamespaceCountView = &view.View{
		Name:        NamespaceCount.Name(),
		Measure:     NamespaceCount,
		Description: "The number of namespaces used by resources in a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// ClusterScopedResourceCountView counts number of namespaces in a ResourceGroup
	ClusterScopedResourceCountView = &view.View{
		Name:        ClusterScopedResourceCount.Name(),
		Measure:     ClusterScopedResourceCount,
		Description: "The number of cluster scoped resources in a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// CRDCountView counts number of namespaces in a ResourceGroup
	CRDCountView = &view.View{
		Name:        CRDCount.Name(),
		Measure:     CRDCount,
		Description: "The number of CRDs in a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// KCCResourceCountView aggregates the KCC resources in a ResourceGroup
	KCCResourceCountView = &view.View{
		Name:        KCCResourceCount.Name(),
		Measure:     KCCResourceCount,
		Description: "The total number of KCC resources in a ResourceGroup",
		TagKeys:     []tag.Key{KeyResourceGroup},
		Aggregation: view.LastValue(),
	}

	// PipelineErrorView aggregates the PipelineError by components
	// TODO: add link to same metric in Config Sync under pkg/metrics/views.go
	PipelineErrorView = &view.View{
		Name:        PipelineError.Name(),
		Measure:     PipelineError,
		Description: "A boolean value indicates if error happened from different stages when syncing a commit",
		TagKeys:     []tag.Key{KeyName, KeyComponent, KeyType},
		Aggregation: view.LastValue(),
	}
)
