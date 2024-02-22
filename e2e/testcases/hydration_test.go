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
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	nomosstatus "kpt.dev/configsync/cmd/nomos/status"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

var expectedBuiltinOrigin = "configuredIn: kustomization.yaml\nconfiguredBy:\n  apiVersion: builtin\n  kind: HelmChartInflationGenerator\n"

func TestHydrateKustomizeComponents(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	syncDirMap := map[types.NamespacedName]string{
		nomostest.DefaultRootRepoNamespacedName: "kustomize-components",
	}

	// Dry configs not yet added to repo, assert that hydration is disabled
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			configsync.RootSyncName,
			configsync.ControllerNamespace,
			&v1beta1.RootSync{},
			testpredicates.MissingAnnotation(metadata.RequiresRenderingAnnotationKey),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(configsync.RootSyncName),
			configsync.ControllerNamespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentMissingContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "false"),
		)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add the kustomize components root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the kustomize-components directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kustomize-components"}}}`)
	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// Dry configs added to repo, assert that hydration is enabled
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			configsync.RootSyncName,
			configsync.ControllerNamespace,
			&v1beta1.RootSync{},
			testpredicates.HasAnnotation(metadata.RequiresRenderingAnnotationKey, "true"),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(configsync.RootSyncName),
			configsync.ControllerNamespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentHasContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "true"),
		)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary := getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus := "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}

	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "base", "tenant-a", "tenant-b", "tenant-c")

	nt.T.Log("Remove kustomization.yaml to make the sync fail")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the Kustomize configuration to make the sync fail"))
	nt.WaitForRootSyncRenderingError(configsync.RootSyncName, status.ActionableHydrationErrorCode, "")

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "ERROR"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Errorf("%v\nExpected error: Kustomization should be missing.\n", err)
	}

	nt.T.Log("Add kustomization.yaml back")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components/kustomization.yml", "./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add kustomization.yml back"))

	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Make kustomization.yaml invalid")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/invalid-kustomization.yaml", "./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("update kustomization.yaml to make it invalid"))
	nt.WaitForRootSyncRenderingError(configsync.RootSyncName, status.ActionableHydrationErrorCode, "")

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "ERROR"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Errorf("%v\nExpected error: Should fail to run kustomize build.\n", err)
	}

	// one final validation to ensure hydration-controller can be re-disabled
	nt.T.Log("Remove all dry configs")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./kustomize-components"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/compiled/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Replace dry configs with wet configs"))
	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the hydration-controller is omitted after dry configs were removed")
	// Dry configs removed from repo, assert that hydration is disabled again
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			configsync.RootSyncName,
			configsync.ControllerNamespace,
			&v1beta1.RootSync{},
			testpredicates.HasAnnotation(metadata.RequiresRenderingAnnotationKey, "false"),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(configsync.RootSyncName),
			configsync.ControllerNamespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentMissingContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "false"),
		)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestHydrateHelmComponents(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	nt.T.Log("Add the helm components root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/helm-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the helm-components directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}}}`)
	}

	err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-components"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary := getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus := "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}

	validateHelmComponents(nt, string(declared.RootReconciler))

	nt.T.Log("Use a remote values.yaml file from a public repo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/helm-components-remote-values-kustomization.yaml", "./helm-components/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Render with a remote values.yaml file from a public repo"))
	err = nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-components"}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "Always"),
		testpredicates.DeploymentContainerImageEquals("coredns", "coredns/coredns:1.8.4"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin)); err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}
}

