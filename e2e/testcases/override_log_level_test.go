// Copyright 2023 Google LLC
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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

func TestOverrideRootSyncLogLevel(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured)

	rootSyncName := nomostest.RootSyncNN(configsync.RootSyncName)
	rootReconcilerName := core.RootReconcilerObjectKey(rootSyncName.Name)
	rootSyncV1 := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)

	// add kustomize to enable hydration controller container in root-sync
	nt.T.Log("Add the kustomize components root directory")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the kustomize-components directory")
	nt.MustMergePatch(rootSyncV1, `{"spec": {"git": {"dir": "kustomize-components"}}}`)
	syncDirMap := map[types.NamespacedName]string{
		nomostest.DefaultRootRepoNamespacedName: "kustomize-components",
	}

	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// validate initial container log level value
	err := nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerName.Name, rootReconcilerName.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=0"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
			testpredicates.DeploymentHasContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "true"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// apply override to one container and validate the others are unaffected
	nt.MustMergePatch(rootSyncV1, `{"spec": {"override": {"logLevels": [{"containerName": "reconciler", "logLevel": 3}]}}}`)
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerName.Name, rootReconcilerName.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=3"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// apply override to all containers and validate
	nt.MustMergePatch(rootSyncV1, `{"spec": {"override": {"logLevels": [{"containerName": "reconciler", "logLevel": 5}, {"containerName": "git-sync", "logLevel": 7}, {"containerName": "otel-agent", "logLevel": 0}, {"containerName": "hydration-controller", "logLevel": 9}]}}}`)

	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerName.Name, rootReconcilerName.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=7"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=fatal"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=9"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// remove override and validate values are back to initial
	nt.MustMergePatch(rootSyncV1, `{"spec": {"override": null}}`)
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerName.Name, rootReconcilerName.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=0"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// try invalid log level value
	maxError := "logLevel in body should be less than or equal to 10"
	minError := "logLevel in body should be greater than or equal to 0"

	err = nt.KubeClient.MergePatch(rootSyncV1, `{"spec": {"override": {"logLevels": [{"containerName": "reconciler", "logLevel": 13}]}}}`)
	if !strings.Contains(err.Error(), maxError) {
		nt.T.Fatalf("Expecting invalid value error: %q, got %s", maxError, err.Error())
	}

	err = nt.KubeClient.MergePatch(rootSyncV1, `{"spec": {"override": {"logLevels": [{"containerName": "reconciler", "logLevel": -3}]}}}`)
	if !strings.Contains(err.Error(), minError) {
		nt.T.Fatalf("Expecting invalid value error: %q, got %s", minError, err.Error())
	}
}

func TestOverrideRepoSyncLogLevel(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured, ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	frontendReconcilerNN := core.NsReconcilerObjectKey(frontendNamespace, configsync.RepoSyncName)
	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	repoSyncFrontend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, frontendNN)

	// add kustomize to enable hydration controller container in repo-sync
	nt.T.Log("Add the kustomize components root repo directory")
	nt.Must(nt.NonRootRepos[frontendNN].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.NonRootRepos[frontendNN].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RepoSync to sync from the kustomize-components directory")
	repoSyncFrontend.Spec.Git.Dir = "kustomize-components"
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from the kustomize directory"))

	// Verify ns-reconciler-frontend uses the default log level
	err := nt.Watcher.WatchObject(kinds.Deployment(),
		frontendReconcilerNN.Name, frontendReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=0"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
			testpredicates.DeploymentHasContainer(reconcilermanager.HydrationController),
			testpredicates.DeploymentHasEnvVar(reconcilermanager.Reconciler, reconcilermanager.RenderingEnabled, "true"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the log level of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = &v1beta1.RepoSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			LogLevels: []v1beta1.ContainerLogLevelOverride{
				{
					ContainerName: "reconciler",
					LogLevel:      3,
				},
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update log level of frontend Reposync"))

	// validate override and make sure other containers are unaffected
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		frontendReconcilerNN.Name, frontendReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=3"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the log level of the all containers in ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = &v1beta1.RepoSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			LogLevels: []v1beta1.ContainerLogLevelOverride{
				{
					ContainerName: "reconciler",
					LogLevel:      7,
				},
				{
					ContainerName: "git-sync",
					LogLevel:      9,
				},
				{
					ContainerName: "otel-agent",
					LogLevel:      8,
				},
				{
					ContainerName: "hydration-controller",
					LogLevel:      5,
				},
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update log level of frontend Reposync"))

	// validate override for all containers
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		frontendReconcilerNN.Name, frontendReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=7"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=9"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=debug"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear override from repoSync Frontend
	repoSyncFrontend.Spec.Override = nil
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear override from repoSync Frontend"))

	// validate log level value are back to default for all containers
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		frontendReconcilerNN.Name, frontendReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.Reconciler, "-v=0"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "-v=5"),
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.HydrationController, "-v=0"),
			testpredicates.DeploymentContainerArgsContains(metrics.OtelAgentName, "--set=service.telemetry.logs.level=info"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
}
