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

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestOverrideReconcilerResourcesV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.SkipAutopilotCluster,
		// Disable autoscaling to make resources predictable
		ntopts.WithoutReconcilerAutoscalingStrategy,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootReconcilerNN := core.RootReconcilerObjectKey(rootSyncNN.Name)
	backendReconcilerNN := core.NsReconcilerObjectKey(backendNamespace, configsync.RepoSyncName)
	frontendReconcilerNN := core.NsReconcilerObjectKey(frontendNamespace, configsync.RepoSyncName)

	// Get RootSync
	rootSyncObj := &v1alpha1.RootSync{}
	err := nt.Validate(rootSyncNN.Name, rootSyncNN.Namespace, rootSyncObj)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	var defaultResources map[string]v1beta1.ContainerResourcesSpec
	if *e2e.VPA && nomostest.IsReconcilerAutoscalingEnabled(rootSyncObj) {
		defaultResources = controllers.ReconcilerContainerResourceAutoscaleDefaults()
	} else {
		defaultResources = controllers.ReconcilerContainerResourceDefaults()
	}

	// Verify root-reconciler uses the default resource requests and limits
	rootReconcilerDeployment := &appsv1.Deployment{}
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, rootReconcilerDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	nsReconcilerBackendDeployment := &appsv1.Deployment{}
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, nsReconcilerBackendDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	nsReconcilerFrontendDeployment := &appsv1.Deployment{}
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, nsReconcilerFrontendDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootReconcilerDeploymentGeneration := rootReconcilerDeployment.Generation
	nsReconcilerBackendDeploymentGeneration := nsReconcilerBackendDeployment.Generation
	nsReconcilerFrontendDeploymentGeneration := nsReconcilerFrontendDeployment.Generation

	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)

	backendNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, backendNN)

	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	repoSyncFrontend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, frontendNN)

	// Override the CPU/memory requests and limits of the reconciler container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "cpuRequest": "500m", "cpuLimit": "800m", "memoryRequest": "400Mi", "memoryLimit": "411Mi"}]}}}`)
	rootReconcilerDeploymentGeneration++

	updatedRootReconcilerResources := v1beta1.ContainerResourcesSpec{
		ContainerName: reconcilermanager.Reconciler,
		CPURequest:    resource.MustParse("500m"),
		CPULimit:      resource.MustParse("800m"),
		MemoryRequest: resource.MustParse("400Mi"),
		MemoryLimit:   resource.MustParse("411Mi"),
	}

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerResources),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
		},
		testwatcher.WatchTimeout(30*time.Second),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-backend
	repoSyncBackend.Spec.Override = &v1alpha1.OverrideSpec{
		Resources: []v1alpha1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("500m"),
				CPULimit:      resource.MustParse("1000m"),
				MemoryRequest: resource.MustParse("500Mi"),
				MemoryLimit:   resource.MustParse("555Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("600m"),
				CPULimit:      resource.MustParse("1"),
				MemoryRequest: resource.MustParse("600Mi"),
				MemoryLimit:   resource.MustParse("666Mi"),
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = &v1alpha1.OverrideSpec{
		Resources: []v1alpha1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("511m"),
				CPULimit:      resource.MustParse("2000m"),
				MemoryRequest: resource.MustParse("511Mi"),
				MemoryLimit:   resource.MustParse("544Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("611m"),
				CPULimit:      resource.MustParse("2"),
				MemoryRequest: resource.MustParse("611Mi"),
				MemoryLimit:   resource.MustParse("644Mi"),
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend and frontend RepoSync resource limits"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nsReconcilerBackendDeploymentGeneration++
	nsReconcilerFrontendDeploymentGeneration++

	// Verify the resource requests and limits of root-reconciler are not affected by the resource changes of ns-reconciler-backend and ns-reconciler-fronend
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedBackendReconcilerResources := v1beta1.ContainerResourcesSpec{}
	require.NoError(nt.T, v1alpha1.Convert_v1alpha1_ContainerResourcesSpec_To_v1beta1_ContainerResourcesSpec(&repoSyncBackend.Spec.Override.Resources[0], &updatedBackendReconcilerResources, nil))
	updatedBackendGitSyncResources := v1beta1.ContainerResourcesSpec{}
	require.NoError(nt.T, v1alpha1.Convert_v1alpha1_ContainerResourcesSpec_To_v1beta1_ContainerResourcesSpec(&repoSyncBackend.Spec.Override.Resources[1], &updatedBackendGitSyncResources, nil))

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedFrontendReconcilerResources := v1beta1.ContainerResourcesSpec{}
	require.NoError(nt.T, v1alpha1.Convert_v1alpha1_ContainerResourcesSpec_To_v1beta1_ContainerResourcesSpec(&repoSyncFrontend.Spec.Override.Resources[0], &updatedFrontendReconcilerResources, nil))
	updatedFrontendGitSyncResources := v1beta1.ContainerResourcesSpec{}
	require.NoError(nt.T, v1alpha1.Convert_v1alpha1_ContainerResourcesSpec_To_v1beta1_ContainerResourcesSpec(&repoSyncFrontend.Spec.Override.Resources[1], &updatedFrontendGitSyncResources, nil))

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)
	rootReconcilerDeploymentGeneration++

	updatedRootReconcilerGitSyncResources := v1beta1.ContainerResourcesSpec{
		ContainerName: reconcilermanager.GitSync,
		CPURequest:    defaultResources[reconcilermanager.GitSync].CPURequest,
		CPULimit:      resource.MustParse("333m"),
		MemoryRequest: defaultResources[reconcilermanager.GitSync].MemoryRequest,
		MemoryLimit:   defaultResources[reconcilermanager.GitSync].MemoryLimit,
	}

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
			testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerGitSyncResources),
		},
		testwatcher.WatchTimeout(30*time.Second),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)
	rootReconcilerDeploymentGeneration++

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = nil
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nsReconcilerBackendDeploymentGeneration++

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncFrontend
	repoSyncFrontend.Spec.Override = nil
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncFrontend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nsReconcilerFrontendDeploymentGeneration++

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestOverrideReconcilerResourcesV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.SkipAutopilotCluster,
		// Disable autoscaling to make resources predictable
		ntopts.WithoutReconcilerAutoscalingStrategy,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootReconcilerNN := core.RootReconcilerObjectKey(rootSyncNN.Name)
	backendReconcilerNN := core.NsReconcilerObjectKey(backendNamespace, configsync.RepoSyncName)
	frontendReconcilerNN := core.NsReconcilerObjectKey(frontendNamespace, configsync.RepoSyncName)

	// Get RootSync
	rootSyncObj := &v1beta1.RootSync{}
	err := nt.Validate(rootSyncNN.Name, rootSyncNN.Namespace, rootSyncObj)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	var defaultResources map[string]v1beta1.ContainerResourcesSpec
	if *e2e.VPA && nomostest.IsReconcilerAutoscalingEnabled(rootSyncObj) {
		defaultResources = controllers.ReconcilerContainerResourceAutoscaleDefaults()
	} else {
		defaultResources = controllers.ReconcilerContainerResourceDefaults()
	}

	// Verify root-reconciler uses the default resource requests and limits
	rootReconcilerDeployment := &appsv1.Deployment{}
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, rootReconcilerDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	nsReconcilerBackendDeployment := &appsv1.Deployment{}
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, nsReconcilerBackendDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	nsReconcilerFrontendDeployment := &appsv1.Deployment{}
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, nsReconcilerFrontendDeployment,
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootReconcilerDeploymentGeneration := rootReconcilerDeployment.Generation
	nsReconcilerBackendDeploymentGeneration := nsReconcilerBackendDeployment.Generation
	nsReconcilerFrontendDeploymentGeneration := nsReconcilerFrontendDeployment.Generation

	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	backendNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, backendNN)

	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	repoSyncFrontend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, frontendNN)

	// Override the CPU/memory requests and limits of the reconciler container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "cpuRequest": "500m", "cpuLimit": "800m", "memoryRequest": "400Mi", "memoryLimit": "411Mi"}]}}}`)
	rootReconcilerDeploymentGeneration++

	updatedRootReconcilerResources := v1beta1.ContainerResourcesSpec{
		ContainerName: reconcilermanager.Reconciler,
		CPURequest:    resource.MustParse("500m"),
		CPULimit:      resource.MustParse("800m"),
		MemoryRequest: resource.MustParse("400Mi"),
		MemoryLimit:   resource.MustParse("411Mi"),
	}

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerResources),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-backend
	repoSyncBackend.Spec.Override = &v1beta1.OverrideSpec{
		Resources: []v1beta1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("500m"),
				CPULimit:      resource.MustParse("1000m"),
				MemoryRequest: resource.MustParse("500Mi"),
				MemoryLimit:   resource.MustParse("555Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("600m"),
				CPULimit:      resource.MustParse("1"),
				MemoryRequest: resource.MustParse("600Mi"),
				MemoryLimit:   resource.MustParse("666Mi"),
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = &v1beta1.OverrideSpec{
		Resources: []v1beta1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("511m"),
				CPULimit:      resource.MustParse("2000m"),
				MemoryRequest: resource.MustParse("511Mi"),
				MemoryLimit:   resource.MustParse("544Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("611m"),
				CPULimit:      resource.MustParse("2"),
				MemoryRequest: resource.MustParse("611Mi"),
				MemoryLimit:   resource.MustParse("644Mi"),
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend and frontend RepoSync resource limits"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nsReconcilerBackendDeploymentGeneration++
	nsReconcilerFrontendDeploymentGeneration++

	// Verify the resource requests and limits of root-reconciler are not affected by the resource changes of ns-reconciler-backend and ns-reconciler-fronend
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedBackendReconcilerResources := repoSyncBackend.Spec.Override.Resources[0]
	updatedBackendGitSyncResources := repoSyncBackend.Spec.Override.Resources[1]

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedFrontendReconcilerResources := repoSyncFrontend.Spec.Override.Resources[0]
	updatedFrontendGitSyncResources := repoSyncFrontend.Spec.Override.Resources[1]

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)
	rootReconcilerDeploymentGeneration++

	updatedRootReconcilerGitSyncResources := v1beta1.ContainerResourcesSpec{
		ContainerName: reconcilermanager.GitSync,
		CPURequest:    defaultResources[reconcilermanager.GitSync].CPURequest,
		CPULimit:      resource.MustParse("333m"),
		MemoryRequest: defaultResources[reconcilermanager.GitSync].MemoryRequest,
		MemoryLimit:   defaultResources[reconcilermanager.GitSync].MemoryLimit,
	}

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
			testpredicates.DeploymentContainerResourcesEqual(updatedRootReconcilerGitSyncResources),
		},
		testwatcher.WatchTimeout(30*time.Second),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)
	rootReconcilerDeploymentGeneration++

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
			testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedBackendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = nil
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nsReconcilerBackendDeploymentGeneration++

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendReconcilerResources),
		testpredicates.DeploymentContainerResourcesEqual(updatedFrontendGitSyncResources),
	)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncFrontend
	repoSyncFrontend.Spec.Override = nil
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncFrontend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nsReconcilerFrontendDeploymentGeneration++

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.Reconciler]),
		testpredicates.DeploymentContainerResourcesEqual(defaultResources[reconcilermanager.GitSync]),
	)
	if err != nil {
		nt.T.Fatal(err)
	}
}
