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

	"go.opencensus.io/tag"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metrics"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
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
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.SkipAutopilotCluster,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	reconcilerResourceRequests, reconcilerResourceLimits, gitSyncResourceRequests, gitSyncResourceLimits := defaultResourceRequestsLimits(nt)
	defaultReconcilerCPURequest, defaultReconcilerMemRequest := reconcilerResourceRequests[corev1.ResourceCPU], reconcilerResourceRequests[corev1.ResourceMemory]
	defaultReconcilerCPULimits, defaultReconcilerMemLimits := reconcilerResourceLimits[corev1.ResourceCPU], reconcilerResourceLimits[corev1.ResourceMemory]
	defaultGitSyncCPURequest, defaultGitSyncMemRequest := gitSyncResourceRequests[corev1.ResourceCPU], gitSyncResourceRequests[corev1.ResourceMemory]
	defaultGitSyncCPULimits, defaultGitSyncMemLimits := gitSyncResourceLimits[corev1.ResourceCPU], gitSyncResourceLimits[corev1.ResourceMemory]

	// Verify root-reconciler uses the default resource requests and limits
	err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.ResourceOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	backendNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	backendRepo, exist := nt.NonRootRepos[backendNN]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	frontendRepo, exist := nt.NonRootRepos[frontendNN]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1(backendNN.Namespace, backendNN.Name, nt.GitProvider.SyncURL(backendRepo.RemoteRepoName))
	repoSyncFrontend := nomostest.RepoSyncObjectV1Alpha1(frontendNN.Namespace, frontendNN.Name, nt.GitProvider.SyncURL(frontendRepo.RemoteRepoName))

	// Override the CPU/memory requests and limits of the reconciler container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "cpuRequest": "500m", "cpuLimit": "800m", "memoryRequest": "400Mi", "memoryLimit": "411Mi"}]}}}`)

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-backend
	repoSyncBackend.Spec.Override = v1alpha1.OverrideSpec{
		Resources: []v1alpha1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("500m"),
				CPULimit:      resource.MustParse("555m"),
				MemoryRequest: resource.MustParse("500Mi"),
				MemoryLimit:   resource.MustParse("555Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("600m"),
				CPULimit:      resource.MustParse("666m"),
				MemoryRequest: resource.MustParse("600Mi"),
				MemoryLimit:   resource.MustParse("666Mi"),
			},
		},
	}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = v1alpha1.OverrideSpec{
		Resources: []v1alpha1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("511m"),
				CPULimit:      resource.MustParse("544m"),
				MemoryRequest: resource.MustParse("511Mi"),
				MemoryLimit:   resource.MustParse("544Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("611m"),
				CPULimit:      resource.MustParse("644m"),
				MemoryRequest: resource.MustParse("611Mi"),
				MemoryLimit:   resource.MustParse("644Mi"),
			},
		},
	}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend and frontend RepoSync resource limits")
	nt.WaitForRepoSyncs()

	// Verify the resource requests and limits of root-reconciler are not affected by the resource changes of ns-reconciler-backend and ns-reconciler-fronend
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, resource.MustParse("333m"), defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		if err := nt.ValidateResourceOverrideCount(string(controllers.RootReconcilerType), "git-sync", "cpu", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCountMissingTags([]tag.Tag{
			{Key: metrics.KeyReconcilerType, Value: string(controllers.RootReconcilerType)},
			{Key: metrics.KeyContainer, Value: "reconciler"},
			{Key: metrics.KeyResourceType, Value: "memory"},
		}); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "cpu", 2); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "memory", 2); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "cpu", 2); err != nil {
			return err
		}
		return nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "memory", 2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Verify root-reconciler uses the default resource requests and limits
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = v1alpha1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend")
	nt.WaitForRepoSyncs()

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		if err := nt.ValidateResourceOverrideCountMissingTags([]tag.Tag{
			{Key: metrics.KeyReconcilerType, Value: string(controllers.RootReconcilerType)},
		}); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "cpu", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "memory", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "cpu", 1); err != nil {
			return err
		}
		return nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "memory", 1)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from repoSyncFrontend
	repoSyncFrontend.Spec.Override = v1alpha1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncFrontend")
	nt.WaitForRepoSyncs()

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.ResourceOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestOverrideReconcilerResourcesV1Beta1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.SkipAutopilotCluster,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	// Get the default CPU/memory requests and limits of the reconciler container and the git-sync container
	reconcilerResourceRequests, reconcilerResourceLimits, gitSyncResourceRequests, gitSyncResourceLimits := defaultResourceRequestsLimits(nt)
	defaultReconcilerCPURequest, defaultReconcilerMemRequest := reconcilerResourceRequests[corev1.ResourceCPU], reconcilerResourceRequests[corev1.ResourceMemory]
	defaultReconcilerCPULimits, defaultReconcilerMemLimits := reconcilerResourceLimits[corev1.ResourceCPU], reconcilerResourceLimits[corev1.ResourceMemory]
	defaultGitSyncCPURequest, defaultGitSyncMemRequest := gitSyncResourceRequests[corev1.ResourceCPU], gitSyncResourceRequests[corev1.ResourceMemory]
	defaultGitSyncCPULimits, defaultGitSyncMemLimits := gitSyncResourceLimits[corev1.ResourceCPU], gitSyncResourceLimits[corev1.ResourceMemory]

	// Verify root-reconciler uses the default resource requests and limits
	err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.ResourceOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	backendNN := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	backendRepo, exist := nt.NonRootRepos[backendNN]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	frontendRepo, exist := nt.NonRootRepos[frontendNN]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1(backendNN.Namespace, backendNN.Name, nt.GitProvider.SyncURL(backendRepo.RemoteRepoName))
	repoSyncFrontend := nomostest.RepoSyncObjectV1Beta1(frontendNN.Namespace, frontendNN.Name, nt.GitProvider.SyncURL(frontendRepo.RemoteRepoName))

	// Override the CPU/memory requests and limits of the reconciler container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "cpuRequest": "500m", "cpuLimit": "800m", "memoryRequest": "400Mi", "memoryLimit": "411Mi"}]}}}`)

	// Verify the reconciler container of root-reconciler uses the new resource request and limits, and the git-sync container uses the default resource requests and limits.
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-backend
	repoSyncBackend.Spec.Override = v1beta1.OverrideSpec{
		Resources: []v1beta1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("500m"),
				CPULimit:      resource.MustParse("555m"),
				MemoryRequest: resource.MustParse("500Mi"),
				MemoryLimit:   resource.MustParse("555Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("600m"),
				CPULimit:      resource.MustParse("666m"),
				MemoryRequest: resource.MustParse("600Mi"),
				MemoryLimit:   resource.MustParse("666Mi"),
			},
		},
	}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)

	// Override the CPU/memory requests and limits of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = v1beta1.OverrideSpec{
		Resources: []v1beta1.ContainerResourcesSpec{
			{
				ContainerName: "reconciler",
				CPURequest:    resource.MustParse("511m"),
				CPULimit:      resource.MustParse("544m"),
				MemoryRequest: resource.MustParse("511Mi"),
				MemoryLimit:   resource.MustParse("544Mi"),
			},
			{
				ContainerName: "git-sync",
				CPURequest:    resource.MustParse("611m"),
				CPULimit:      resource.MustParse("644m"),
				MemoryRequest: resource.MustParse("611Mi"),
				MemoryLimit:   resource.MustParse("644Mi"),
			},
		},
	}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend and frontend RepoSync resource limits")
	nt.WaitForRepoSyncs()

	// Verify the resource requests and limits of root-reconciler are not affected by the resource changes of ns-reconciler-backend and ns-reconciler-fronend
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("800m"), resource.MustParse("400Mi"), resource.MustParse("411Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-backend uses the new resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify ns-reconciler-frontend uses the new resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the CPU limit of the git-sync container of root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "git-sync", "cpuLimit": "333m"}]}}}`)

	// Verify the reconciler container root-reconciler uses the default resource requests and limits, and the git-sync container uses the new resource limits.
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, resource.MustParse("333m"), defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		if err := nt.ValidateResourceOverrideCount(string(controllers.RootReconcilerType), "git-sync", "cpu", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCountMissingTags([]tag.Tag{
			{Key: metrics.KeyReconcilerType, Value: string(controllers.RootReconcilerType)},
			{Key: metrics.KeyContainer, Value: "reconciler"},
			{Key: metrics.KeyResourceType, Value: "memory"},
		}); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "cpu", 2); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "memory", 2); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "cpu", 2); err != nil {
			return err
		}
		return nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "memory", 2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Verify root-reconciler uses the default resource requests and limits
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
			nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-backend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("500m"), resource.MustParse("555m"), resource.MustParse("500Mi"), resource.MustParse("555Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("600m"), resource.MustParse("666m"), resource.MustParse("600Mi"), resource.MustParse("666Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of root-reconciler
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = v1beta1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend")
	nt.WaitForRepoSyncs()

	// Verify ns-reconciler-backend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify root-reconciler uses the default resource requests and limits
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Verify the resource requests and limits of ns-reconciler-frontend are not affected by the resource limit change of ns-reconciler-backend
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, resource.MustParse("511m"), resource.MustParse("544m"), resource.MustParse("511Mi"), resource.MustParse("544Mi")),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, resource.MustParse("611m"), resource.MustParse("644m"), resource.MustParse("611Mi"), resource.MustParse("644Mi")))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		if err := nt.ValidateResourceOverrideCountMissingTags([]tag.Tag{
			{Key: metrics.KeyReconcilerType, Value: string(controllers.RootReconcilerType)},
		}); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "cpu", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "reconciler", "memory", 1); err != nil {
			return err
		}
		if err := nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "cpu", 1); err != nil {
			return err
		}
		return nt.ValidateResourceOverrideCount(string(controllers.NamespaceReconcilerType), "git-sync", "memory", 1)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from repoSyncFrontend
	repoSyncFrontend.Spec.Override = v1beta1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncFrontend")
	nt.WaitForRepoSyncs()

	// Verify ns-reconciler-frontend uses the default resource requests and limits
	err = nt.Validate(reconciler.NsReconcilerName(frontendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{},
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.Reconciler, defaultReconcilerCPURequest, defaultReconcilerCPULimits, defaultReconcilerMemRequest, defaultReconcilerMemLimits),
		nomostest.HasCorrectResourceRequestsLimits(reconcilermanager.GitSync, defaultGitSyncCPURequest, defaultGitSyncCPULimits, defaultGitSyncMemRequest, defaultGitSyncMemLimits))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.ResourceOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}
