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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
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

// Set in init.
var filesystemPollingPeriod time.Duration
var hydrationPollingPeriod time.Duration
var nsReconcilerName = reconciler.NsReconcilerName(reposyncNs, reposyncName)

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
				Volumes:    deploymentSecretVolumes("git-creds"),
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

func reposyncSecretType(auth string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Auth = auth
	}
}

func reposyncOCIAuthType(auth string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Oci.Auth = auth
	}
}

func reposyncSecretRef(ref string) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Git.SecretRef = v1beta1.SecretReference{Name: ref}
	}
}

func reposyncGCPSAEmail(email string) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.GCPServiceAccountEmail = email
	}
}

func reposyncOverrideResources(containers []v1beta1.ContainerResourcesSpec) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Override = v1beta1.OverrideSpec{
			Resources: containers,
		}
	}
}

func reposyncOverrideGitSyncDepth(depth int64) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Override.GitSyncDepth = &depth
	}
}

func reposyncOverrideReconcileTimeout(reconcileTimeout metav1.Duration) func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.Override.ReconcileTimeout = &reconcileTimeout
	}
}

func reposyncNoSSLVerify() func(*v1beta1.RepoSync) {
	return func(rs *v1beta1.RepoSync) {
		rs.Spec.NoSSLVerify = true
	}
}

func repoSync(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1(ns, name)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1beta1.Git{
		Repo: reposyncRepo,
		Dir:  reposyncDir,
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func repoSyncWithOCI(ns, name string, opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1(ns, name)
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{
		Image: ociImage,
		Dir:   reposyncDir,
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func rolebinding(name, reconcilerName string, opts ...core.MetaMutator) *rbacv1.RoleBinding {
	result := fake.RoleBindingObject(opts...)
	result.Name = name

	result.RoleRef.Name = RepoSyncPermissionsName()
	result.RoleRef.Kind = "ClusterRole"
	result.RoleRef.APIGroup = "rbac.authorization.k8s.io"

	var sub rbacv1.Subject
	sub.Kind = "ServiceAccount"
	sub.Name = reconcilerName
	sub.Namespace = configsync.ControllerNamespace
	result.Subjects = append(result.Subjects, sub)

	return result
}

func setupNSReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *RepoSyncReconciler) {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := rbacv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := admissionregistrationv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	fakeClient := syncerFake.NewClient(t, s, objs...)
	testReconciler := NewRepoSyncReconciler(
		testCluster,
		filesystemPollingPeriod,
		hydrationPollingPeriod,
		fakeClient,
		controllerruntime.Log.WithName("controllers").WithName("RepoSync"),
		s,
	)
	return fakeClient, testReconciler
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

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH),
		reposyncSecretRef(reposyncSSHKey), reposyncOverrideResources(overrideReconcilerAndGitSyncResourceLimits))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerAndGitSyncResourceLimits),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
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

	rs.Spec.Override = v1beta1.OverrideSpec{
		Resources: overrideReconcilerCPUAndGitSyncMemResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerCPUAndGitSyncMemResources),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	rs.Spec.Override = v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestUpdateNamespaceReconcilerWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
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

	rs.Spec.Override = v1beta1.OverrideSpec{
		Resources: overrideReconcilerAndGitSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerAndGitSyncResources),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
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

	rs.Spec.Override = v1beta1.OverrideSpec{
		Resources: overrideReconcilerResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideReconcilerResources),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
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

	rs.Spec.Override = v1beta1.OverrideSpec{
		Resources: overrideGitSyncResources,
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerResourcesMutator(overrideGitSyncResources),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	rs.Spec.Override = v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncCreateWithNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncNoSSLVerify())
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	// Set rs.Spec.NoSSLVerify to false
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("No need to update Deployment")

	// Set rs.Spec.NoSSLVerify to true
	rs.Spec.NoSSLVerify = true
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(updatedRepoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.NoSSLVerify to false
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncCreateWithOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncOverrideGitSyncDepth(5))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	// Test overriding the git sync depth to a positive value
	var depth int64 = 5
	rs.Spec.Override.GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Test overriding the git sync depth to 0
	depth = 0
	rs.Spec.Override.GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.Override.GitSyncDepth to nil.
	rs.Spec.Override.GitSyncDepth = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	rs.Spec.Override = v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("No need to update Deployment.")
}

func TestRepoSyncCreateWithOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey), reposyncOverrideReconcileTimeout(metav1.Duration{Duration: 50 * time.Second}))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRepoSyncUpdateOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	// Test overriding the reconcile timeout to 50s
	reconcileTimeout := metav1.Duration{Duration: 50 * time.Second}
	rs.Spec.Override.ReconcileTimeout = &reconcileTimeout
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	updatedRepoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = updatedRepoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Set rs.Spec.Override.ReconcileTimeout to nil.
	rs.Spec.Override.ReconcileTimeout = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Clear rs.Spec.Override
	rs.Spec.Override = v1beta1.OverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("No need to update Deployment.")
}

func TestRepoSyncSwitchAuthTypes(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantNamespaces := map[string]struct{}{
		rs.Namespace: {},
	}

	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
	}

	wantServiceAccount := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.GCPServiceAccountEmail),
		core.Labels(label),
	)

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		gceNodeMutator(gcpSAEmail),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare ServiceAccount.
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(wantServiceAccount)], wantServiceAccount, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ServiceAccount diff %s", diff)
	}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Resources successfully created")

	// Test updating RepoSync resources with SSH auth type.
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef.Name = reposyncSSHKey
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Test updating RepoSync resources with None auth type.
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncReconcilerRestart(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	// Scale down the Reconciler Deployment to 0 replicas.
	deploymentCoreObject := fakeClient.Objects[core.IDOf(repoDeployment)]
	deployment := deploymentCoreObject.(*appsv1.Deployment)
	*deployment.Spec.Replicas = 0
	if err := fakeClient.Update(ctx, deployment); err != nil {
		t.Fatalf("failed to update the deployment request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
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
	rs1 := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	reqNamespacedName1 := namespacedName(rs1.Name, rs1.Namespace)

	rs2 := repoSync(ns2, configsync.RepoSyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCENode))
	reqNamespacedName2 := namespacedName(rs2.Name, rs2.Namespace)

	rs3 := repoSync(ns2, "my-rs-3", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName3 := namespacedName(rs3.Name, rs3.Namespace)

	rs4 := repoSync(reposyncNs, "my-rs-4", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthCookieFile), reposyncSecretRef(reposyncCookie))
	secret4 := secretObjWithProxy(t, reposyncCookie, "cookie_file", core.Namespace(rs4.Namespace))
	reqNamespacedName4 := namespacedName(rs4.Name, rs4.Namespace)

	rs5 := repoSync(reposyncNs, "my-rs-5", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthToken), reposyncSecretRef(secretName))
	reqNamespacedName5 := namespacedName(rs5.Name, rs5.Namespace)
	secret5 := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs5.Namespace))
	secret5.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")

	fakeClient, testReconciler := setupNSReconciler(t, rs1, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs1.Namespace)))

	nsReconcilerName2 := reconciler.NsReconcilerName(rs2.Namespace, rs2.Name)
	nsReconcilerName3 := reconciler.NsReconcilerName(rs3.Namespace, rs3.Name)
	nsReconcilerName4 := reconciler.NsReconcilerName(rs4.Namespace, rs4.Name)
	nsReconcilerName5 := reconciler.NsReconcilerName(rs5.Namespace, rs5.Name)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantNamespaces := map[string]struct{}{
		rs1.Namespace: {},
	}

	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label1 := map[string]string{
		metadata.SyncNamespaceLabel: rs1.Namespace,
		metadata.SyncNameLabel:      rs1.Name,
	}

	serviceAccount1 := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Label(metadata.SyncNamespaceLabel, label1[metadata.SyncNamespaceLabel]),
		core.Label(metadata.SyncNameLabel, label1[metadata.SyncNameLabel]),
	)
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(serviceAccount1): serviceAccount1}

	roleBinding1 := rolebinding(
		RepoSyncPermissionsName(),
		nsReconcilerName,
		core.Namespace(rs1.Namespace),
	)
	wantRoleBindings := map[core.ID]*rbacv1.RoleBinding{core.IDOf(roleBinding1): roleBinding1}

	repoContainerEnv1 := testReconciler.populateRepoContainerEnvs(ctx, rs1, nsReconcilerName)
	repoDeployment1 := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment1): repoDeployment1}

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("ServiceAccount, RoleBinding, Deployment successfully created")

	// Test reconciler rs2: repo-sync
	if err := fakeClient.Create(ctx, rs2); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantNamespaces[rs2.Namespace] = struct{}{}
	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}
	label2 := map[string]string{
		metadata.SyncNamespaceLabel: rs2.Namespace,
		metadata.SyncNameLabel:      rs2.Name,
	}

	repoContainerEnv2 := testReconciler.populateRepoContainerEnvs(ctx, rs2, nsReconcilerName2)
	repoDeployment2 := repoSyncDeployment(
		nsReconcilerName2,
		setServiceAccountName(nsReconcilerName2),
		gceNodeMutator(""),
		containerEnvMutator(repoContainerEnv2),
	)
	wantDeployments[core.IDOf(repoDeployment2)] = repoDeployment2
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount2 := fake.ServiceAccountObject(
		nsReconcilerName2,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label2),
	)
	wantServiceAccounts[core.IDOf(serviceAccount2)] = serviceAccount2
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	roleBinding2 := rolebinding(
		RepoSyncPermissionsName(),
		nsReconcilerName2,
		core.Namespace(rs2.Namespace),
	)
	wantRoleBindings[core.IDOf(roleBinding2)] = roleBinding2
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}

	t.Log("Deployments, ServiceAccounts, and RoleBindings successfully created")

	// Test reconciler rs3: my-rs-3
	if err := fakeClient.Create(ctx, rs3); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label3 := map[string]string{
		metadata.SyncNamespaceLabel: rs3.Namespace,
		metadata.SyncNameLabel:      rs3.Name,
	}

	repoContainerEnv3 := testReconciler.populateRepoContainerEnvs(ctx, rs3, nsReconcilerName3)
	repoDeployment3 := repoSyncDeployment(
		nsReconcilerName3,
		setServiceAccountName(nsReconcilerName3),
		gceNodeMutator(gcpSAEmail),
		containerEnvMutator(repoContainerEnv3),
	)
	wantDeployments[core.IDOf(repoDeployment3)] = repoDeployment3
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount3 := fake.ServiceAccountObject(
		nsReconcilerName3,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs3.Spec.GCPServiceAccountEmail),
		core.Labels(label3),
	)
	wantServiceAccounts[core.IDOf(serviceAccount3)] = serviceAccount3
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding2.Subjects because rs3 and rs2 are in the same namespace.
	roleBinding2.Subjects = append(roleBinding2.Subjects, subject(nsReconcilerName3,
		configsync.ControllerNamespace,
		"ServiceAccount"))
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
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

	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label4 := map[string]string{
		metadata.SyncNamespaceLabel: rs4.Namespace,
		metadata.SyncNameLabel:      rs4.Name,
	}

	repoContainerEnv4 := testReconciler.populateRepoContainerEnvs(ctx, rs4, nsReconcilerName4)
	repoDeployment4 := repoSyncDeployment(
		nsReconcilerName4,
		setServiceAccountName(nsReconcilerName4),
		secretMutator(nsReconcilerName4+"-"+reposyncCookie),
		envVarMutator("HTTPS_PROXY", nsReconcilerName4+"-"+reposyncCookie, "https_proxy"),
		containerEnvMutator(repoContainerEnv4),
	)
	wantDeployments[core.IDOf(repoDeployment4)] = repoDeployment4
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount4 := fake.ServiceAccountObject(
		nsReconcilerName4,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label4),
	)
	wantServiceAccounts[core.IDOf(serviceAccount4)] = serviceAccount4
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding1.Subjects because rs1 and rs4 are in the same namespace.
	roleBinding1.Subjects = append(roleBinding1.Subjects, subject(nsReconcilerName4,
		configsync.ControllerNamespace,
		"ServiceAccount"))
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
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

	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label5 := map[string]string{
		metadata.SyncNamespaceLabel: rs5.Namespace,
		metadata.SyncNameLabel:      rs5.Name,
	}

	repoContainerEnv5 := testReconciler.populateRepoContainerEnvs(ctx, rs5, nsReconcilerName5)
	repoDeployment5 := repoSyncDeployment(
		nsReconcilerName5,
		setServiceAccountName(nsReconcilerName5),
		secretMutator(nsReconcilerName5+"-"+secretName),
		envVarMutator("HTTPS_PROXY", nsReconcilerName5+"-"+secretName, "https_proxy"),
		envVarMutator(gitSyncName, nsReconcilerName5+"-"+secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, nsReconcilerName5+"-"+secretName, GitSecretConfigKeyToken),
		containerEnvMutator(repoContainerEnv5),
	)
	wantDeployments[core.IDOf(repoDeployment5)] = repoDeployment5
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	serviceAccount5 := fake.ServiceAccountObject(
		nsReconcilerName5,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label5),
	)
	wantServiceAccounts[core.IDOf(serviceAccount5)] = serviceAccount5
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	// Add to roleBinding1.Subjects because rs1 and rs5 are in the same namespace.
	roleBinding1.Subjects = append(roleBinding1.Subjects, subject(nsReconcilerName5,
		configsync.ControllerNamespace,
		"ServiceAccount"))
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}

	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

	// Test updating Deployment resources for rs1: my-repo-sync
	rs1.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs1); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv1 = testReconciler.populateRepoContainerEnvs(ctx, rs1, nsReconcilerName)
	repoDeployment1 = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv1),
	)
	wantDeployments[core.IDOf(repoDeployment1)] = repoDeployment1

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Test updating Deployment resources for rs2: repo-sync
	rs2.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs2); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv2 = testReconciler.populateRepoContainerEnvs(ctx, rs2, nsReconcilerName2)
	repoDeployment2 = repoSyncDeployment(
		nsReconcilerName2,
		setServiceAccountName(nsReconcilerName2),
		gceNodeMutator(""),
		containerEnvMutator(repoContainerEnv2),
	)
	wantDeployments[core.IDOf(repoDeployment2)] = repoDeployment2

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Test updating Deployment resources for rs3: my-rs-3
	rs3.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs3); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv3 = testReconciler.populateRepoContainerEnvs(ctx, rs3, nsReconcilerName3)
	repoDeployment3 = repoSyncDeployment(
		nsReconcilerName3,
		setServiceAccountName(nsReconcilerName3),
		gceNodeMutator(gcpSAEmail),
		containerEnvMutator(repoContainerEnv3),
	)
	wantDeployments[core.IDOf(repoDeployment3)] = repoDeployment3
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Resources successfully updated")

	// Test garbage collecting RoleBinding after all RepoSyncs are deleted
	if err := fakeClient.Delete(ctx, rs1); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// Subject for rs1 is removed from RoleBinding.Subjects
	roleBinding1.Subjects = updateSubjects(roleBinding1.Subjects, nsReconcilerName)
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}
	validateGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName, rs1.Spec.Git.SecretRef.Name)

	if err := fakeClient.Delete(ctx, rs2); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// Subject for rs2 is removed from RoleBinding.Subjects
	roleBinding2.Subjects = updateSubjects(roleBinding2.Subjects, nsReconcilerName2)
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}
	validateGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName2, rs2.Spec.Git.SecretRef.Name)

	if err := fakeClient.Delete(ctx, rs3); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// roleBinding2 is deleted because there are no more RepoSyncs in the namespace.
	if err := validateResourceDeleted(core.IDOf(roleBinding2), fakeClient); err != nil {
		t.Error(err)
	}
	delete(wantRoleBindings, core.IDOf(roleBinding2))
	validateGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName3, rs3.Spec.Git.SecretRef.Name)

	if err := fakeClient.Delete(ctx, rs4); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName4); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// Subject for rs4 is removed from RoleBinding.Subjects
	roleBinding1.Subjects = updateSubjects(roleBinding1.Subjects, nsReconcilerName4)
	if err := validateRoleBindings(wantRoleBindings, fakeClient); err != nil {
		t.Error(err)
	}
	validateGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName4, rs4.Spec.Git.SecretRef.Name)

	if err := fakeClient.Delete(ctx, rs5); err != nil {
		t.Fatalf("failed to delete the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName5); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// Verify the RoleBinding is deleted after all RepoSyncs are deleted in the namespace.
	if err := validateResourceDeleted(core.IDOf(roleBinding1), fakeClient); err != nil {
		t.Error(err)
	}
	validateGeneratedResourcesDeleted(t, fakeClient, nsReconcilerName5, rs5.Spec.Git.SecretRef.Name)
}

func validateGeneratedResourcesDeleted(t *testing.T, fakeClient *syncerFake.Client, reconcilerName, secretRefName string) {
	// Verify deployment is deleted.
	deployment := fake.DeploymentObject(core.Namespace(configsync.ControllerNamespace), core.Name(reconcilerName))
	if err := validateResourceDeleted(core.IDOf(deployment), fakeClient); err != nil {
		t.Error(err)
	}

	// Verify service account is deleted.
	serviceAccount := fake.ServiceAccountObject(reconcilerName, core.Namespace(configsync.ControllerNamespace))
	if err := validateResourceDeleted(core.IDOf(serviceAccount), fakeClient); err != nil {
		t.Error(err)
	}
	// Verify the copied secret is deleted for RepoSync.
	if strings.HasPrefix(reconcilerName, reconciler.NsReconcilerPrefix) {
		s := fake.SecretObject(ReconcilerResourceName(reconcilerName, secretRefName), core.Namespace(configsync.ControllerNamespace))
		if err := validateResourceDeleted(core.IDOf(s), fakeClient); err != nil {
			t.Error(err)
		}
	}
}

func TestMapSecretToRepoSyncs(t *testing.T) {
	testSecretName := "ssh-test"
	rs1 := repoSync("ns1", "rs1", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	rs2 := repoSync("ns1", "rs2", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	rs3 := repoSync("ns1", "rs3", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(testSecretName))

	ns1rs1ReconcilerName := reconciler.NsReconcilerName(rs1.Namespace, rs1.Name)
	serviceAccountToken := ns1rs1ReconcilerName + "-token-p29b5"
	serviceAccount := fake.ServiceAccountObject(ns1rs1ReconcilerName, core.Namespace(configsync.ControllerNamespace))
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
			name:   fmt.Sprintf("A secret from the %s namespace NOT starting with %s", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject("s1", core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, but no corresponding RepoSync",
				configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ReconcilerResourceName(reconciler.NsReconcilerName("any-ns", "any-rs"), reposyncSSHKey),
				core.Namespace(configsync.ControllerNamespace),
			),
			want: nil,
		},
		{
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, with a mapping RepoSync",
				configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ReconcilerResourceName(ns1rs1ReconcilerName, reposyncSSHKey),
				core.Namespace(configsync.ControllerNamespace),
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
			name: fmt.Sprintf("A secret from the %s namespace starting with %s, including `-token-`, but no service account", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(ns1rs1ReconcilerName+"-token-123456",
				core.Namespace(configsync.ControllerNamespace),
			),
			want: nil,
		},
		{
			name:   fmt.Sprintf("A secret from the %s namespace starting with %s, including `-token-`, with a mapping service account", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(serviceAccountToken, core.Namespace(configsync.ControllerNamespace)),
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
	}

	_, testReconciler := setupNSReconciler(t, rs1, rs2, rs3, serviceAccount)
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
	rs1 := repoSync("ns1", "rs1", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	ns1rs1ReconcilerName := reconciler.NsReconcilerName(rs1.Namespace, rs1.Name)
	rs2 := repoSync("ns2", "rs2", reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
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
			name:   fmt.Sprintf("A deployment from the %s namespace NOT starting with %s", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name("deploy1"), core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A deployment from the %s namespace starting with %s, no mapping RepoSync", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name(reconciler.NsReconcilerName("any", "any")), core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A deployment from the %s namespace starting with %s, with mapping RepoSync", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.DeploymentObject(core.Name(ns1rs1ReconcilerName), core.Namespace(configsync.ControllerNamespace)),
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
			name:   fmt.Sprintf("A serviceaccount from the %s namespace NOT starting with %s", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject("sa1", core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A serviceaccount from the %s namespace starting with %s, no mapping RepoSync", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject(reconciler.NsReconcilerName("any", "any"), core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A serviceaccount from the %s namespace starting with %s, with mapping RepoSync", configsync.ControllerNamespace, reconciler.NsReconcilerPrefix+"-"),
			object: fake.ServiceAccountObject(ns1rs1ReconcilerName, core.Namespace(configsync.ControllerNamespace)),
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
			name:   fmt.Sprintf("A rolebinding from the %s namespace, different from %s", configsync.ControllerNamespace, rsRoleBindingName),
			object: fake.RoleBindingObject(core.Name("any"), core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A rolebinding from the %s namespace, same as %s", configsync.ControllerNamespace, rsRoleBindingName),
			object: fake.RoleBindingObject(core.Name(rsRoleBindingName), core.Namespace(configsync.ControllerNamespace)),
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

	_, testReconciler := setupNSReconciler(t, rs1, rs2)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
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

	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthGCPServiceAccount), reposyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs, secretObj(t, reposyncSSHKey, configsync.AuthSSH, core.Namespace(rs.Namespace)))
	workloadIdentityPool := "test-gke-dev.svc.id.goog"
	testReconciler.membership = &hubv1.Membership{
		Spec: hubv1.MembershipSpec{
			WorkloadIdentityPool: workloadIdentityPool,
			IdentityProvider:     "https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster",
		},
	}

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(nsReconcilerName),
		fleetWorkloadIdentityMutator(workloadIdentityPool, gcpSAEmail),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Resources successfully created")

	// Test updating RepoSync resources with SSH auth type.
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef.Name = reposyncSSHKey
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)

	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		secretMutator(nsReconcilerName+"-"+reposyncSSHKey),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// Test updating RepoSync resources with None auth type.
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestRepoSyncWithOCI(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := repoSyncWithOCI(reposyncNs, reposyncName, reposyncOCIAuthType(configsync.AuthNone))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, testReconciler := setupNSReconciler(t, rs)

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantNamespaces := map[string]struct{}{
		rs.Namespace: {},
	}

	// compare namespaces.
	if diff := cmp.Diff(testReconciler.namespaces, wantNamespaces, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("namespaces diff %s", diff)
	}

	label := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
	}

	wantServiceAccount := fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Labels(label),
	)

	repoContainerEnv := testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment := repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(repoDeployment): repoDeployment}

	// compare ServiceAccount.
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(wantServiceAccount)], wantServiceAccount, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ServiceAccount diff %s", diff)
	}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Resources successfully created")

	t.Log("Test updating RepoSync resources with gcenode auth type.")
	rs.Spec.Oci.Auth = configsync.AuthGCENode
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// compare ServiceAccount.
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(wantServiceAccount)], wantServiceAccount, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ServiceAccount diff %s", diff)
	}

	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
	)
	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment
	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	t.Log("Test updating RepoSync resources with gcpserviceaccount auth type.")
	rs.Spec.Oci.Auth = configsync.AuthGCPServiceAccount
	rs.Spec.Oci.GCPServiceAccountEmail = gcpSAEmail
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	repoContainerEnv = testReconciler.populateRepoContainerEnvs(ctx, rs, nsReconcilerName)
	repoDeployment = repoSyncDeployment(
		nsReconcilerName,
		setServiceAccountName(nsReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerEnvMutator(repoContainerEnv),
	)

	wantServiceAccount = fake.ServiceAccountObject(
		nsReconcilerName,
		core.Namespace(v1.NSConfigManagementSystem),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.Oci.GCPServiceAccountEmail),
		core.Labels(label),
	)
	// compare ServiceAccount.
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(wantServiceAccount)], wantServiceAccount, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ServiceAccount diff %s", diff)
	}

	wantDeployments[core.IDOf(repoDeployment)] = repoDeployment

	if err := validateDeployments(wantDeployments, fakeClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	// TODO: add test for FWI
}

