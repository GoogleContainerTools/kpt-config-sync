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

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

var expectedBuiltinOrigin = "configuredIn: kustomization.yaml\nconfiguredBy:\n  apiVersion: builtin\n  kind: HelmChartInflationGenerator\n"

func TestHydrateKustomizeComponents(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	// Dry configs not yet added to repo, assert that hydration is disabled
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			rootSyncID.Name,
			rootSyncID.Namespace,
			&v1beta1.RootSync{},
			testpredicates.MissingAnnotation(metadata.RequiresRenderingAnnotationKey),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(rootSyncID.Name),
			rootSyncID.Namespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentMissingContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "false"),
		)
	})
	nt.Must(tg.Wait())

	nt.T.Log("Add the kustomize components root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the kustomize-components directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kustomize-components"}}}`)
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "kustomize-components")
	nt.Must(nt.WatchForAllSyncs())

	// Dry configs added to repo, assert that hydration is enabled
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			rootSyncID.Name,
			rootSyncID.Namespace,
			&v1beta1.RootSync{},
			testpredicates.HasAnnotation(metadata.RequiresRenderingAnnotationKey, "true"),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(rootSyncID.Name),
			rootSyncID.Namespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentHasContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "true"),
		)
	})
	nt.Must(tg.Wait())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")

	nt.T.Log("Remove kustomization.yaml to make the sync fail")
	nt.Must(rootSyncGitRepo.Remove("./kustomize-components/kustomization.yml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove the Kustomize configuration to make the sync fail"))

	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "Kustomization config file is missing from the sync directory"),
		)))

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncRenderingErrors(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		status.ActionableHydrationErrorCode)

	nt.T.Log("Add kustomization.yaml back")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/kustomize-components/kustomization.yml", "./kustomize-components/kustomization.yml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("add kustomization.yml back"))

	nt.Must(nt.WatchForAllSyncs())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

	nt.T.Log("Make kustomization.yaml invalid")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/invalid-kustomization.yaml", "./kustomize-components/kustomization.yml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("update kustomization.yaml to make it invalid"))

	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "failed to run kustomize build"),
		)))

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncRenderingErrors(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		status.ActionableHydrationErrorCode)

	// one final validation to ensure hydration-controller can be re-disabled
	nt.T.Log("Remove all dry configs")
	nt.Must(rootSyncGitRepo.Remove("./kustomize-components"))
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/compiled/kustomize-components", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("Replace dry configs with wet configs"))

	nt.Must(nt.WatchForAllSyncs())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSkipped)

	nt.T.Log("Verify the hydration-controller is omitted after dry configs were removed")
	// Dry configs removed from repo, assert that hydration is disabled again
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Validate(
			rootSyncID.Name,
			rootSyncID.Namespace,
			&v1beta1.RootSync{},
			testpredicates.HasAnnotation(metadata.RequiresRenderingAnnotationKey, "false"),
		)
	})
	tg.Go(func() error {
		return nt.Validate(
			core.RootReconcilerName(rootSyncID.Name),
			configsync.ControllerNamespace,
			&appsv1.Deployment{},
			testpredicates.DeploymentMissingContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "false"),
		)
	})
	nt.Must(tg.Wait())
}

func TestHydrateExternalFiles(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	nt.T.Log("Add the external files root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/external-files", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the external-files directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "external-files"}}}`)
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "external-files")
	nt.Must(nt.WatchForAllSyncs())

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
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	nt.T.Log("Add the helm components root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/helm-components", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the helm-components directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-components"}}}`)
	}
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "helm-components")

	nt.Must(nt.WatchForAllSyncs())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

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
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/helm-components-remote-values-kustomization.yaml", "./helm-components/kustomization.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Render with a remote values.yaml file from a public repo"))

	nt.Must(nt.WatchForAllSyncs())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

	nt.Must(nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "Always"),
		testpredicates.DeploymentContainerImageEquals("coredns", "coredns/coredns:1.8.4"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, expectedBuiltinOrigin)))
}

