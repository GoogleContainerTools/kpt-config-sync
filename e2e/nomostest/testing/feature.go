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

package testing

// Feature indicates which feature the e2e test verifies
type Feature string

const (
	// ACMController verifies features like the resource-group-controller, reconciler-manager, and etc.
	ACMController Feature = "acm-controller"
	// NomosCLI verifies the CLI.
	NomosCLI = "nomos-cli"
	// Selector verifies ClusterSelector, the cluster-name-selector annotation, and NamespaceSelector
	Selector = "selector"
	// DriftControl verifies the admission webhook and drift correction.
	DriftControl = "drift-control"
	// Hydration verifies the rendering feature in the hydration-controller.
	Hydration = "hydration"
	// Lifecycle verifies the lifecycle directives feature.
	Lifecycle = "lifecycle"
	// MultiRepos verifies the multiple repos, including multiple root repos, namespace repos and different control modes.
	MultiRepos = "multi-repos"
	// OverrideAPI verifies the override API in RSync.
	OverrideAPI = "override-api"
	// Reconciliation1 verifies the first part of the reconciliation test.
	Reconciliation1 = "reconciliation-1"
	// Reconciliation2 verifies the second part of the reconciliation test.
	Reconciliation2 = "reconciliation-2"
	// SyncSource verifies the various source configs, including different source types, sync directory, branch, and etc.
	SyncSource = "sync-source"
	// WorkloadIdentity verifies authenticating with workload identity (GKE and Fleet).
	WorkloadIdentity = "workload-identity"
)

// KnownFeature indicates whether the test verifies a known feature
func KnownFeature(f Feature) bool {
	switch f {
	case ACMController, NomosCLI, Selector, DriftControl, Hydration,
		Lifecycle, MultiRepos, OverrideAPI, Reconciliation1, Reconciliation2,
		SyncSource, WorkloadIdentity:
		return true
	}
	return false
}