func validateServiceAccounts(wants map[core.ID]*corev1.ServiceAccount, fakeClient *syncerFake.Client) error {
	for id, want := range wants {
		gotCoreObject := fakeClient.Objects[id]
		got := gotCoreObject.(*corev1.ServiceAccount)
		if diff := cmp.Diff(got, want, cmpopts.EquateEmpty()); diff != "" {
			return errors.Errorf("ServiceAccount[%s/%s] diff: %s", got.Namespace, got.Name, diff)
		}
	}
	return nil
}

func validateRoleBindings(wants map[core.ID]*rbacv1.RoleBinding, fakeClient *syncerFake.Client) error {
	for id, want := range wants {
		gotCoreObject := fakeClient.Objects[id]
		got := gotCoreObject.(*rbacv1.RoleBinding)
		if len(want.Subjects) != len(got.Subjects) {
			return errors.Errorf("RoleBinding[%s/%s] has unexpected number of subjects, expected %d, got %d",
				got.Namespace, got.Name, len(want.Subjects), len(got.Subjects))
		}
		for _, ws := range want.Subjects {
			for _, gs := range got.Subjects {
				if ws.Namespace == gs.Namespace && ws.Name == gs.Name {
					if !reflect.DeepEqual(ws, gs) {
						return errors.Errorf("RoleBinding[%s/%s] has unexpected subject, expected %v, got %v", got.Namespace, got.Name, ws, gs)
					}
				}
			}
		}
		got.Subjects = want.Subjects
		if diff := cmp.Diff(got, want, cmpopts.EquateEmpty()); diff != "" {
			return errors.Errorf("RoleBinding[%s/%s] diff: %s", got.Namespace, got.Name, diff)
		}
	}
	return nil
}