func TestHydrateHelmOverlay(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	nt.T.Log("Add the helm-overlay root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/helm-overlay", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the helm-overlay directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	if nt.IsGKEAutopilot {
		// b/209458334: set a higher memory of the hydration-controller on Autopilot clusters to avoid the kustomize build failure
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}, "override": {"resources": [{"containerName": "hydration-controller", "memoryRequest": "200Mi"}]}}}`)
	} else {
		nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "helm-overlay"}}}`)
	}
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "helm-overlay")

	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Validate resources are synced")
	nt.Must(nt.Validate("my-coredns-coredns", "coredns", &appsv1.Deployment{},
		testpredicates.HasAnnotation("hydration-tool", "kustomize"),
		testpredicates.HasLabel("team", "coredns"),
		testpredicates.HasAnnotation("client.lifecycle.config.k8s.io/mutation", "ignore"),
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, "configuredIn: base/kustomization.yaml\nconfiguredBy:\n  apiVersion: builtin\n  kind: HelmChartInflationGenerator\n"),
		testpredicates.HasLabel("test-case", "hydration"),
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "Always")))

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

	nt.T.Log("Make the hydration fail by checking in an invalid kustomization.yaml")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/resource-duplicate/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/resource-duplicate/namespace_tenant-a.yaml", "./helm-overlay/namespace_tenant-a.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update kustomization.yaml with duplicated resources"))

	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, "failed to run kustomize build"),
		)))

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncRenderingErrors(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		status.ActionableHydrationErrorCode)

	nt.T.Log("Make the parsing fail by checking in a deprecated group and kind")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/deprecated-GK/kustomization.yaml", "./helm-overlay/kustomization.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update kustomization.yaml to render a deprecated group and kind"))

	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasSourceError(nonhierarchical.DeprecatedGroupKindErrorCode, "The config is using a deprecated Group and Kind"),
		)))

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSourceErrors(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		nonhierarchical.DeprecatedGroupKindErrorCode)
}

func TestHydrateRemoteResources(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	nt.T.Log("Add the remote-base root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/remote-base", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the remote-base directory without enable shell in hydration controller")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "remote-base")
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, ""),
		)))

	// hydration-controller disabled by default, check for existing of container
	// after sync source contains DRY configs
	nt.T.Log("Check hydration controller default image name")
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", "")))

	nt.T.Log("Enable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"override": {"enableShellInRendering": true}}}`)

	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationControllerWithShell, "", "")))
	expectedOrigin := "path: base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	nt.T.Log("Validate resources are synced")
	var expectedNamespaces = []string{"tenant-a"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.T.Log("Update kustomization.yaml to use a remote overlay")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/remote-overlay-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update kustomization.yaml to use a remote overlay"))

	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-b"}
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	// Update kustomization.yaml to use remote resources
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/remote-resources-kustomization.yaml", "./remote-base/kustomization.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update kustomization.yaml to use remote resources"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Validate resources are synced")
	expectedNamespaces = []string{"tenant-a", "tenant-b", "tenant-c"}
	expectedOrigin = "path: notCloned/base/namespace.yaml\nrepo: https://github.com/config-sync-examples/kustomize-components\nref: main\n"
	kustomizecomponents.ValidateNamespaces(nt, expectedNamespaces, expectedOrigin)

	nt.Must(rootSyncGitRepo.Remove("./remote-base"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove remote-base repository"))
	nt.T.Log("Disable shell in hydration controller")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"override": {"enableShellInRendering": false}, "git": {"dir": %q}}}`,
		gitproviders.DefaultSyncDir))
	nomostest.SetExpectedSyncPath(nt, rootSyncID, gitproviders.DefaultSyncDir)
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/remote-base", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the remote-base directory when disable shell in hydration controller")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "remote-base"}}}`)
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasRenderingError(status.ActionableHydrationErrorCode, ""),
		)))
	nt.Must(nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{},
		testpredicates.HasExactlyImage(reconcilermanager.HydrationController, reconcilermanager.HydrationController, "", "")))
}

