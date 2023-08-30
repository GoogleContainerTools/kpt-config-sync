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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reposync"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	gitSourceType      = "git"
	branch             = "1.0.0"
	gitRevision        = "1.0.0.rc.8"
	gitUpdatedRevision = "1.1.0.rc.1"

	reposyncNs     = "bookinfo"
	reposyncName   = "my-repo-sync"
	reposyncRepo   = "https://github.com/test/reposync/csp-config-management/"
	reposyncDir    = "foo-corp"
	reposyncSSHKey = "ssh-key"
	reposyncCookie = "cookie"

	secretName = "git-creds"

	gcpSAEmail = "config-sync@cs-project.iam.gserviceaccount.com"

	pollingPeriod = "50ms"
)

var filesystemPollingPeriod time.Duration
var hydrationPollingPeriod time.Duration
var helmSyncVersionPollingPeriod time.Duration
var nsReconcilerName = core.NsReconcilerName(reposyncNs, reposyncName)
var reconcilerDeploymentReplicaCount int32 = 1

var parsedDeployment = func(de *appsv1.Deployment) error {
	de.TypeMeta = fake.ToTypeMeta(kinds.Deployment())
	de.Spec = appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				metadata.ReconcilerLabel: reconcilermanager.Reconciler,
			},
		},
		Replicas: &reconcilerDeploymentReplicaCount,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: defaultContainers(),
				Volumes:    deploymentSecretVolumes("git-creds", ""),
			},
		},
	}
	return nil
}

var helmParsedDeployment = func(de *appsv1.Deployment) error {
	de.TypeMeta = fake.ToTypeMeta(kinds.Deployment())
	de.Spec = appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				metadata.ReconcilerLabel: reconcilermanager.Reconciler,
			},
		},
		Replicas: &reconcilerDeploymentReplicaCount,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: defaultContainers(),
				Volumes:    helmDeploymentSecretVolumes("helm-creds"),
			},
		},
	}
	return nil
}

func init() {
	var err error
	filesystemPollingPeriod, err = time.ParseDuration(pollingPeriod)
	if err != nil {
		klog.Exitf("failed to parse polling period: %q, got error: %v, want error: nil", pollingPeriod, err)
	}
	hydrationPollingPeriod = filesystemPollingPeriod
	helmSyncVersionPollingPeriod = filesystemPollingPeriod
}

func reposyncSourceType(sourceType string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.SourceType = sourceType
	}
}

func reposyncRef(rev string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Revision = rev
	}
}

func reposyncBranch(branch string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Branch = branch
	}
}

func reposyncSecretType(auth configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Auth = auth
	}
}

func reposyncOCIAuthType(auth configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Oci.Auth = auth
	}
}
func reposyncHelmAuthType(auth configsync.AuthType) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Helm.Auth = auth
	}
}

func reposyncHelmSecretRef(ref string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Helm.SecretRef = &v1beta1.SecretReference{Name: ref}
	}
}

func reposyncSecretRef(ref string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: ref}
	}
}

func reposyncGCPSAEmail(email string) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.GCPServiceAccountEmail = email
	}
}

func reposyncOverrideResources(containers []v1beta1.ContainerResourcesSpec) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Override = &v1beta1.OverrideSpec{
			Resources: containers,
		}
	}
}

func reposyncOverrideGitSyncDepth(depth int64) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.SafeOverride().GitSyncDepth = &depth
	}
}

func reposyncOverrideReconcileTimeout(reconcileTimeout metav1.Duration) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.SafeOverride().ReconcileTimeout = &reconcileTimeout
	}
}

func reposyncOverrideAPIServerTimeout(apiServerTimout metav1.Duration) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.SafeOverride().APIServerTimeout = &apiServerTimout
	}
}

func reposyncNoSSLVerify() func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.NoSSLVerify = true
	}
}

func reposyncCACert(caCertSecretRef string) func(sync *v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecretRef}
	}
}

func reposyncRenderingRequired(renderingRequired bool) func(sync *v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		val := strconv.FormatBool(renderingRequired)
		core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, val)
	}
}

func repoSync(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1(ns, name)
	// default to require rendering for convenience with existing tests
	core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, "true")
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func repoSyncWithGit(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	addGit := func(rs *v1beta1.RepoSync) {
		rs.Spec.SourceType = string(v1beta1.GitSource)
		rs.Spec.Git = &v1beta1.Git{
			Repo: reposyncRepo,
			Dir:  reposyncDir,
		}
	}
	opts = append([]func(*v1beta1.RepoSync){addGit}, opts...)
	return repoSync(ns, name, opts...)
}

func repoSyncWithOCI(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	addOci := func(rs *v1beta1.RepoSync) {
		rs.Spec.SourceType = string(v1beta1.OciSource)
		rs.Spec.Oci = &v1beta1.Oci{
			Image: ociImage,
			Dir:   reposyncDir,
		}
	}
	opts = append([]func(*v1beta1.RepoSync){addOci}, opts...)
	return repoSync(ns, name, opts...)
}

func repoSyncWithHelm(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	addHelm := func(rs *v1beta1.RepoSync) {
		rs.Spec.SourceType = string(v1beta1.HelmSource)
		rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
			Repo:    helmRepo,
			Chart:   helmChart,
			Version: helmVersion,
		}}
	}
	opts = append([]func(*v1beta1.RepoSync){addHelm}, opts...)
	return repoSync(ns, name, opts...)
}

func rolebinding(name string, opts ...core.MetaMutator) *rbacv1.RoleBinding {
	result := fake.RoleBindingObject(opts...)
	result.Name = name

	result.RoleRef.Name = RepoSyncPermissionsName()
	result.RoleRef.Kind = "ClusterRole"
	result.RoleRef.APIGroup = "rbac.authorization.k8s.io"

	return result
}

func setupNSReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *syncerFake.DynamicClient, *RepoSyncReconciler) {
	t.Helper()

	// Configure controller-manager to log to the test logger
	controllerruntime.SetLogger(testr.New(t))

	cs := syncerFake.NewClientSet(t, core.Scheme)

	ctx := context.Background()
	for _, obj := range objs {
		err := cs.Client.Create(ctx, obj)
		if err != nil {
			t.Fatalf("Failed to create object: %v", err)
		}
	}

	testReconciler := NewRepoSyncReconciler(
		testCluster,
		filesystemPollingPeriod,
		hydrationPollingPeriod,
		cs.Client,
		cs.Client,
		cs.DynamicClient,
		controllerruntime.Log.WithName("controllers").WithName(configsync.RepoSyncKind),
		cs.Client.Scheme(),
	)
	return cs.Client, cs.DynamicClient, testReconciler
}

