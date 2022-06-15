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
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/policycontroller/constrainttemplate"
	"kpt.dev/configsync/pkg/status"
)

func TestConstraintTemplateAndConstraintInSameCommit(t *testing.T) {
	// TODO enable the test on autopilot clusters when GKE 1.21.3-gke.900 reaches regular/stable.
	nt := nomostest.New(t, ntopts.SkipAutopilotCluster)

	if err := nt.ApplyGatekeeperTestData("constraint-template-crd.yaml", "constrainttemplates.templates.gatekeeper.sh"); err != nil {
		nt.T.Fatalf("Failed to create constraint template CRD: %v", err)
	}

	nt.T.Log("Adding CT/C in one commit")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/gatekeeper/constraint-template.yaml", "acme/cluster/constraint-template.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/gatekeeper/constraint.yaml", "acme/cluster/constraint.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add CT/C in one commit")
	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, `No CustomResourceDefinition is defined for the type "K8sAllowedRepos.constraints.gatekeeper.sh" in the cluster`)
	} else {
		nt.WaitForRepoImportErrorCode(status.UnknownKindErrorCode)
	}
	nomostest.Wait(nt.T, "CT on API server", time.Minute*2, func() error {
		ct := constrainttemplate.EmptyConstraintTemplate()
		return nt.Validate("k8sallowedrepos", "", &ct)
	})

	if err := nt.ApplyGatekeeperTestData("constraint-crd.yaml", "k8sallowedrepos.constraints.gatekeeper.sh"); err != nil {
		nt.T.Fatalf("Failed to create constraint CRD: %v", err)
	}
	nt.WaitForRepoSyncs()

	// Delete the constraint template and constraint before deleting CRDs to avoid resource_conflicts error to be recorded
	// Remove them in two separate commits to avoid the safety check failure.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint-template.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove constraint template")
	nt.WaitForRepoSyncs()
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/constraint.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove constraint")
	nt.WaitForRepoSyncs()
}
