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
	"io/ioutil"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
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
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/yaml"
)

func defaultResourceRequestsLimits(nt *nomostest.NT) (reconcilerRequests, reconcilerLimits, gitSyncRequests, gitSyncLimits corev1.ResourceList) {
	path := "../../manifests/templates/reconciler-manager-configmap.yaml"
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		nt.T.Fatalf("Failed to read file (%s): %v", path, err)
	}

	var cm corev1.ConfigMap
	if err := yaml.Unmarshal(contents, &cm); err != nil {
		nt.T.Fatalf("Failed to parse the ConfigMap object in %s: %v", path, err)
	}

	key := "deployment.yaml"
	deployContents, ok := cm.Data[key]
	if !ok {
		nt.T.Fatalf("The `data` field of the ConfigMap object in %s does not include the %q key", path, key)
	}

	var deploy appsv1.Deployment
	if err := yaml.Unmarshal([]byte(deployContents), &deploy); err != nil {
		nt.T.Fatalf("Failed to parse the Deployment object in the `data` field of the ConfigMap object in %s: %v", path, err)
	}

	for _, container := range deploy.Spec.Template.Spec.Containers {
		if container.Name == reconcilermanager.Reconciler || container.Name == reconcilermanager.GitSync {
			if container.Resources.Requests.Cpu().IsZero() || container.Resources.Requests.Memory().IsZero() {
				nt.T.Fatalf("The %s container in %s should define CPU/memory requests", container.Name, path)
			}
		}
		if container.Name == reconcilermanager.Reconciler {
			reconcilerRequests = container.Resources.Requests
			reconcilerLimits = container.Resources.Limits
		}
		if container.Name == reconcilermanager.GitSync {
			gitSyncRequests = container.Resources.Requests
			gitSyncLimits = container.Resources.Limits
		}
	}
	return
}

func TestOverrideReconcilerResourcesV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.SkipAutopilotCluster,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: v1.NSConfigManagementSystem,
	}
	backendReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}
	frontendReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(frontendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	reconcilerResourceRequests, reconcilerResourceLimits, gitSyncResourceRequests, gitSyncResourceLimits := defaultResourceRequestsLimits(nt)
	defaultReconcilerCPURequest, defaultReconcilerMemRequest := reconcilerResourceRequests[corev1.ResourceCPU], reconcilerResourceRequests[corev1.ResourceMemory]
	defaultReconcilerCPULimits, defaultReconcilerMemLimits := reconcilerResourceLimits[corev1.ResourceCPU], reconcilerResourceLimits[corev1.ResourceMemory]
	defaultGitSyncCPURequest, defaultGitSyncMemRequest := gitSyncResourceRequests[corev1.ResourceCPU], gitSyncResourceRequests[corev1.ResourceMemory]
	defaultGitSyncCPULimits, defaultGitSyncMemLimits := gitSyncResourceLimits[corev1.ResourceCPU], gitSyncResourceLimits[corev1.ResourceMemory]

	// Verify root-reconciler uses the default resource requests and limits
	rootReconcilerDeployment := &appsv1.Deployment{}
	err := nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, rootReconcilerDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	nsReconcilerBackendDeployment := &appsv1.Deployment{}
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, nsReconcilerBackendDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	nsReconcilerFrontendDeployment := &appsv1.Deployment{}
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, nsReconcilerFrontendDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
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

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)
	rootReconcilerDeploymentGeneration++

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, resource.MustParse("333m"), defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestOverrideReconcilerResourcesV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.SkipAutopilotCluster,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: v1.NSConfigManagementSystem,
	}
	backendReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}
	frontendReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(frontendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	reconcilerResourceRequests, reconcilerResourceLimits, gitSyncResourceRequests, gitSyncResourceLimits := defaultResourceRequestsLimits(nt)
	defaultReconcilerCPURequest, defaultReconcilerMemRequest := reconcilerResourceRequests[corev1.ResourceCPU], reconcilerResourceRequests[corev1.ResourceMemory]
	defaultReconcilerCPULimits, defaultReconcilerMemLimits := reconcilerResourceLimits[corev1.ResourceCPU], reconcilerResourceLimits[corev1.ResourceMemory]
	defaultGitSyncCPURequest, defaultGitSyncMemRequest := gitSyncResourceRequests[corev1.ResourceCPU], gitSyncResourceRequests[corev1.ResourceMemory]
	defaultGitSyncCPULimits, defaultGitSyncMemLimits := gitSyncResourceLimits[corev1.ResourceCPU], gitSyncResourceLimits[corev1.ResourceMemory]

	// Verify root-reconciler uses the default resource requests and limits
	rootReconcilerDeployment := &appsv1.Deployment{}
	err := nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, rootReconcilerDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	nsReconcilerBackendDeployment := &appsv1.Deployment{}
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, nsReconcilerBackendDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	nsReconcilerFrontendDeployment := &appsv1.Deployment{}
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, nsReconcilerFrontendDeployment,
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
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

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)
	rootReconcilerDeploymentGeneration++

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerNN.Name, rootReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, resource.MustParse("333m"), defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(backendReconcilerNN.Name, backendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerBackendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("1000m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("1"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(rootReconcilerNN.Name, rootReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(rootReconcilerDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(frontendReconcilerNN.Name, frontendReconcilerNN.Namespace, &appsv1.Deployment{},
		testpredicates.GenerationEquals(nsReconcilerFrontendDeploymentGeneration),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("2000m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("2"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
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
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		testpredicates.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}
}