func TestCreateAndUpdateNamespaceReconcilerWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	overrideReconcilerAndGitSyncResourceLimits := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
		{
			ContainerName: reconcilermanager.GitSync,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH),
		reposyncSecretRef(reposyncSSHKey), reposyncOverrideResources(overrideReconcilerAndGitSyncResourceLimits))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerAndGitSyncResourceLimits),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the CPU resources of the reconciler container and the memory resources of the git-sync container
	overrideReconcilerCPUAndGitSyncMemResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("0.8"),
			CPULimit:      resource.MustParse("1.2"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("0.6"),
			CPULimit:      resource.MustParse("0.8"),
		},
		{
			ContainerName: reconcilermanager.GitSync,
			MemoryRequest: resource.MustParse("777Gi"),
			MemoryLimit:   resource.MustParse("888Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideReconcilerCPUAndGitSyncMemResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerCPUAndGitSyncMemResources),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestUpdateNamespaceReconcilerWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the CPU/memory limits of both the reconciler and git-sync container
	overrideReconcilerAndGitSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1000m"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
		{
			ContainerName: reconcilermanager.GitSync,
			CPURequest:    resource.MustParse("500m"),
			CPULimit:      resource.MustParse("1"),
			MemoryRequest: resource.MustParse("500Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideReconcilerAndGitSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerAndGitSyncResources),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test overriding the CPU/memory requests and limits of the reconciler container
	overrideReconcilerResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("1.8"),
			CPULimit:      resource.MustParse("2"),
			MemoryRequest: resource.MustParse("1.8Gi"),
			MemoryLimit:   resource.MustParse("2Gi"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("1"),
			CPULimit:      resource.MustParse("1.3"),
			MemoryRequest: resource.MustParse("3Gi"),
			MemoryLimit:   resource.MustParse("4Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideReconcilerResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerResources),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test overriding the memory requests and limits of the git-sync container
	overrideGitSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.GitSync,
			MemoryRequest: resource.MustParse("800m"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideGitSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideGitSyncResources),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("5"), setGeneration(5),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncCreateWithNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncNoSSLVerify())
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncSourceType(gitSourceType), reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"replicas":          0,
				"updatedReplicas":   0,
				"readyReplicas":     0,
				"availableReplicas": 0,
				"conditions": []appsv1.DeploymentCondition{
					*newDeploymentCondition(appsv1.DeploymentAvailable, corev1.ConditionFalse, "unused", "unused"),
					*newDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue, "NewReplicaSetAvailable", "unused"),
				},
			},
		},
	}
	deployment.SetGroupVersionKind(kinds.Deployment())
	patchData, err := json.Marshal(deployment)
	if err != nil {
		t.Fatalf("failed to change unstructured to byte array: %v", err)
	}
	t.Logf("Applying Patch: %s", string(patchData))
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(repoDeployment.Namespace).
		Patch(ctx, repoDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to patch the deployment status: %v", err)
	}

	// Simulate Reconcile triggered by Deployment update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error after deployment created, got error: %q, want error: nil", err)
	}

	// RepoSync should still be reconciling because the Deployment is not yet available
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// Simulate Deployment becoming Available
	replicas := *repoDeployment.Spec.Replicas
	deployment = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"replicas":          replicas,
				"updatedReplicas":   replicas,
				"readyReplicas":     replicas,
				"availableReplicas": replicas,
				"conditions": []appsv1.DeploymentCondition{
					*newDeploymentCondition(appsv1.DeploymentAvailable, corev1.ConditionTrue, "unused", "unused"),
					*newDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue, "NewReplicaSetAvailable", "unused"),
				},
			},
		},
	}
	deployment.SetGroupVersionKind(kinds.Deployment())
	patchData, err = json.Marshal(deployment)
	if err != nil {
		t.Fatalf("failed to change unstructured to byte array: %v", err)
	}
	t.Logf("Applying Patch: %s", string(patchData))
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(repoDeployment.Namespace).
		Patch(ctx, repoDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to patch the deployment status: %v", err)
	}

	// Simulate Reconcile triggered by Deployment update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error after deployment update, got error: %q, want error: nil", err)
	}

	// RepoSync should be done reconciling because the Deployment is available
	reposync.ClearCondition(wantRs, v1beta1.RepoSyncReconciling)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// Set rs.Spec.NoSSLVerify to false
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	// Simulate Reconcile triggered by RepoSync update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// RepoSync should be unchanged because NoSSLVerify defaults to false
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoDeployment.ResourceVersion = "3"
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.NoSSLVerify to true
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.NoSSLVerify = true
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	// Simulate Reconcile triggered by RepoSync update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRepoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Simulate Deployment being unavailable while replacing its pod
	deployment = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"replicas":          1,
				"updatedReplicas":   0,
				"readyReplicas":     0,
				"availableReplicas": 0,
				"conditions": []appsv1.DeploymentCondition{
					*newDeploymentCondition(appsv1.DeploymentAvailable, corev1.ConditionFalse, "unused", "unused"),
					*newDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue, "NewReplicaSetAvailable", "unused"),
				},
			},
		},
	}
	deployment.SetGroupVersionKind(kinds.Deployment())
	patchData, err = json.Marshal(deployment)
	if err != nil {
		t.Fatalf("failed to change unstructured to byte array: %v", err)
	}
	t.Logf("Applying Patch: %s", string(patchData))
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(repoDeployment.Namespace).
		Patch(ctx, repoDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to patch the deployment status: %v", err)
	}

	// Simulate Reconcile triggered by Deployment update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error after deployment created, got error: %q, want error: nil", err)
	}

	// RepoSync should still be reconciling because the Deployment is not yet available
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Updated: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// Simulate Deployment becoming Available
	deployment = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"status": map[string]interface{}{
				"replicas":          replicas,
				"updatedReplicas":   replicas,
				"readyReplicas":     replicas,
				"availableReplicas": replicas,
				"conditions": []appsv1.DeploymentCondition{
					*newDeploymentCondition(appsv1.DeploymentAvailable, corev1.ConditionTrue, "unused", "unused"),
					*newDeploymentCondition(appsv1.DeploymentProgressing, corev1.ConditionTrue, "NewReplicaSetAvailable", "unused"),
				},
			},
		},
	}
	deployment.SetGroupVersionKind(kinds.Deployment())
	patchData, err = json.Marshal(deployment)
	if err != nil {
		t.Fatalf("failed to change unstructured to byte array: %v", err)
	}
	t.Logf("Applying Patch: %s", string(patchData))
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(repoDeployment.Namespace).
		Patch(ctx, repoDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to patch the deployment status: %v", err)
	}

	// Simulate Reconcile triggered by Deployment update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error after deployment update, got error: %q, want error: nil", err)
	}

	// RepoSync should be done reconciling because the Deployment is available
	reposync.ClearCondition(wantRs, v1beta1.RepoSyncReconciling)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("6"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}

	// Set rs.Spec.NoSSLVerify to false
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	// Simulate Reconcile triggered by RepoSync update
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// RepoSync should still be reconciling because the Deployment is not yet available
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("7"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncCreateWithCACert(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment
	caCertSecret := "foo-secret"
	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch),
		reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName),
		reposyncCACert(caCertSecret))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	certSecret.Data[CACertSecretKey] = []byte("test-cert")
	_, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, gitSecret, certSecret)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)

	nsSecretName := nsReconcilerName + "-" + secretName
	nsCACertSecret := nsReconcilerName + "-" + caCertSecret
	repoDeployment := repoSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		caCertSecretMutator(nsSecretName, nsCACertSecret),
		envVarMutator("HTTPS_PROXY", nsSecretName, "https_proxy"),
		envVarMutator(gitSyncName, nsSecretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsSecretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateCACert(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	caCertSecret := "foo-secret"
	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	certSecret.Data[CACertSecretKey] = []byte("test-cert")
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, gitSecret, certSecret)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	nsSecretName := nsReconcilerName + "-" + secretName
	repoDeployment := rootSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsSecretName),
		envVarMutator("HTTPS_PROXY", nsSecretName, "https_proxy"),
		envVarMutator(gitSyncName, nsSecretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsSecretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	// Unset rs.Spec.CACertSecretRef
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.CACertSecretRef = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("No need to update Deployment")

	// Set rs.Spec.CACertSecretRef
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	nsCACertSecret := nsReconcilerName + "-" + caCertSecret
	updatedRepoDeployment := rootSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		caCertSecretMutator(nsSecretName, nsCACertSecret),
		envVarMutator("HTTPS_PROXY", nsSecretName, "https_proxy"),
		envVarMutator(gitSyncName, nsSecretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsSecretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRepoDeployment)] = updatedRepoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Unset rs.Spec.CACertSecretRef
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.CACertSecretRef = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsSecretName),
		envVarMutator("HTTPS_PROXY", nsSecretName, "https_proxy"),
		envVarMutator(gitSyncName, nsSecretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsSecretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncReconcileWithInvalidCACertSecret(t *testing.T) {
	// reposync setup for testing
	caCertSecret := "foo-secret"
	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch),
		reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName),
		reposyncCACert(caCertSecret))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	fakeClient, _, testReconciler := setupNSReconciler(t, rs, gitSecret, certSecret)

	// reconcile
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	// reposync should be in stalled status
	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	reposync.SetStalled(wantRs, "Validation", fmt.Errorf("caCertSecretRef was set, but %s key is not present in %s Secret", CACertSecretKey, caCertSecret))
	validateRepoSyncStatus(t, wantRs, fakeClient)
}