func TestHydrateResourcesInRelativePath(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t,
		nomostesting.Hydration,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	nt.T.Log("Add the root directory")
	nt.Must(rootSyncGitRepo.Copy("../testdata/hydration/relative-path", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the relative-path directory")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncID.Name)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "relative-path/overlays/dev"}}}`)
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "relative-path/overlays/dev")

	nt.Must(nt.WatchForAllSyncs())

	rs = getRootSync(nt, rootSyncID.Name, rootSyncID.Namespace)
	validateRootSyncSyncCompleted(nt, rs,
		rootSyncGitRepo.MustHash(nt.T),
		parse.RenderingSucceeded)

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

// getRootSync gets the RootSync from the cluster
func getRootSync(nt *nomostest.NT, name string, namespace string) *v1beta1.RootSync {
	rs := &v1beta1.RootSync{}
	nt.Must(nt.KubeClient.Get(name, namespace, rs))
	return rs
}

// gitRevisionOrDefault returns the specified Revision or the default value.
func gitRevisionOrDefault(git v1beta1.Git) string {
	if git.Revision == "" {
		return "HEAD"
	}
	return git.Revision
}

func validateRootSyncSyncCompleted(nt *nomostest.NT, rs *v1beta1.RootSync, commit, renderingMessage string) {
	nt.T.Helper()

	// Use a custom asserter so we can ignore hard-to-test fields.
	// Testing whole structs makes debugging easier by printing the full
	// expected and actual values, but it will also print any ignored fields.
	asserter := testutil.NewAsserter(
		cmpopts.IgnoreFields(v1beta1.SourceStatus{}, "LastUpdate"),
		cmpopts.IgnoreFields(v1beta1.RenderingStatus{}, "LastUpdate"),
		cmpopts.IgnoreFields(v1beta1.SyncStatus{}, "LastUpdate"),
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "LastUpdateTime", "LastTransitionTime", "Message"),
		cmpopts.IgnoreFields(v1beta1.ConfigSyncError{}, "ErrorMessage", "Resources"),
	)

	expectedRootSyncStatus := v1beta1.Status{
		ObservedGeneration: rs.Generation,
		Reconciler:         core.RootReconcilerName(rs.Name),
		LastSyncedCommit:   commit,
		Source: v1beta1.SourceStatus{
			Git: &v1beta1.GitStatus{
				Repo:     rs.Spec.Repo,
				Revision: gitRevisionOrDefault(*rs.Spec.Git),
				Branch:   rs.Spec.Git.Branch,
				Dir:      rs.Spec.Git.Dir,
			},
			// LastUpdate ignored
			Commit:       commit,
			Errors:       nil,
			ErrorSummary: &v1beta1.ErrorSummary{},
		},
		Rendering: v1beta1.RenderingStatus{
			Git: &v1beta1.GitStatus{
				Repo:     rs.Spec.Repo,
				Revision: gitRevisionOrDefault(*rs.Spec.Git),
				Branch:   rs.Spec.Git.Branch,
				Dir:      rs.Spec.Git.Dir,
			},
			// LastUpdate ignored
			Message:      renderingMessage, // RenderingSucceeded/RenderingSkipped
			Commit:       commit,
			Errors:       nil,
			ErrorSummary: &v1beta1.ErrorSummary{},
		},
		Sync: v1beta1.SyncStatus{
			Git: &v1beta1.GitStatus{
				Repo:     rs.Spec.Repo,
				Revision: gitRevisionOrDefault(*rs.Spec.Git),
				Branch:   rs.Spec.Git.Branch,
				Dir:      rs.Spec.Git.Dir,
			},
			// LastUpdate ignored
			Commit:       commit,
			Errors:       nil,
			ErrorSummary: &v1beta1.ErrorSummary{},
		},
	}
	assertEqual(nt, asserter, expectedRootSyncStatus, rs.Status.Status,
		"RootSync .status")

	// Validate Syncing condition fields
	rsSyncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
	expectedSyncingCondition := &v1beta1.RootSyncCondition{
		Type:   v1beta1.RootSyncSyncing,
		Status: metav1.ConditionFalse,
		// LastUpdateTime ignored
		// LastTransitionTime ignored
		Reason:  "Sync",
		Message: "Sync Completed",
		Commit:  commit,
		// Errors unused by the Syncing condition (always nil)
		ErrorSourceRefs: nil,
		ErrorSummary:    &v1beta1.ErrorSummary{},
	}
	assertEqual(nt, asserter, expectedSyncingCondition, rsSyncingCondition,
		"RootSync .status.conditions[.status=%q]", v1beta1.RootSyncSyncing)

	if nt.T.Failed() {
		nt.T.FailNow()
	}
}

func validateRootSyncRenderingErrors(nt *nomostest.NT, rs *v1beta1.RootSync, commit string, errCodes ...string) {
	nt.T.Helper()

	if len(errCodes) == 0 {
		nt.T.Fatal("Invalid test: expected specific errors to validate, but none were specified")
	}

	// Use a custom asserter so we can ignore hard-to-test fields.
	// Testing whole structs makes debugging easier by printing the full
	// expected and actual values, but it will also print any ignored fields.
	asserter := testutil.NewAsserter(
		cmpopts.IgnoreFields(v1beta1.RenderingStatus{}, "LastUpdate"),
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "LastUpdateTime", "LastTransitionTime", "Message"),
		cmpopts.IgnoreFields(v1beta1.ConfigSyncError{}, "ErrorMessage", "Resources"),
		// Ignore the current Syncing condition status. Retry will flip it back to True.
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "Status"),
	)

	// Build untruncated ErrorSummary & fake Errors list from error codes
	var errorList []v1beta1.ConfigSyncError
	var errorSources []v1beta1.ErrorSource

	errorSources = append(errorSources, v1beta1.RenderingError)
	errorSummary := &v1beta1.ErrorSummary{
		TotalCount:                len(errCodes),
		Truncated:                 false,
		ErrorCountAfterTruncation: len(errCodes),
	}
	for _, errCode := range errCodes {
		errorList = append(errorList, v1beta1.ConfigSyncError{
			Code: errCode,
			// ErrorMessage ignored
			// Resources ignored
		})
	}

	// Validate .status.rendering fields.
	expectedRenderingStatus := v1beta1.RenderingStatus{
		Git: &v1beta1.GitStatus{
			Repo:     rs.Spec.Repo,
			Revision: gitRevisionOrDefault(*rs.Spec.Git),
			Branch:   rs.Spec.Git.Branch,
			Dir:      rs.Spec.Git.Dir,
		},
		// LastUpdate ignored
		Message:      parse.RenderingFailed,
		Commit:       commit,
		Errors:       errorList,
		ErrorSummary: errorSummary,
	}
	assertEqual(nt, asserter, expectedRenderingStatus, rs.Status.Rendering,
		"RootSync .status.rendering")

	// Validate Syncing condition fields
	rsSyncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
	expectedSyncingCondition := &v1beta1.RootSyncCondition{
		Type: v1beta1.RootSyncSyncing,
		// Status ignored
		// LastUpdateTime ignored
		// LastTransitionTime ignored
		Reason:  "Rendering",
		Message: "Rendering failed",
		Commit:  commit,
		// Errors unused by the Syncing condition (always nil)
		ErrorSourceRefs: errorSources,
		ErrorSummary:    errorSummary,
	}
	assertEqual(nt, asserter, expectedSyncingCondition, rsSyncingCondition,
		"RootSync .status.conditions[.status=%q]", v1beta1.RootSyncSyncing)

	if nt.T.Failed() {
		nt.T.FailNow()
	}
}