func TestHydrateHelmOverlay(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	nt.T.Log("Add the helm-overlay root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/helm-overlay", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the helm-overlay directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}}}`)
	}

	err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-overlay"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Validate resources are synced")
	if err := nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.HasAnnotation("hydration-tool", "kustomize"),
		testpredicates.HasLabel("team", "coredns"),
		testpredicates.HasAnnotation("client.lifecycle.config.k8s.io/mutation", "ignore"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, "configuredIn: base/kustomization.yaml\nconfiguredBy:\n  apiVersion: builtin\n  kind: HelmChartInflationGenerator\n"),
		testpredicates.HasLabel("test-case", "hydration")); err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary := getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus := "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Make the hydration fail by checking in an invalid kustomization.yaml")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/resource-duplicate/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/resource-duplicate/namespace_tenant-a.yaml", "./helm-overlay/namespace_tenant-a.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml with duplicated resources"))
	nt.WaitForRootSyncRenderingError(configsync.RootSyncName, status.ActionableHydrationErrorCode, "")

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "ERROR"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Errorf("%v\nExpected errors: kustomization should be invalid.\n", err)
	}

	nt.T.Log("Make the parsing fail by checking in a deprecated group and kind")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/deprecated-GK/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to render a deprecated group and kind"))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, nonhierarchical.DeprecatedGroupKindErrorCode, "")

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary = getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus = "ERROR"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Errorf("%v\nExpected errors: group and kind should be deprecated.\n", err)
	}
}

func TestHydrateRemoteResources(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	nt.T.Log("Add the remote-base root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-base", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the remote-base directory without enable shell in hydration controller")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nt.WaitForRootSyncRenderingError(configsync.RootSyncName, status.ActionableHydrationErrorCode, "")

	// hydration-controller disabled by default, check for existing of container
	// after sync source contains DRY configs
	nt.T.Log("Check hydration controller default image name")
	err := nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", ""))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Enable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"override": {"enableShellInRendering": true}}}`)
	err = nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"}))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationControllerWithShell, "", ""))
	if err != nil {
		nt.T.Fatal(err)
	}
	expectedOrigin := "path: base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	nt.T.Log("Validate resources are synced")
	var expectedNamespaces = []string{"tenant-a"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.T.Log("Update kustomization.yaml to use a remote overlay")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-overlay-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to use a remote overlay"))
	err = nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-b"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	// Update kustomization.yaml to use remote resources
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-resources-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to use remote resources"))
	err = nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-a", "tenant-b", "tenant-c"}
	expectedOrigin = "path: notCloned/base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./remote-base"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove remote-base repository"))
	nt.T.Log("Disable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"override": {"enableShellInRendering": false}, "git": {"dir": "acme"}}}`)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-base", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the remote-base directory when disable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nt.WaitForRootSyncRenderingError(configsync.RootSyncName, status.ActionableHydrationErrorCode, "")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", ""))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestHydrateResourcesInRelativePath(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	nt.T.Log("Add the root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/relative-path", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the relative-path directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "relative-path/overlays/dev"}}}`)

	err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "relative-path/overlays/dev"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	rs = getUpdatedRootSync(nt, configsync.RootSyncName, configsync.ControllerNamespace)
	rsCommit, rsStatus, rsErrorSummary := getRootSyncCommitStatusErrorSummary(rs, nil, false)
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	expectedStatus := "SYNCED"
	if err := validateRootSyncRepoState(latestCommit, rsCommit, expectedStatus, rsStatus, rsErrorSummary); err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Validating resources are synced")
	if err := nt.Validate("foo", "", &corev1.Namespace{}, testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/namespace.yaml\n")); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("pod-creators", "foo", &rbacv1.RoleBinding{}, testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/pod-creator-rolebinding.yaml\n")); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("foo-ksa-dev", "foo", &corev1.ServiceAccount{}, testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/serviceaccount.yaml\n")); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("pod-creator", "", &rbacv1.ClusterRole{}, testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/pod-creator-clusterrole.yaml\n")); err != nil {
		nt.T.Error(err)
	}
}

// getRootSyncCommitStatusErrorSummary converts the given rootSync into a RepoState, then into a string
func getRootSyncCommitStatusErrorSummary(rootSync *v1beta1.RootSync, rg *unstructured.Unstructured, syncingConditionSupported bool) (string, string, string) {
	rs := nomosstatus.RootRepoStatus(rootSync, rg, syncingConditionSupported)

	return rs.GetCommit(), rs.GetStatus(), fmt.Sprintf("%v", rs.GetErrorSummary())
}

// getUpdatedRootSync gets the most recent RootSync from Client
func getUpdatedRootSync(nt *nomostest.NT, name string, namespace string) *v1beta1.RootSync {
	rs := &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(name, namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	return rs
}

// validateRootSyncRepoState verifies the output from getRootSyncCommitStatusErrorSummary is as expected.
func validateRootSyncRepoState(expectedCommit string, commit string, expectedStatus string, status string, errorSummary string) error {
	if expectedCommit != commit || expectedStatus != status {
		return fmt.Errorf("Error: rootSync does not match expected. Got: commit: %v, status: %v\nError Summary: %v\nExpected: commit: %v, status: %v\n",
			commit, status, errorSummary, expectedCommit, expectedStatus)
	}
	return nil
}

// validateHelmComponents validates if all resources are rendered, created and managed by the reconciler.
func validateHelmComponents(nt *nomostest.NT, reconcilerScope string) {
	nt.T.Log("Validate resources are synced")
	if err := nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "IfNotPresent"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin),
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("my-wordpress", "wordpress",
		&appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("wordpress", "IfNotPresent"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin),
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
		nt.T.Error(err)
	}
}