func TestRepoSyncWithInvalidCACertSecret(t *testing.T) {
	// reposync setup for testing
	caCertSecret := "foo-secret"
	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch),
		reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName),
		reposyncCACert(caCertSecret))
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	_, _, testReconciler := setupNSReconciler(t, rs, gitSecret, certSecret)
	ctx := context.Background()

	// validation should return an error
	err := testReconciler.validateCACertSecret(ctx, rs.Namespace, caCertSecret)
	require.Error(t, err, "Function call should return an error")
	require.Equal(t, fmt.Sprintf("caCertSecretRef was set, but %s key is not present in %s Secret", CACertSecretKey, caCertSecret), err.Error(), "unexpected function error")
}

func TestRepoSyncWithoutCACertSecret(t *testing.T) {
	// reposync setup for testing
	caCertSecret := "foo-secret"
	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch),
		reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName),
		reposyncCACert(caCertSecret))
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")

	// no cert secret is setup to trigger not found error
	_, _, testReconciler := setupNSReconciler(t, rs, gitSecret)
	ctx := context.Background()

	// validation should return a not found error
	err := testReconciler.validateCACertSecret(ctx, rs.Namespace, caCertSecret)
	require.Error(t, err, "Function call should return an error")
	require.Equal(t, fmt.Sprintf("Secret %s not found, create one to allow client connections with CA certificate", caCertSecret), err.Error(), "unexpected function error")
}

func TestRepoSyncCreateWithOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncOverrideGitSyncDepth(5))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	_, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the git sync depth to a positive value
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	var depth int64 = 5
	rs.Spec.SafeOverride().GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test overriding the git sync depth to 0
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	depth = 0
	rs.Spec.SafeOverride().GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.Override.GitSyncDepth to nil.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SafeOverride().GitSyncDepth = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("No need to update Deployment.")
}

func TestRepoSyncCreateWithOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncOverrideReconcileTimeout(metav1.Duration{Duration: 50 * time.Second}))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	_, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the reconcile timeout to 50s
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	reconcileTimeout := metav1.Duration{Duration: 50 * time.Second}
	rs.Spec.SafeOverride().ReconcileTimeout = &reconcileTimeout
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.Override.ReconcileTimeout to nil.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SafeOverride().ReconcileTimeout = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("No need to update Deployment.")
}

func TestRepoSyncCreateWithOverrideAPIServerTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncOverrideAPIServerTimeout(metav1.Duration{Duration: 50 * time.Second}))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	_, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)

	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateOverrideAPIServerTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the api server timeout to 50s
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	reconcileTimeout := metav1.Duration{Duration: 50 * time.Second}
	rs.Spec.SafeOverride().APIServerTimeout = &reconcileTimeout
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.Override.APIServerTimeout to nil.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SafeOverride().APIServerTimeout = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("No need to update Deployment.")
}