func validateRootSyncSourceErrors(nt *nomostest.NT, rs *v1beta1.RootSync, commit string, errCodes ...string) {
	nt.T.Helper()

	if len(errCodes) == 0 {
		nt.T.Fatal("Invalid test: expected specific errors to validate, but none were specified")
	}

	// Use a custom asserter so we can ignore hard-to-test fields.
	// Testing whole structs makes debugging easier by printing the full
	// expected and actual values, but it will also print any ignored fields.
	asserter := testutil.NewAsserter(
		cmpopts.IgnoreFields(v1beta1.SourceStatus{}, "LastUpdate"),
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "LastUpdateTime", "LastTransitionTime", "Message"),
		cmpopts.IgnoreFields(v1beta1.ConfigSyncError{}, "ErrorMessage", "Resources"),
		// Ignore the current Syncing condition status. Retries will cause flapping between True & False.
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "Status"),
	)

	// Build untruncated ErrorSummary & fake Errors list from error codes
	var errorList []v1beta1.ConfigSyncError
	var errorSources []v1beta1.ErrorSource

	errorSources = append(errorSources, v1beta1.SourceError)
	errorSummary := &v1beta1.ErrorSummary{
		TotalCount:                len(errCodes),
		Truncated:                 false,
		ErrorCountAfterTruncation: len(errCodes),
	}
	for _, errCode := range errCodes {
		errorList = append(errorList, v1beta1.ConfigSyncError{
			Code: errCode,
			// ErrorMessage ignored
			// Resources ignored
		})
	}

	// Validate .status.source fields.
	expectedSourceStatus := v1beta1.SourceStatus{
		Git: &v1beta1.GitStatus{
			Repo:     rs.Spec.Repo,
			Revision: gitRevisionOrDefault(*rs.Spec.Git),
			Branch:   rs.Spec.Git.Branch,
			Dir:      rs.Spec.Git.Dir,
		},
		// LastUpdate ignored
		Commit:       commit,
		Errors:       errorList,
		ErrorSummary: errorSummary,
	}
	assertEqual(nt, asserter, expectedSourceStatus, rs.Status.Source,
		"RootSync .status.source")

	// Validate Syncing condition fields
	rsSyncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
	expectedSyncingCondition := &v1beta1.RootSyncCondition{
		Type: v1beta1.RootSyncSyncing,
		// Status ignored
		// LastUpdateTime ignored
		// LastTransitionTime ignored
		Reason:  "Source",
		Message: "Source",
		Commit:  commit,
		// Errors unused by the Syncing condition (always nil)
		ErrorSourceRefs: errorSources,
		ErrorSummary:    errorSummary,
	}
	assertEqual(nt, asserter, expectedSyncingCondition, rsSyncingCondition,
		"RootSync .status.conditions[.status=%q]", v1beta1.RootSyncSyncing)

	if nt.T.Failed() {
		nt.T.FailNow()
	}
}

// assertEqual simulates testutil.AssertEqual, but works with the nomostest.NT interface.
func assertEqual(nt *nomostest.NT, asserter *testutil.Asserter, expected, actual interface{}, msgAndArgs ...interface{}) {
	nt.T.Helper()
	matcher := asserter.EqualMatcher(expected)
	match, err := matcher.Match(actual)
	if err != nil {
		nt.T.Fatalf("errored testing equality: %v", err)
		return
	}
	if !match {
		assert.Fail(nt.T, matcher.FailureMessage(actual), msgAndArgs...)
	}
}
