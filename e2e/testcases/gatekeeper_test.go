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

package e2e

import (
	"testing"
	"time"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/policycontroller/constrainttemplate"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestConstraintTemplateAndConstraintInSameCommit(t *testing.T) {
	// TODO enable the test on autopilot clusters when GKE 1.21.3-gke.900 reaches regular/stable.
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.SkipAutopilotCluster)

	// Simulate install of Gatekeeper with just the ConstraintTemplate CRD
	if err := nt.ApplyGatekeeperCRD("constraint-template-crd.yaml", "constrainttemplates.templates.gatekeeper.sh"); err != nil {
		nt.T.Fatalf("Failed to create ConstraintTemplate CRD: %v", err)
	}

	nt.T.Log("Adding ConstraintTemplate & Constraint in one commit")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/gatekeeper/constraint-template.yaml", "acme/cluster/constraint-template.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/gatekeeper/constraint.yaml", "acme/cluster/constraint.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add ConstraintTemplate & Constraint")

	// Cleanup if waiting for sync error fails.
	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint-template.yaml")
			nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint.yaml")
			// Apply a cluster-scoped resource to pass the safety check (KNV2006).
			// Will be removed by the normal root repo cleanup.
			nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/test-clusterrole.yaml", fake.ClusterRoleObject())
			nt.RootRepos[configsync.RootSyncName].CommitAndPush("Reset the acme directory")
			nt.WaitForRepoSyncs()
		}
	})

	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode,
			`No CustomResourceDefinition is defined for the type "K8sAllowedRepos.constraints.gatekeeper.sh" in the cluster`)
	} else {
		nt.WaitForRepoImportErrorCode(status.UnknownKindErrorCode)
	}

	// Simulate Gatekeeper's controller behavior.
	// Wait for the ConstraintTemplate to be applied, then apply the Constraint CRD.
	nomostest.Wait(nt.T, "ConstraintTemplate on API server", 2*time.Minute, func() error {
		ct := constrainttemplate.EmptyConstraintTemplate()
		return nt.Validate("k8sallowedrepos", "", &ct)
	})
	if err := nt.ApplyGatekeeperCRD("constraint-crd.yaml", "k8sallowedrepos.constraints.gatekeeper.sh"); err != nil {
		nt.T.Fatalf("Failed to create constraint CRD: %v", err)
	}
	// Sync should eventually succeed on retry, now that all the required CRDs exist.
	nt.WaitForRepoSyncs()

	// Cleanup before deleting the CRDs to avoid resource conflict errors from the webhook.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint-template.yaml")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint.yaml")
	// Apply a cluster-scoped resource to pass the safety check (KNV2006).
	// Will be removed by the normal root repo cleanup.
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/test-clusterrole.yaml", fake.ClusterRoleObject())
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Reset the acme directory")
	nt.WaitForRepoSyncs()
}