func TestRepoSyncSwitchAuthTypes(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	label := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	wantServiceAccount := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.GCPServiceAccountEmail),
		core.Labels(label),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare ServiceAccount.
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(wantServiceAccount): wantServiceAccount}
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	// Test updating RepoSync resources with SSH auth type.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: reposyncSSHKey}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test updating RepoSync resources with None auth type.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = &v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncReconcilerRestart(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Simulate Deployment being scaled down by the user
	deployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"replicas": 0,
			},
		},
	}
	deployment.SetGroupVersionKind(kinds.Deployment())
	patchData, err := json.Marshal(deployment)
	if err != nil {
		t.Fatalf("failed to change unstructured to byte array: %v", err)
	}
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(repoDeployment.Namespace).
		Patch(ctx, repoDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to update the deployment, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

// This test reconcilers multiple RepoSyncs with different auth types.
// - rs1: "my-repo-sync", namespace is bookinfo, auth type is ssh.
// - rs2: uses the default "repo-sync" name, namespace is videoinfo, and auth type is gcenode
// - rs3: "my-rs-3", namespace is videoinfo, auth type is gcpserviceaccount
// - rs4: "my-rs-4", namespace is bookinfo, auth type is cookiefile with proxy
// - rs5: "my-rs-5", namespace is bookinfo, auth type is token with proxy
func TestMultipleRepoSyncs(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	ns2 := "videoinfo"
	rs1 := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName1 := namespacedName(rs1.Name, rs1.Namespace)

	rs2 := repoSyncWithGit(ns2, configsync.RepoSyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCENode))
	reqNamespacedName2 := namespacedName(rs2.Name, rs2.Namespace)

	rs3 := repoSyncWithGit(ns2, "my-rs-3", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName3 := namespacedName(rs3.Name, rs3.Namespace)

	rs4 := repoSyncWithGit(reposyncNs, "my-rs-4", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthCookieFile), reposyncSecretRef(reposyncCookie))
	secret4 := secretObjWithProxy(t, reposyncCookie, "cookie_file", core.Namespace(rs4.Namespace))
	reqNamespacedName4 := namespacedName(rs4.Name, rs4.Namespace)

	rs5 := repoSyncWithGit(reposyncNs, "my-rs-5", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName))
	reqNamespacedName5 := namespacedName(rs5.Name, rs5.Namespace)
	secret5 := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs5.Namespace))
	secret5.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")

	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs1, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs1.Namespace)))

	nsReconcilerName2 := core.NsReconcilerName(rs2.Namespace, rs2.Name)
	nsReconcilerName3 := core.NsReconcilerName(rs3.Namespace, rs3.Name)
	nsReconcilerName4 := core.NsReconcilerName(rs4.Namespace, rs4.Name)
	nsReconcilerName5 := core.NsReconcilerName(rs5.Namespace, rs5.Name)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs1 := fake.RepoSyncObjectV1Beta1(rs1.Namespace, rs1.Name)
	wantRs1.Spec = rs1.Spec
	wantRs1.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs1, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs1, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs1, fakeClient)

	label1 := map[string]string{
		metadata.SyncNamespaceLabel: rs1.Namespace,
		metadata.SyncNameLabel:      rs1.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	serviceAccount1 := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label1),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(serviceAccount1): serviceAccount1}

	roleBinding1 := rolebinding(
		RepoSyncPermissionsName(),
		core.Namespace(rs1.Namespace),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	roleBinding1.Subjects = addSubjectByName(roleBinding1.Subjects, nsReconcilerName)
	wantRoleBindings := map[core.ID]*rbacv1.RoleBinding{core.IDOf(roleBinding1): roleBinding1}

	repoContainerEnv1 := testReconciler.populateContainerEnvs(ctx, rs1, nsReconcilerName)
	repoDeployment1 := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv1),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment1): repoDeployment1}

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("ServiceAccount, RoleBinding, Deployment successfully created")

	// Test reconciler rs2: repo-sync
	if err := fakeClient.Create(ctx, rs2); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs2 := fake.RepoSyncObjectV1Beta1(rs2.Namespace, rs2.Name)
	wantRs2.Spec = rs2.Spec
	wantRs2.Status.Reconciler = nsReconcilerName2
	reposync.SetReconciling(wantRs2, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName2))
	controllerutil.AddFinalizer(wantRs2, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs2, fakeClient)

	label2 := map[string]string{
		metadata.SyncNamespaceLabel: rs2.Namespace,
		metadata.SyncNameLabel:      rs2.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	repoContainerEnv2 := testReconciler.populateContainerEnvs(ctx, rs2, nsReconcilerName2)
	repoDeployment2 := repoSyncDeployment(
		nsReconcilerName2,
		setServiceAccountName(nsReconcilerName2),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv2),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(repoDeployment2)] = repoDeployment2
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount2 := fake.ServiceAccountObject(
		nsReconcilerName2,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label2),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount2)] = serviceAccount2
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	roleBinding2 := rolebinding(
		RepoSyncPermissionsName(),
		core.Namespace(rs2.Namespace),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	roleBinding2.Subjects = addSubjectByName(roleBinding2.Subjects, nsReconcilerName2)
	wantRoleBindings[core.IDOf(roleBinding2)] = roleBinding2
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and RoleBindings successfully created")

	// Test reconciler rs3: my-rs-3
	if err := fakeClient.Create(ctx, rs3); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs3 := fake.RepoSyncObjectV1Beta1(rs3.Namespace, rs3.Name)
	wantRs3.Spec = rs3.Spec
	wantRs3.Status.Reconciler = nsReconcilerName3
	reposync.SetReconciling(wantRs3, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName3))
	controllerutil.AddFinalizer(wantRs3, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs3, fakeClient)

	label3 := map[string]string{
		metadata.SyncNamespaceLabel: rs3.Namespace,
		metadata.SyncNameLabel:      rs3.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	repoContainerEnv3 := testReconciler.populateContainerEnvs(ctx, rs3, nsReconcilerName3)
	repoDeployment3 := repoSyncDeployment(
		nsReconcilerName3,
		setServiceAccountName(nsReconcilerName3),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv3),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(repoDeployment3)] = repoDeployment3
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount3 := fake.ServiceAccountObject(
		nsReconcilerName3,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs3.Spec.GCPServiceAccountEmail),
		core.Labels(label3),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount3)] = serviceAccount3
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding2.Subjects because rs3 and rs2 are in the same namespace.
	roleBinding2.Subjects = addSubjectByName(roleBinding2.Subjects, nsReconcilerName3)
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and RoleBindings successfully created")

	// Test reconciler rs4: my-rs-4
	if err := fakeClient.Create(ctx, rs4); err != nil {
		t.Fatal(err)
	}
	if err := fakeClient.Create(ctx, secret4); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName4); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs4 := fake.RepoSyncObjectV1Beta1(rs4.Namespace, rs4.Name)
	wantRs4.Spec = rs4.Spec
	wantRs4.Status.Reconciler = nsReconcilerName4
	reposync.SetReconciling(wantRs4, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName4))
	controllerutil.AddFinalizer(wantRs4, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs4, fakeClient)

	label4 := map[string]string{
		metadata.SyncNamespaceLabel: rs4.Namespace,
		metadata.SyncNameLabel:      rs4.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	repoContainerEnv4 := testReconciler.populateContainerEnvs(ctx, rs4, nsReconcilerName4)
	repoDeployment4 := repoSyncDeployment(
		nsReconcilerName4,
		setServiceAccountName(nsReconcilerName4),
		secretMutator(nsReconcilerName4+"-"+reposyncCookie),
		envVarMutator("HTTPS_PROXY", nsReconcilerName4+"-"+reposyncCookie, "https_proxy"),
		containerEnvMutator(repoContainerEnv4),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(repoDeployment4)] = repoDeployment4
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount4 := fake.ServiceAccountObject(
		nsReconcilerName4,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label4),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount4)] = serviceAccount4
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding1.Subjects because rs1 and rs4 are in the same namespace.
	roleBinding1.Subjects = addSubjectByName(roleBinding1.Subjects, nsReconcilerName4)
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and RoleBindings successfully created")

	// Test reconciler rs5: my-rs-5
	if err := fakeClient.Create(ctx, rs5); err != nil {
		t.Fatal(err)
	}
	if err := fakeClient.Create(ctx, secret5); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName5); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs5 := fake.RepoSyncObjectV1Beta1(rs5.Namespace, rs5.Name)
	wantRs5.Spec = rs5.Spec
	wantRs5.Status.Reconciler = nsReconcilerName5
	reposync.SetReconciling(wantRs5, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName5))
	controllerutil.AddFinalizer(wantRs5, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs5, fakeClient)

	label5 := map[string]string{
		metadata.SyncNamespaceLabel: rs5.Namespace,
		metadata.SyncNameLabel:      rs5.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	repoContainerEnv5 := testReconciler.populateContainerEnvs(ctx, rs5, nsReconcilerName5)
	repoDeployment5 := repoSyncDeployment(
		nsReconcilerName5,
		setServiceAccountName(nsReconcilerName5),
		secretMutator(nsReconcilerName5+"-"+secretName),
		envVarMutator("HTTPS_PROXY", nsReconcilerName5+"-"+secretName, "https_proxy"),
		envVarMutator(gitSyncName, nsReconcilerName5+"-"+secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsReconcilerName5+"-"+secretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnv5),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(repoDeployment5)] = repoDeployment5
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	serviceAccount5 := fake.ServiceAccountObject(
		nsReconcilerName5,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label5),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount5)] = serviceAccount5
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding1.Subjects because rs1 and rs5 are in the same namespace.
	roleBinding1.Subjects = addSubjectByName(roleBinding1.Subjects, nsReconcilerName5)
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

	// Test updating Deployment resources for rs1: my-repo-sync
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs1), rs1); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs1.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs1); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	reposync.SetReconciling(wantRs1, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs1, fakeClient)

	repoContainerEnv1 = testReconciler.populateContainerEnvs(ctx, rs1, nsReconcilerName)
	repoDeployment1 = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv1),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment1)] = repoDeployment1

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test updating Deployment resources for rs2: repo-sync
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs2), rs2); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs2.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs2); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	reposync.SetReconciling(wantRs2, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName2))
	validateRepoSyncStatus(t, wantRs2, fakeClient)

	repoContainerEnv2 = testReconciler.populateContainerEnvs(ctx, rs2, nsReconcilerName2)
	repoDeployment2 = repoSyncDeployment(
		nsReconcilerName2,
		setServiceAccountName(nsReconcilerName2),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv2),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment2)] = repoDeployment2

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test updating Deployment resources for rs3: my-rs-3
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs3), rs3); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs3.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs3); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	reposync.SetReconciling(wantRs3, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName3))
	validateRepoSyncStatus(t, wantRs3, fakeClient)

	repoContainerEnv3 = testReconciler.populateContainerEnvs(ctx, rs3, nsReconcilerName3)
	repoDeployment3 = repoSyncDeployment(
		nsReconcilerName3,
		setServiceAccountName(nsReconcilerName3),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv3),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment3)] = repoDeployment3
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully updated")

	// Test garbage collecting RoleBinding after all RepoSyncs are deleted
	rs1.ResourceVersion = "" // Skip ResourceVersion validation
	if err := fakeClient.Delete(ctx, rs1); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateResourceDeleted(core.IDOf(rs1), fakeClient); err != nil {
		t.Error(err)
	}

	// Subject for rs1 is removed from RoleBinding.Subjects
	roleBinding1.Subjects = deleteSubjectByName(roleBinding1.Subjects, nsReconcilerName)
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	validateRepoGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName, v1beta1.GetSecretName(rs1.Spec.Git.SecretRef))
	if t.Failed() {
		t.FailNow()
	}

	rs2.ResourceVersion = "" // Skip ResourceVersion validation
	if err := fakeClient.Delete(ctx, rs2); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateResourceDeleted(core.IDOf(rs2), fakeClient); err != nil {
		t.Error(err)
	}

	// Subject for rs2 is removed from RoleBinding.Subjects
	roleBinding2.Subjects = deleteSubjectByName(roleBinding2.Subjects, nsReconcilerName2)
	validateRoleBindings(t, wantRoleBindings, fakeClient)

	validateRepoGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName2, v1beta1.GetSecretName(rs2.Spec.Git.SecretRef))
	if t.Failed() {
		t.FailNow()
	}

	rs3.ResourceVersion = "" // Skip ResourceVersion validation
	if err := fakeClient.Delete(ctx, rs3); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateResourceDeleted(core.IDOf(rs3), fakeClient); err != nil {
		t.Error(err)
	}

	// roleBinding2 is deleted because there are no more RepoSyncs in the namespace.
	if err := validateResourceDeleted(core.IDOf(roleBinding2), fakeClient); err != nil {
		t.Error(err)
	}
	delete(wantRoleBindings, core.IDOf(roleBinding2))
	validateRepoGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName3, v1beta1.GetSecretName(rs3.Spec.Git.SecretRef))
	if t.Failed() {
		t.FailNow()
	}

	rs4.ResourceVersion = "" // Skip ResourceVersion validation
	if err := fakeClient.Delete(ctx, rs4); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName4); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateResourceDeleted(core.IDOf(rs4), fakeClient); err != nil {
		t.Error(err)
	}

	// Subject for rs4 is removed from RoleBinding.Subjects
	roleBinding1.Subjects = deleteSubjectByName(roleBinding1.Subjects, nsReconcilerName4)
	validateRoleBindings(t, wantRoleBindings, fakeClient)
	validateRepoGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName4, v1beta1.GetSecretName(rs4.Spec.Git.SecretRef))
	if t.Failed() {
		t.FailNow()
	}

	rs5.ResourceVersion = "" // Skip ResourceVersion validation
	if err := fakeClient.Delete(ctx, rs5); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName5); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateResourceDeleted(core.IDOf(rs5), fakeClient); err != nil {
		t.Error(err)
	}

	// Verify the RoleBinding is deleted after all RepoSyncs are deleted in the namespace.
	if err := validateResourceDeleted(core.IDOf(roleBinding1), fakeClient); err != nil {
		t.Error(err)
	}
	validateRepoGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName5, v1beta1.GetSecretName(rs5.Spec.Git.SecretRef))
}