func validateClusterRoleBinding(want *rbacv1.ClusterRoleBinding, fakeClient *syncerFake.Client) error {
	gotCoreObject := fakeClient.Objects[core.IDOf(want)]
	got := gotCoreObject.(*rbacv1.ClusterRoleBinding)
	if len(want.Subjects) != len(got.Subjects) {
		return errors.Errorf("ClusterRoleBinding[%s/%s] has unexpected number of subjects, expected %d, got %d",
			got.Namespace, got.Name, len(want.Subjects), len(got.Subjects))
	}
	for _, ws := range want.Subjects {
		for _, gs := range got.Subjects {
			if ws.Namespace == gs.Namespace && ws.Name == gs.Name {
				if !reflect.DeepEqual(ws, gs) {
					return errors.Errorf("ClusterRoleBinding[%s/%s] has unexpected subject, expected %v, got %v", got.Namespace, got.Name, ws, gs)
				}
			}
		}
	}
	got.Subjects = want.Subjects
	if diff := cmp.Diff(got, want, cmpopts.EquateEmpty()); diff != "" {
		return errors.Errorf("ClusterRoleBinding[%s/%s] diff: %s", got.Namespace, got.Name, diff)
	}
	return nil
}

// validateDeployments validates that important fields in the `wants` deployments match those same fields in the deployments found in the fakeClient
func validateDeployments(wants map[core.ID]*appsv1.Deployment, fakeClient *syncerFake.Client) error {
	for id, want := range wants {
		gotCoreObject := fakeClient.Objects[id]
		got := gotCoreObject.(*appsv1.Deployment)

		// Compare Deployment Annotations
		if diff := cmp.Diff(want.Annotations, got.Annotations); diff != "" {
			return errors.Errorf("Unexpected Deployment Annotations found for %q. Diff: %v", id, diff)
		}

		// Compare Deployment Template Annotations.
		if diff := cmp.Diff(want.Spec.Template.Annotations, got.Spec.Template.Annotations); diff != "" {
			return errors.Errorf("Unexpected Template Annotations found for %q. Diff: %v", id, diff)
		}

		// Compare ServiceAccountName.
		if diff := cmp.Diff(want.Spec.Template.Spec.ServiceAccountName, got.Spec.Template.Spec.ServiceAccountName); diff != "" {
			return errors.Errorf("Unexpected ServiceAccountName for %q. Diff: %v", id, diff)
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
						return errors.Errorf("Unexpected configMapRef found for %q, diff %s", id, diff)
					}
					// Compare VolumeMount fields in the container.
					if diff := cmp.Diff(i.VolumeMounts, j.VolumeMounts,
						cmpopts.SortSlices(func(x, y corev1.VolumeMount) bool { return x.Name < y.Name })); diff != "" {
						return errors.Errorf("Unexpected volumeMount found for %q, diff %s", id, diff)
					}

					// Compare Env fields in the container.
					if diff := cmp.Diff(i.Env, j.Env,
						cmpopts.SortSlices(func(x, y corev1.EnvVar) bool { return x.Name < y.Name })); diff != "" {
						return errors.Errorf("Unexpected EnvVar found for %q, diff %s", id, diff)
					}

					// Compare Resources fields in the container.
					if diff := cmp.Diff(i.Resources, j.Resources); diff != "" {
						return errors.Errorf("Unexpected resources found for the %q container of %q, diff %s", i.Name, id, diff)
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
					if !reflect.DeepEqual(wantVolume.VolumeSource, gotVolume.VolumeSource) {
						return errors.Errorf("Unexpected volume source for volume %s of %q, want %v, got %v",
							wantVolume.Name, id, wantVolume.VolumeSource, gotVolume.VolumeSource)
					}
				}
			}
		}
	}
	return nil
}

func validateResourceDeleted(resourceID core.ID, fakeClient *syncerFake.Client) error {
	if _, found := fakeClient.Objects[resourceID]; found {
		return errors.Errorf("resource %s still exists", resourceID)
	}
	return nil
}

func updateSubjects(subjects []rbacv1.Subject, name string) []rbacv1.Subject {
	var result []rbacv1.Subject
	for _, s := range subjects {
		if s.Namespace != configsync.ControllerNamespace || s.Name != name {
			result = append(result, s)
		}
	}
	return result
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
	for _, mut := range muts {
		mut(dep)
	}
	return dep
}
