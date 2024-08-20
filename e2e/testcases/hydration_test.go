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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
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
	nt.Must(tg.Wait())

	nt.T.Log("Add the kustomize components root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the kustomize-components directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kustomize-components"}}}`)
	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)))

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
	nt.Must(tg.Wait())

	// Validate nomos status
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))

	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")

	nt.T.Log("Remove kustomization.yaml to make the sync fail")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the Kustomize configuration to make the sync fail"))
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "Kustomization config file is missing from the sync directory"),
			testpredicates.RootSyncHasNomosStatus(latestCommit, "ERROR"),
		}))

	nt.T.Log("Add kustomization.yaml back")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components/kustomization.yml", "./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add kustomization.yml back"))

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)))

	// Validate nomos status
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))

	nt.T.Log("Make kustomization.yaml invalid")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/invalid-kustomization.yaml", "./kustomize-components/kustomization.yml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("update kustomization.yaml to make it invalid"))
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "failed to run kustomize build"),
			testpredicates.RootSyncHasNomosStatus(latestCommit, "ERROR"),
		}))

	// one final validation to ensure hydration-controller can be re-disabled
	nt.T.Log("Remove all dry configs")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./kustomize-components"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/compiled/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Replace dry configs with wet configs"))
	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)))

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
	nt.Must(tg.Wait())
}

func TestHydrateExternalFiles(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.Unstructured,
	)

	syncDirMap := map[types.NamespacedName]string{
		nomostest.DefaultRootRepoNamespacedName: "external-files",
	}

	nt.T.Log("Add the external files root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/external-files", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the external-files directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "external-files"}}}`)
	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)))

	nt.T.Log("Validating resources are synced")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Validate("test-configmap", "test-namespace", &corev1.ConfigMap{}, testpredicates.ConfigMapHasData("external-data.txt", "Foo"))
	})

	tg.Go(func() error {
		return nt.Validate("test-namespace", "", &corev1.Namespace{}, testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: namespace.yaml\n"))
	})
	nt.Must(tg.Wait())
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
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}}}`)
	}

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-components"})))

	// Validate nomos status
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))

	nt.T.Log("Validate resources are synced")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
			testpredicates.DeploymentContainerPullPolicyEquals("coredns", "IfNotPresent"),
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin),
			testpredicates.HasAnnotation(metadata.ResourceManagerKey, string(declared.RootScope)))
	})
	tg.Go(func() error {
		return nt.Validate("my-wordpress", "wordpress",
			&appsv1.Deployment{},
			testpredicates.DeploymentContainerPullPolicyEquals("wordpress", "IfNotPresent"),
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin),
			testpredicates.HasAnnotation(metadata.ResourceManagerKey, string(declared.RootScope)))
	})
	nt.Must(tg.Wait())

	nt.T.Log("Use a remote values.yaml file from a public repo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/helm-components-remote-values-kustomization.yaml", "./helm-components/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Render with a remote values.yaml file from a public repo"))

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-components"})))

	nt.Must(nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "Always"),
		testpredicates.DeploymentContainerImageEquals("coredns", "coredns/coredns:1.8.4"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin)))

	// Validate nomos status
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))
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
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}}}`)
	}

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "helm-overlay"})))

	nt.T.Log("Validate resources are synced")
	nt.Must(nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.HasAnnotation("hydration-tool", "kustomize"),
		testpredicates.HasLabel("team", "coredns"),
		testpredicates.HasAnnotation("client.lifecycle.config.k8s.io/mutation", "ignore"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, "configuredIn: base/kustomization.yaml\nconfiguredBy:\n  apiVersion: builtin\n  kind: HelmChartInflationGenerator\n"),
		testpredicates.HasLabel("test-case", "hydration"),
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "Always")))

	// Validate nomos status
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))

	nt.T.Log("Make the hydration fail by checking in an invalid kustomization.yaml")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/resource-duplicate/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/resource-duplicate/namespace_tenant-a.yaml", "./helm-overlay/namespace_tenant-a.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml with duplicated resources"))
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "failed to run kustomize build"),
			testpredicates.RootSyncHasNomosStatus(latestCommit, "ERROR"),
		}))

	nt.T.Log("Make the parsing fail by checking in a deprecated group and kind")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/deprecated-GK/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to render a deprecated group and kind"))
	latestCommit = nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasSourceError(nonhierarchical.DeprecatedGroupKindErrorCode, "The config is using a deprecated Group and Kind"),
			testpredicates.RootSyncHasNomosStatus(latestCommit, "ERROR"),
		}))
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
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, ""),
		}))

	// hydration-controller disabled by default, check for existing of container
	// after sync source contains DRY configs
	nt.T.Log("Check hydration controller default image name")
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", "")))

	nt.T.Log("Enable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"override": {"enableShellInRendering": true}}}`)

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"})))
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationControllerWithShell, "", "")))
	expectedOrigin := "path: base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	nt.T.Log("Validate resources are synced")
	var expectedNamespaces = []string{"tenant-a"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.T.Log("Update kustomization.yaml to use a remote overlay")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-overlay-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to use a remote overlay"))

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"})))

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-b"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	// Update kustomization.yaml to use remote resources
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-resources-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update kustomization.yaml to use remote resources"))
	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "remote-base"})))

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-a", "tenant-b", "tenant-c"}
	expectedOrigin = "path: notCloned/base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("./remote-base"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove remote-base repository"))
	nt.T.Log("Disable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"override": {"enableShellInRendering": false}, "git": {"dir": "acme"}}}`)
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/remote-base", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the remote-base directory when disable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, ""),
		}))
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", "")))
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
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "relative-path/overlays/dev"}}}`)

	nt.Must(nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "relative-path/overlays/dev"})))

	// Validate nomos status
	latestCommit := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)
	nt.Must(nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &v1beta1.RootSync{},
		testpredicates.RootSyncHasNomosStatus(latestCommit, "SYNCED")))

	nt.T.Log("Validating resources are synced")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Validate("foo", "", &corev1.Namespace{},
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/namespace.yaml\n"))
	})
	tg.Go(func() error {
		return nt.Validate("pod-creators", "foo", &rbacv1.RoleBinding{},
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/pod-creator-rolebinding.yaml\n"))
	})
	tg.Go(func() error {
		return nt.Validate("foo-ksa-dev", "foo", &corev1.ServiceAccount{},
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/foo/serviceaccount.yaml\n"))
	})
	tg.Go(func() error {
		return nt.Validate("pod-creator", "", &rbacv1.ClusterRole{},
			testpredicates.HasAnnotation(metadata.KustomizeOrigin, "path: ../../base/pod-creator-clusterrole.yaml\n"))
	})
	nt.Must(tg.Wait())
}