func validateRepoGeneratedResourcesDeleted(t *testing.T, fakeClient *syncerFake.Client, reconcilerName, secretRefName string) {
	t.Helper()

	// Verify deployment is deleted.
	deployment := fake.DeploymentObject(core.Namespace(nsReconcilerKey.Namespace), core.Name(reconcilerName))
	if err := validateResourceDeleted(core.IDOf(deployment), fakeClient); err != nil {
		t.Error(err)
	}

	// Verify service account is deleted.
	serviceAccount := fake.ServiceAccountObject(reconcilerName, core.Namespace(nsReconcilerKey.Namespace))
	if err := validateResourceDeleted(core.IDOf(serviceAccount), fakeClient); err != nil {
		t.Error(err)
	}

	// Verify the copied secret is deleted for RepoSync.
	if strings.HasPrefix(reconcilerName, core.NsReconcilerPrefix) {
		s := fake.SecretObject(ReconcilerResourceName(reconcilerName, secretRefName), core.Namespace(nsReconcilerKey.Namespace))
		if err := validateResourceDeleted(core.IDOf(s), fakeClient); err != nil {
			t.Error(err)
		}
	}
}

func TestMapSecretToRepoSyncs(t *testing.T) {
	testSecretName := "ssh-test"
	caCertSecret := "cert-pub"
	rs1 := repoSyncWithGit("ns1", "rs1", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	rs2 := repoSyncWithGit("ns1", "rs2", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	rs3 := repoSyncWithGit("ns1", "rs3", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(testSecretName))
	rs4 := repoSyncWithGit("ns1", "rs4", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthNone), reposyncCACert(caCertSecret))

	ns1rs1ReconcilerName := core.NsReconcilerName(rs1.Namespace, rs1.Name)
	ns1rs4ReconcilerName := core.NsReconcilerName(rs4.Namespace, rs4.Name)
	serviceAccountToken := ns1rs1ReconcilerName + "-token-p29b5"
	serviceAccount := fake.ServiceAccountObject(ns1rs1ReconcilerName, core.Namespace(nsReconcilerKey.Namespace))
	serviceAccount.Secrets = []corev1.ObjectReference{{Name: serviceAccountToken}}

	testCases := []struct {
		name   string
		secret client.Object
		want   []reconcile.Request
	}{
		{
			name:   "A secret from a namespace that has no RepoSync",
			secret: fake.SecretObject("s1", core.Namespace("default")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A secret from the %s namespace NOT starting with %s", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject("s1", core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, but no corresponding RepoSync",
				nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ReconcilerResourceName(core.NsReconcilerName("any-ns", "any-rs"), reposyncSSHKey),
				core.Namespace(nsReconcilerKey.Namespace),
			),
			want: nil,
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, with a mapping RepoSync",
				nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ReconcilerResourceName(ns1rs1ReconcilerName, reposyncSSHKey),
				core.Namespace(nsReconcilerKey.Namespace),
			),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name: fmt.Sprintf("A caCertSecretRef from the %s namespace starting with %s, with a mapping RepoSync",
				configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ReconcilerResourceName(ns1rs4ReconcilerName, caCertSecret),
				core.Namespace(configsync.ControllerNamespace),
			),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs4",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, including `-token-`, but no service account",
				configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ns1rs1ReconcilerName+"-token-123456",
				core.Namespace(nsReconcilerKey.Namespace),
			),
			want: nil,
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, including `-token-`, with a mapping service account",
				nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(serviceAccountToken, core.Namespace(nsReconcilerKey.Namespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:   "A secret from the ns1 namespace with no RepoSync found",
			secret: fake.SecretObject(reposyncSSHKey, core.Namespace("any-ns")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A secret %s from the ns1 namespace with mapping RepoSyncs", reposyncSSHKey),
			secret: fake.SecretObject(reposyncSSHKey, core.Namespace("ns1")),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs2",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:   fmt.Sprintf("A secret %s from the ns1 namespace with mapping RepoSyncs", testSecretName),
			secret: fake.SecretObject(testSecretName, core.Namespace("ns1")),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs3",
						Namespace: "ns1",
					},
				},
			},
		},
		{
			name:   fmt.Sprintf("A caCertSecretRef %s from the ns1 namespace with mapping RepoSyncs", caCertSecret),
			secret: fake.SecretObject(caCertSecret, core.Namespace("ns1")),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs4",
						Namespace: "ns1",
					},
				},
			},
		},
	}

	_, _, testReconciler := setupNSReconciler(t, rs1, rs2, rs3, rs4, serviceAccount)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := testReconciler.mapSecretToRepoSyncs(tc.secret)
			if len(tc.want) != len(result) {
				t.Fatalf("%s: expected %d requests, got %d", tc.name, len(tc.want), len(result))
			}
			for _, wantReq := range tc.want {
				found := false
				for _, gotReq := range result {
					if diff := cmp.Diff(wantReq, gotReq); diff == "" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("%s: expected reques %s doesn't exist in the got requests: %v", tc.name, wantReq, result)
				}
			}
		})
	}
}

func TestMapObjectToRepoSync(t *testing.T) {
	rs1 := repoSyncWithGit("ns1", "rs1", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	ns1rs1ReconcilerName := core.NsReconcilerName(rs1.Namespace, rs1.Name)
	rs2 := repoSyncWithGit("ns2", "rs2", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	rsRoleBindingName := RepoSyncPermissionsName()

	testCases := []struct {
		name   string
		object client.Object
		want   []reconcile.Request
	}{
		// Deployment
		{
			name:   "A deployment from the default namespace",
			object: fake.DeploymentObject(core.Name("deploy1"), core.Namespace("default")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A deployment from the %s namespace NOT starting with %s", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name("deploy1"), core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A deployment from the %s namespace starting with %s, no mapping RepoSync", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name(core.NsReconcilerName("any", "any")), core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A deployment from the %s namespace starting with %s, with mapping RepoSync", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name(ns1rs1ReconcilerName), core.Namespace(nsReconcilerKey.Namespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
			},
		},
		// ServiceAccount
		{
			name:   "A serviceaccount from the default namespace",
			object: fake.ServiceAccountObject("sa1", core.Namespace("default")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A serviceaccount from the %s namespace NOT starting with %s", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject("sa1", core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A serviceaccount from the %s namespace starting with %s, no mapping RepoSync", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject(core.NsReconcilerName("any", "any"), core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A serviceaccount from the %s namespace starting with %s, with mapping RepoSync", nsReconcilerKey.Namespace, core.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject(ns1rs1ReconcilerName, core.Namespace(nsReconcilerKey.Namespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
			},
		},
		// RoleBinding
		{
			name:   "A rolebinding from the default namespace",
			object: fake.RoleBindingObject(core.Name("rb1"), core.Namespace("default")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A rolebinding from the %s namespace, different from %s", nsReconcilerKey.Namespace, rsRoleBindingName),
			object: fake.RoleBindingObject(core.Name("any"), core.Namespace(nsReconcilerKey.Namespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A rolebinding from the %s namespace, same as %s", nsReconcilerKey.Namespace, rsRoleBindingName),
			object: fake.RoleBindingObject(core.Name(rsRoleBindingName), core.Namespace(nsReconcilerKey.Namespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs1",
						Namespace: "ns1",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs2",
						Namespace: "ns2",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, testReconciler := setupNSReconciler(t, rs1, rs2)

			result := testReconciler.mapObjectToRepoSync(tc.object)
			if len(tc.want) != len(result) {
				t.Fatalf("%s: expected %d requests, got %d", tc.name, len(tc.want), len(result))
			}
			for _, wantReq := range tc.want {
				found := false
				for _, gotReq := range result {
					if diff := cmp.Diff(wantReq, gotReq); diff == "" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("%s: expected reques %s doesn't exist in the got requests: %v", tc.name, wantReq, result)
				}
			}
		})
	}
}

func TestInjectFleetWorkloadIdentityCredentialsToRepoSync(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))
	testReconciler.membership = &hubv1.Membership{
		Spec: hubv1.MembershipSpec{
			Owner: hubv1.MembershipOwner{
				ID: "fakeId",
			},
		},
	}
	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)

	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		gceNodeMutator(),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	workloadIdentityPool := "test-gke-dev.svc.id.goog"
	testReconciler.membership = &hubv1.Membership{
		Spec: hubv1.MembershipSpec{
			WorkloadIdentityPool: workloadIdentityPool,
			IdentityProvider:     "https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster",
		},
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(nsReconcilerName),
		fleetWorkloadIdentityMutator(workloadIdentityPool),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments = map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	// Test updating RepoSync resources with SSH auth type.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: reposyncSSHKey}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test updating RepoSync resources with None auth type.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = &v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncWithHelm(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = helmParsedDeployment
	secretName := "helm-secret"
	// Test creating RepoSync resources with Token auth type
	rs := repoSyncWithHelm(reposyncNs, reposyncName,
		reposyncHelmAuthType(configsync.AuthToken), reposyncHelmSecretRef(secretName))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	helmSecret := secretObj(t, secretName, configsync.AuthToken, v1beta1.HelmSource, core.Namespace(rs.Namespace))
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, helmSecret)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	repoContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)

	repoDeployment := rootSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		helmSecretMutator(nsReconcilerName+"-"+secretName),
		envVarMutator(helmSyncName, nsReconcilerName+"-"+secretName, "username"),
		envVarMutator(helmSyncPassword, nsReconcilerName+"-"+secretName, "password"),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test updating RepoSync resources with None auth type
	rs = repoSyncWithHelm(reposyncNs, reposyncName,
		reposyncHelmAuthType(configsync.AuthNone))
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneHelmContainers()),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	t.Log("Test overriding the cpu request and memory limits of the helm-sync container")
	overrideHelmSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.HelmSync,
			CPURequest:    resource.MustParse("200m"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideHelmSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneHelmContainers()),
		containerResourcesMutator(overrideHelmSyncResources),
		containerEnvMutator(repoContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncWithOCI(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithOCI(reposyncNs, reposyncName, reposyncOCIAuthType(configsync.AuthNone))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs)

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	label := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
		metadata.SyncKindLabel:      testReconciler.syncKind,
	}

	wantServiceAccount := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare ServiceAccount.
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(wantServiceAccount): wantServiceAccount}
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	t.Log("Test updating RepoSync resources with gcenode auth type.")
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Oci.Auth = configsync.AuthGCENode
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// compare ServiceAccount.
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	t.Log("Test updating RepoSync resources with gcpserviceaccount auth type.")
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Oci.Auth = configsync.AuthGCPServiceAccount
	rs.Spec.Oci.GCPServiceAccountEmail = gcpSAEmail
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)

	wantServiceAccount = fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.Oci.GCPServiceAccountEmail),
		core.Labels(label),
		core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
	)
	// compare ServiceAccount.
	wantServiceAccounts[core.IDOf(wantServiceAccount)] = wantServiceAccount
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	t.Log("Test FWI")
	workloadIdentityPool := "test-gke-dev.svc.id.goog"
	testReconciler.membership = &hubv1.Membership{
		Spec: hubv1.MembershipSpec{
			WorkloadIdentityPool: workloadIdentityPool,
			IdentityProvider:     "https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster",
		},
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(nsReconcilerName),
		fwiOciMutator(workloadIdentityPool),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	t.Log("Test overriding the memory requests and limits of the oci-sync container")
	overrideOciSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.OciSync,
			MemoryRequest: resource.MustParse("800m"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		Resources: overrideOciSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(nsReconcilerName),
		fwiOciMutator(workloadIdentityPool),
		containerResourcesMutator(overrideOciSyncResources),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("5"), setGeneration(5),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncSpecValidation(t *testing.T) {
	rs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, _, testReconciler := setupNSReconciler(t, rs)

	// Verify unsupported source type
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	reposync.SetStalled(wantRs, "Validation", validate.InvalidSourceType(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing Git
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.GitSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingGitSpec(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing Oci
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingOciSpec(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingHelmSpec(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing OCI image
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingOciImage(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify invalid OCI Auth
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{Image: ociImage}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.InvalidOciAuthType(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm repo
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Oci = nil
	rs.Spec.Helm = &v1beta1.HelmRepoSync{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingHelmRepo(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm chart
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.MissingHelmChart(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify invalid Helm Auth
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo, Chart: helmChart}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	reposync.SetStalled(wantRs, "Validation", validate.InvalidHelmAuthType(rs))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify valid OCI spec
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Git = nil
	rs.Spec.Helm = nil
	rs.Spec.Oci = &v1beta1.Oci{Image: ociImage, Auth: configsync.AuthNone}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	// Clear the stalled condition
	rs.Status = v1beta1.RepoSyncStatus{}
	if err := fakeClient.Status().Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	wantRs.Status.Conditions = nil // clear the stalled condition
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	// verify valid Helm spec
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Git = nil
	rs.Spec.Oci = nil
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo, Chart: helmChart, Auth: configsync.AuthNone}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	// Clear the stalled condition
	rs.Status = v1beta1.RepoSyncStatus{}
	if err := fakeClient.Status().Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	wantRs.Status.Conditions = nil // clear the stalled condition
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)
}

func TestRepoSyncReconcileStaleClientCache(t *testing.T) {
	rs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, _, testReconciler := setupNSReconciler(t, rs)
	ctx := context.Background()

	rs = fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	err := fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	oldRS := rs.DeepCopy()

	// Reconcile should succeed and update the RepoSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect Stalled condition with True status, because the RepoSync is invalid
	rs = fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "KNV1061: RepoSyncs must specify spec.sourceType", "unexpected Stalled condition message")

	// Simulate stale cache (rollback to previous resource version)
	err = fakeClient.Storage().TestPut(oldRS)
	require.NoError(t, err)

	// Expect next Reconcile to succeed but NOT update the RepoSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Simulate cache update from watch event (roll forward to the latest resource version)
	err = fakeClient.Storage().TestPut(rs)
	require.NoError(t, err)

	// Reconcile should succeed but NOT update the RepoSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect the same Stalled condition error message
	rs = fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition = reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "KNV1061: RepoSyncs must specify spec.sourceType", "unexpected Stalled condition message")

	// Simulate a spec update, with ResourceVersion updated by the apiserver
	rs = fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.ResourceVersion = "2" // doesn't need to be increasing or even numeric
	err = fakeClient.Update(ctx, rs)
	require.NoError(t, err, "unexpected Update error")

	// Reconcile should succeed and update the RepoSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect Stalled condition with True status, because the RepoSync is differently invalid
	rs = fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition = reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "RepoSyncs must specify spec.git when spec.sourceType is \"git\"", "unexpected Stalled condition message")
}

func TestPopulateRepoContainerEnvs(t *testing.T) {
	defaults := map[string]map[string]string{
		reconcilermanager.HydrationController: {
			reconcilermanager.HydrationPollingPeriod: hydrationPollingPeriod.String(),
			reconcilermanager.NamespaceNameKey:       reposyncNs,
			reconcilermanager.ReconcilerNameKey:      nsReconcilerName,
			reconcilermanager.ScopeKey:               reposyncNs,
			reconcilermanager.SourceTypeKey:          string(gitSource),
			reconcilermanager.SyncDirKey:             reposyncDir,
		},
		reconcilermanager.Reconciler: {
			reconcilermanager.ClusterNameKey:          testCluster,
			reconcilermanager.ScopeKey:                reposyncNs,
			reconcilermanager.SyncNameKey:             reposyncName,
			reconcilermanager.NamespaceNameKey:        reposyncNs,
			reconcilermanager.SyncGenerationKey:       "1",
			reconcilermanager.ReconcilerNameKey:       nsReconcilerName,
			reconcilermanager.SyncDirKey:              reposyncDir,
			reconcilermanager.SourceRepoKey:           reposyncRepo,
			reconcilermanager.SourceTypeKey:           string(gitSource),
			reconcilermanager.StatusMode:              "enabled",
			reconcilermanager.SourceBranchKey:         "master",
			reconcilermanager.SourceRevKey:            "HEAD",
			reconcilermanager.APIServerTimeout:        restconfig.DefaultTimeout.String(),
			reconcilermanager.ReconcileTimeout:        "5m0s",
			reconcilermanager.ReconcilerPollingPeriod: "50ms",
			reconcilermanager.RenderingEnabled:        "false",
		},
		reconcilermanager.GitSync: {
			"GIT_KNOWN_HOSTS": "false",
			"GIT_SYNC_REPO":   reposyncRepo,
			"GIT_SYNC_DEPTH":  "1",
			"GIT_SYNC_WAIT":   "15.000000",
		},
	}

	createEnv := func(overrides map[string]map[string]string) map[string][]corev1.EnvVar {
		envs := map[string]map[string]string{}

		for container, env := range defaults {
			envs[container] = map[string]string{}
			for k, v := range env {
				envs[container][k] = v
			}
		}
		for container, env := range overrides {
			if _, ok := envs[container]; !ok {
				envs[container] = map[string]string{}
			}
			for k, v := range env {
				envs[container][k] = v
			}
		}

		result := map[string][]corev1.EnvVar{}
		for container, env := range envs {
			result[container] = []corev1.EnvVar{}
			for k, v := range env {
				result[container] = append(result[container], corev1.EnvVar{Name: k, Value: v})
			}
		}
		return result
	}

	testCases := []struct {
		name     string
		repoSync *v1beta1.RepoSync
		expected map[string][]corev1.EnvVar
	}{
		{
			name: "no override uses default value",
			repoSync: repoSyncWithGit(reposyncNs, reposyncName,
				reposyncRenderingRequired(false),
			),
			expected: createEnv(map[string]map[string]string{}),
		},
		{
			name: "override uses override value",
			repoSync: repoSyncWithGit(reposyncNs, reposyncName,
				reposyncOverrideAPIServerTimeout(metav1.Duration{Duration: 40 * time.Second}),
				reposyncRenderingRequired(false),
			),
			expected: createEnv(map[string]map[string]string{
				reconcilermanager.Reconciler: {reconcilermanager.APIServerTimeout: "40s"},
			}),
		},
		{
			name: "rendering-required annotation sets env var",
			repoSync: repoSyncWithGit(reposyncNs, reposyncName,
				reposyncRenderingRequired(true),
			),
			expected: createEnv(map[string]map[string]string{
				reconcilermanager.Reconciler: {reconcilermanager.RenderingEnabled: "true"},
			}),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, testReconciler := setupNSReconciler(t, tc.repoSync, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(tc.repoSync.Namespace)))

			env := testReconciler.populateContainerEnvs(ctx, tc.repoSync, nsReconcilerName)

			for container, vars := range env {
				if diff := cmp.Diff(tc.expected[container], vars, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b corev1.EnvVar) bool { return a.Name < b.Name })); diff != "" {
					t.Errorf("%s/%s: unexpected env; diff: %s", tc.name, container, diff)
				}
			}
		})
	}
}

func TestUpdateNamespaceReconcilerLogLevelWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithGit(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantRs := fake.RepoSyncObjectV1Beta1(reposyncNs, reposyncName)
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = nsReconcilerName
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	controllerutil.AddFinalizer(wantRs, metadata.ReconcilerManagerFinalizer)
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	overrideLogLevel := []v1beta1.ContainerLogLevelOverride{
		{
			ContainerName: reconcilermanager.Reconciler,
			LogLevel:      5,
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			LogLevel:      7,
		},
		{
			ContainerName: reconcilermanager.GitSync,
			LogLevel:      9,
		},
	}

	containerArgs := map[string][]string{
		"reconciler": {
			"-v=5",
		},
		"hydration-controller": {
			"-v=7",
		},
		"git-sync": {
			"-v=9",
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the repo sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.OverrideSpec{
		LogLevels: overrideLogLevel,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantRs.Spec = rs.Spec
	reposync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", nsReconcilerName))
	validateRepoSyncStatus(t, wantRs, fakeClient)

	repoContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerArgsMutator(containerArgs),
		containerEnvMutator(repoContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
}

func validateRepoSyncStatus(t *testing.T, want *v1beta1.RepoSync, fakeClient *syncerFake.Client) {
	t.Helper()

	key := client.ObjectKeyFromObject(want)
	got := &v1beta1.RepoSync{}
	ctx := context.Background()
	err := fakeClient.Get(ctx, key, got)
	require.NoError(t, err, "RepoSync[%s] not found", key)

	asserter := testutil.NewAsserter(
		cmpopts.IgnoreFields(v1beta1.RepoSyncCondition{}, "LastUpdateTime", "LastTransitionTime"))
	// cmpopts.SortSlices(func(x, y v1beta1.RepoSyncCondition) bool { return x.Message < y.Message })
	asserter.Equal(t, want.Status.Conditions, got.Status.Conditions, "Unexpected status conditions")
}

func validateServiceAccounts(wants map[core.ID]*corev1.ServiceAccount, fakeClient *syncerFake.Client) error {
	for id, want := range wants {
		key := id.ObjectKey
		got := &corev1.ServiceAccount{}
		ctx := context.Background()
		err := fakeClient.Get(ctx, key, got)
		if err != nil {
			return errors.Errorf("ServiceAccount[%s] not found", key)
		}

		if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
			return errors.Errorf("ServiceAccount[%s/%s] diff: %s", got.Namespace, got.Name, diff)
		}
	}
	return nil
}

func validateRoleBindings(t *testing.T, wants map[core.ID]*rbacv1.RoleBinding, fakeClient *syncerFake.Client) {
	t.Helper()

	for id, want := range wants {
		key := id.ObjectKey
		got := &rbacv1.RoleBinding{}
		ctx := context.Background()
		err := fakeClient.Get(ctx, key, got)
		require.NoError(t, err, "RoleBinding[%s] not found", key)

		testutil.AssertEqual(t, want.Subjects, got.Subjects, "RoleBinding[%s] unexpected subjects", key)
	}
}

func validateClusterRoleBinding(want *rbacv1.ClusterRoleBinding, fakeClient *syncerFake.Client) error {
	key := client.ObjectKeyFromObject(want)
	got := &rbacv1.ClusterRoleBinding{}
	ctx := context.Background()
	err := fakeClient.Get(ctx, key, got)
	if err != nil {
		return errors.Errorf("ClusterRoleBinding[%s] not found", key)
	}
	if len(want.Subjects) != len(got.Subjects) {
		return errors.Errorf("ClusterRoleBinding[%s] has unexpected number of subjects, expected %d, got %d",
			key, len(want.Subjects), len(got.Subjects))
	}
	for _, ws := range want.Subjects {
		for _, gs := range got.Subjects {
			if ws.Namespace == gs.Namespace && ws.Name == gs.Name {
				if !reflect.DeepEqual(ws, gs) {
					return errors.Errorf("ClusterRoleBinding[%s] has unexpected subject, expected %v, got %v", key, ws, gs)
				}
			}
		}
	}
	got.Subjects = want.Subjects
	if diff := cmp.Diff(want, got, cmpopts.EquateEmpty()); diff != "" {
		return errors.Errorf("ClusterRoleBinding[%s] diff: %s", key, diff)
	}
	return nil
}

// validateDeployments validates that important fields in the `wants` deployments match those same fields in the current deployments found in the unstructured Map
func validateDeployments(wants map[core.ID]*appsv1.Deployment, fakeDynamicClient *syncerFake.DynamicClient) error {
	ctx := context.Background()
	for id, want := range wants {
		uObj, err := fakeDynamicClient.Resource(kinds.DeploymentResource()).
			Namespace(id.Namespace).
			Get(ctx, id.Name, metav1.GetOptions{})
		if err != nil {
			return errors.Wrapf(err, "Deployment[%s] not found", id.ObjectKey)
		}
		gotCoreObject, err := kinds.ToTypedObject(uObj, core.Scheme)
		if err != nil {
			return errors.Errorf("Deployment[%s] conversion failed", id.ObjectKey)
		}
		got := gotCoreObject.(*appsv1.Deployment)

		// Compare Deployment ResourceVersion
		if diff := cmp.Diff(want.ResourceVersion, got.ResourceVersion); diff != "" {
			return errors.Errorf("Unexpected Deployment ResourceVersion found for %q. Diff: %v", id, diff)
		}

		// Compare Deployment Generation
		if diff := cmp.Diff(want.Generation, got.Generation); diff != "" {
			return errors.Errorf("Unexpected Deployment Generation found for %q: Diff (- want, + got): %v", id, diff)
		}

		// Compare Deployment Annotations
		if diff := cmp.Diff(want.Annotations, got.Annotations); diff != "" {
			return errors.Errorf("Unexpected Deployment Annotations found for %q. Diff (- want, + got): %v", id, diff)
		}

		// Compare Deployment Template Annotations.
		if diff := cmp.Diff(want.Spec.Template.Annotations, got.Spec.Template.Annotations); diff != "" {
			return errors.Errorf("Unexpected Template Annotations found for %q. Diff (- want, + got): %v", id, diff)
		}

		// Compare ServiceAccountName.
		if diff := cmp.Diff(want.Spec.Template.Spec.ServiceAccountName, got.Spec.Template.Spec.ServiceAccountName); diff != "" {
			return errors.Errorf("Unexpected ServiceAccountName for %q. Diff (- want, + got): %v", id, diff)
		}

		// Compare Replicas
		if *want.Spec.Replicas != *got.Spec.Replicas {
			return errors.Errorf("Unexpected Replicas for %q. want %d, got %d", id, *want.Spec.Replicas, *got.Spec.Replicas)
		}

		// Compare Containers.
		var wantContainerNames []string
		var gotContainerNames []string
		for _, i := range want.Spec.Template.Spec.Containers {
			wantContainerNames = append(wantContainerNames, i.Name)
		}
		for _, j := range got.Spec.Template.Spec.Containers {
			gotContainerNames = append(gotContainerNames, j.Name)
		}
		if diff := cmp.Diff(wantContainerNames, gotContainerNames, cmpopts.SortSlices(func(x, y string) bool { return x < y })); diff != "" {
			return errors.Errorf("Unexpected containers for %q, want %s, got %s", id,
				wantContainerNames, gotContainerNames)
		}
		for _, i := range want.Spec.Template.Spec.Containers {
			for _, j := range got.Spec.Template.Spec.Containers {
				if i.Name == j.Name {
					// Compare EnvFrom fields in the container.
					if diff := cmp.Diff(i.EnvFrom, j.EnvFrom,
						cmpopts.SortSlices(func(x, y corev1.EnvFromSource) bool { return x.ConfigMapRef.Name < y.ConfigMapRef.Name })); diff != "" {
						return errors.Errorf("Unexpected configMapRef found for the %q container of %q, diff %s", i.Name, id, diff)
					}
					// Compare VolumeMount fields in the container.
					if diff := cmp.Diff(i.VolumeMounts, j.VolumeMounts,
						cmpopts.SortSlices(func(x, y corev1.VolumeMount) bool { return x.Name < y.Name })); diff != "" {
						return errors.Errorf("Unexpected volumeMount found for the %q container of %q, diff %s", i.Name, id, diff)
					}

					// Compare Env fields in the container.
					if diff := cmp.Diff(i.Env, j.Env,
						cmpopts.SortSlices(func(x, y corev1.EnvVar) bool { return x.Name < y.Name })); diff != "" {
						return errors.Errorf("Unexpected EnvVar found for the %q container of %q, diff %s", i.Name, id, diff)
					}

					// Compare Resources fields in the container.
					if diff := cmp.Diff(i.Resources, j.Resources); diff != "" {
						return errors.Errorf("Unexpected resources found for the %q container of %q, diff %s", i.Name, id, diff)
					}

					// Compare Args
					if diff := cmp.Diff(i.Args, j.Args); diff != "" {
						return errors.Errorf("Unexpected args found for the %q container of %q, diff %s", i.Name, id, diff)
					}
				}
			}
		}

		// Compare Volumes
		var wantVolumeNames []string
		var gotVolumeNames []string
		for _, i := range want.Spec.Template.Spec.Volumes {
			wantVolumeNames = append(wantVolumeNames, i.Name)
		}
		for _, j := range got.Spec.Template.Spec.Volumes {
			gotVolumeNames = append(gotVolumeNames, j.Name)
		}
		if diff := cmp.Diff(wantVolumeNames, gotVolumeNames, cmpopts.SortSlices(func(x, y string) bool { return x < y })); diff != "" {
			return errors.Errorf("Unexpected volumes for %q, want %s, got %s", id,
				wantVolumeNames, gotVolumeNames)
		}
		for _, wantVolume := range want.Spec.Template.Spec.Volumes {
			for _, gotVolume := range got.Spec.Template.Spec.Volumes {
				if wantVolume.Name == gotVolume.Name {
					// Compare VolumeSource
					if diff := cmp.Diff(wantVolume.VolumeSource, gotVolume.VolumeSource); diff != "" {
						return errors.Errorf("Unexpected volumeSource for the %q volume of %q, diff %s", gotVolume.Name, id, diff)
					}
				}
			}
		}

		// Compare Deployment ResourceVersion
		if diff := cmp.Diff(want.ResourceVersion, got.ResourceVersion); diff != "" {
			return errors.Errorf("Unexpected Deployment ResourceVersion found for %q. Diff (- want, + got): %v", id, diff)
		}
	}
	return nil
}

func validateResourceDeleted(id core.ID, fakeClient *syncerFake.Client) error {
	mapping, err := fakeClient.RESTMapper().RESTMapping(id.GroupKind)
	if err != nil {
		return err
	}

	key := id.ObjectKey
	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(mapping.GroupVersionKind)
	ctx := context.Background()
	err = fakeClient.Get(ctx, key, got)
	if apierrors.IsNotFound(err) {
		return nil // success!
	} else if err != nil {
		return err
	}
	return errors.Errorf("resource %s still exists: %#v", id, got)
}

func addSubjectByName(subjects []rbacv1.Subject, name string) []rbacv1.Subject {
	return addSubject(subjects, newSubject(name, configsync.ControllerNamespace, "ServiceAccount"))
}

func deleteSubjectByName(subjects []rbacv1.Subject, name string) []rbacv1.Subject {
	return removeSubject(subjects, newSubject(name, configsync.ControllerNamespace, "ServiceAccount"))
}

func namespacedName(name, namespace string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
}

func repoSyncDeployment(reconcilerName string, muts ...depMutator) *appsv1.Deployment {
	dep := fake.DeploymentObject(
		core.Namespace(v1.NSConfigManagementSystem),
		core.Name(reconcilerName),
	)
	var replicas int32 = 1
	dep.Spec.Replicas = &replicas
	dep.Annotations = nil
	dep.ResourceVersion = "1"
	for _, mut := range muts {
		mut(dep)
	}
	return dep
}

// newDeploymentCondition creates a new deployment condition.
func newDeploymentCondition(condType appsv1.DeploymentConditionType, status corev1.ConditionStatus, reason, message string) *appsv1.DeploymentCondition {
	return &appsv1.DeploymentCondition{
		Type:               condType,
		Status:             status,
		LastUpdateTime:     metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}
