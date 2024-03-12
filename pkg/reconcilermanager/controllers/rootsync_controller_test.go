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
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/rootsync"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	webhookconfiguration "kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	rootsyncName   = "my-root-sync"
	rootsyncRepo   = "https://github.com/test/rootsync/csp-config-management/"
	rootsyncDir    = "baz-corp"
	testCluster    = "abc-123"
	ociImage       = "gcr.io/your-dev-project/config-sync-test/kustomize-components"
	helmRepo       = "oci://us-central1-docker.pkg.dev/your-dev-project/helm-oci-1"
	helmChart      = "hello-chart"
	helmVersion    = "0.1.0"
	rootsyncSSHKey = "root-ssh-key"
)

var rootReconcilerName = core.RootReconcilerName(rootsyncName)

func role(name string, opts ...core.MetaMutator) *rbacv1.Role {
	result := fake.RoleObject(opts...)
	result.Name = name

	return result
}

func clusterrole(t *testing.T, name string, opts ...core.MetaMutator) *rbacv1.ClusterRole {
	t.Helper()

	result := fake.ClusterRoleObject(opts...)
	result.Name = name

	return result
}

func clusterrolebinding(name string, role string, opts ...core.MetaMutator) *rbacv1.ClusterRoleBinding {
	result := fake.ClusterRoleBindingObject(opts...)
	result.Name = name

	result.RoleRef.Name = role
	result.RoleRef.Kind = "ClusterRole"
	result.RoleRef.APIGroup = "rbac.authorization.k8s.io"

	return result
}

func rootReconcilerClusterRoleBinding(reconcilerName, clusterRole string, opts ...core.MetaMutator) *rbacv1.ClusterRoleBinding {
	defaultOpts := []core.MetaMutator{
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNameLabel:            rootsyncName,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
		core.GenerateName(reconcilerName + "-"),
		core.Generation(1),
		core.UID("1"),
		core.ResourceVersion("1"),
	}
	opts = append(defaultOpts, opts...)
	result := clusterrolebinding("", clusterRole, opts...)
	result.Subjects = addSubjectByName(nil, reconcilerName)
	return result
}

func rootReconcilerRoleBinding(roleRef v1beta1.RootSyncRoleRef, opts ...core.MetaMutator) *rbacv1.RoleBinding {
	defaultOpts := []core.MetaMutator{
		core.Namespace(roleRef.Namespace),
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNameLabel:            rootsyncName,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
		core.GenerateName("root-reconciler-my-root-sync-"),
		core.Generation(1),
		core.UID("1"),
		core.ResourceVersion("1"),
	}
	opts = append(defaultOpts, opts...)
	result := rolebinding("", roleRef.Name, roleRef.Kind, opts...)
	result.Subjects = addSubjectByName(nil, rootReconcilerName)
	return result
}

func defaultRootReconcilerClusterRoleBindingMap() map[v1beta1.RootSyncRoleRef]rbacv1.ClusterRoleBinding {
	defaultMap := make(map[v1beta1.RootSyncRoleRef]rbacv1.ClusterRoleBinding)
	return defaultMap
}

func configMapWithData(namespace, name string, data map[string]string, opts ...core.MetaMutator) *corev1.ConfigMap {
	baseOpts := []core.MetaMutator{
		core.Labels(map[string]string{
			"app": reconcilermanager.Reconciler,
		}),
	}
	opts = append(baseOpts, opts...)
	result := fake.ConfigMapObject(opts...)
	result.Namespace = namespace
	result.Name = name
	result.Data = data
	return result
}

func secretObj(t *testing.T, name string, auth configsync.AuthType, sourceType v1beta1.SourceType, opts ...core.MetaMutator) *corev1.Secret {
	t.Helper()
	result := fake.SecretObject(name, opts...)
	result.Data = secretData(t, "test-key", auth, sourceType)
	return result
}

func secretObjWithProxy(t *testing.T, name string, auth configsync.AuthType, opts ...core.MetaMutator) *corev1.Secret {
	t.Helper()
	result := fake.SecretObject(name, opts...)
	result.Data = secretData(t, "test-key", auth, v1beta1.GitSource)
	m2 := secretData(t, "test-key", "https_proxy", v1beta1.GitSource)
	for k, v := range m2 {
		result.Data[k] = v
	}
	return result
}

func secretObjWithKnownHosts(t *testing.T, name string, opts ...core.MetaMutator) *corev1.Secret {
	t.Helper()
	result := fake.SecretObject(name, opts...)
	result.Data = secretData(t, "test-key", configsync.AuthSSH, v1beta1.GitSource)
	result.Data[KnownHostsKey] = []byte("abc")
	return result
}

func setupRootReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *syncerFake.DynamicClient, *RootSyncReconciler) {
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

	testReconciler := NewRootSyncReconciler(
		testCluster,
		filesystemPollingPeriod,
		hydrationPollingPeriod,
		cs.Client,
		cs.Client,
		cs.DynamicClient,
		controllerruntime.Log.WithName("controllers").WithName(configsync.RootSyncKind),
		cs.Client.Scheme(),
	)
	return cs.Client, cs.DynamicClient, testReconciler
}

func rootsyncRef(rev string) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Revision = rev
	}
}

func rootsyncBranch(branch string) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Branch = branch
	}
}

func rootsyncSecretType(auth configsync.AuthType) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Auth = auth
	}
}

func rootsyncOCIAuthType(auth configsync.AuthType) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Oci.Auth = auth
	}
}
func rootsyncHelmAuthType(auth configsync.AuthType) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Helm.Auth = auth
	}
}

func rootsyncSecretRef(ref string) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: ref}
	}
}

func rootsyncHelmSecretRef(ref string) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Helm.SecretRef = &v1beta1.SecretReference{Name: ref}
	}
}

func rootsyncGCPSAEmail(email string) func(sync *v1beta1.RootSync) {
	return func(sync *v1beta1.RootSync) {
		sync.Spec.GCPServiceAccountEmail = email
	}
}

func rootsyncOverrideResources(containers []v1beta1.ContainerResourcesSpec) func(sync *v1beta1.RootSync) {
	return func(sync *v1beta1.RootSync) {
		sync.Spec.Override = &v1beta1.RootSyncOverrideSpec{
			OverrideSpec: v1beta1.OverrideSpec{
				Resources: containers,
			},
		}
	}
}

func rootsyncOverrideGitSyncDepth(depth int64) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.SafeOverride().GitSyncDepth = &depth
	}
}

func rootsyncOverrideReconcileTimeout(reconcileTimeout metav1.Duration) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.SafeOverride().ReconcileTimeout = &reconcileTimeout
	}
}

func rootsyncOverrideAPIServerTimeout(apiServerTimout metav1.Duration) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.SafeOverride().APIServerTimeout = &apiServerTimout
	}
}

func rootsyncOverrideRoleRefs(roleRefs ...v1beta1.RootSyncRoleRef) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.SafeOverride().RoleRefs = roleRefs
	}
}

func rootsyncNoSSLVerify() func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		rs.Spec.Git.NoSSLVerify = true
	}
}

func rootsyncCACert(sourceType v1beta1.SourceType, caCertSecretRef string) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		switch sourceType {
		case v1beta1.GitSource:
			rs.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecretRef}
		case v1beta1.OciSource:
			rs.Spec.Oci.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecretRef}
		case v1beta1.HelmSource:
			rs.Spec.Helm.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecretRef}
		}
	}
}

func rootsyncRenderingRequired(renderingRequired bool) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		val := strconv.FormatBool(renderingRequired)
		core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, val)
	}
}

func rootsyncDynamicNSSelectorEnabled(dynamicNSSelectorEnabled bool) func(*v1beta1.RootSync) {
	return func(rs *v1beta1.RootSync) {
		val := strconv.FormatBool(dynamicNSSelectorEnabled)
		core.SetAnnotation(rs, metadata.DynamicNSSelectorEnabledAnnotationKey, val)
	}
}

func rootSync(name string, opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	rs := fake.RootSyncObjectV1Beta1(name)
	// default to require rendering for convenience with existing tests
	core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, "true")
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func rootSyncWithGit(name string, opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	addGit := func(rs *v1beta1.RootSync) {
		rs.Spec.SourceType = string(v1beta1.GitSource)
		rs.Spec.Git = &v1beta1.Git{
			Repo: rootsyncRepo,
			Dir:  rootsyncDir,
		}
	}
	opts = append([]func(*v1beta1.RootSync){addGit}, opts...)
	return rootSync(name, opts...)
}

func rootSyncWithOCI(name string, opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	addOci := func(rs *v1beta1.RootSync) {
		rs.Spec.SourceType = string(v1beta1.OciSource)
		rs.Spec.Oci = &v1beta1.Oci{
			Image: ociImage,
			Dir:   rootsyncDir,
		}
	}
	opts = append([]func(*v1beta1.RootSync){addOci}, opts...)
	return rootSync(name, opts...)
}

func rootSyncWithHelm(name string, opts ...func(*v1beta1.RootSync)) *v1beta1.RootSync {
	addHelm := func(rs *v1beta1.RootSync) {
		rs.Spec.SourceType = string(v1beta1.HelmSource)
		rs.Spec.Helm = &v1beta1.HelmRootSync{HelmBase: v1beta1.HelmBase{
			Repo:    helmRepo,
			Chart:   helmChart,
			Version: helmVersion,
		}}
	}
	opts = append([]func(*v1beta1.RootSync){addHelm}, opts...)
	return rootSync(name, opts...)
}

func TestCreateAndUpdateRootReconcilerWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	overrideAllContainerResources := []v1beta1.ContainerResourcesSpec{
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

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH),
		rootsyncSecretRef(rootsyncSSHKey), rootsyncOverrideResources(overrideAllContainerResources))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatal(err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(overrideAllContainerResources),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the CPU resources of the reconciler and hydration-container and the memory resources of the git-sync container
	overrideSelectedResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("1"),
			CPULimit:      resource.MustParse("1.2"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPULimit:      resource.MustParse("0.8"),
		},
		{
			ContainerName: reconcilermanager.GitSync,
			MemoryRequest: resource.MustParse("800Gi"),
			MemoryLimit:   resource.MustParse("888Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideSelectedResources,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(overrideSelectedResources, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestUpdateRootReconcilerWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the CPU/memory requests and limits of the reconciler, hydration-controller, and git-sync container
	overrideAllContainerResources := []v1beta1.ContainerResourcesSpec{
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

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideAllContainerResources,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(overrideAllContainerResources, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test overriding the CPU/memory limits of the reconciler and hydration-controller containers
	overrideReconcilerAndHydrationResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("1"),
			CPULimit:      resource.MustParse("2"),
			MemoryRequest: resource.MustParse("1Gi"),
			MemoryLimit:   resource.MustParse("2Gi"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("1.1"),
			CPULimit:      resource.MustParse("1.3"),
			MemoryRequest: resource.MustParse("3Gi"),
			MemoryLimit:   resource.MustParse("4Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideReconcilerAndHydrationResources,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(overrideReconcilerAndHydrationResources, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test overriding the cpu request and memory limits of the git-sync container
	overrideGitSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.GitSync,
			CPURequest:    resource.MustParse("200Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideGitSyncResources,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(overrideGitSyncResources, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Clear rs.Spec.Override
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("5"), setGeneration(5),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncCreateWithNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey), rootsyncNoSSLVerify())
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRootSyncUpdateNoSSLVerify(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.NoSSLVerify to false
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
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
	t.Log("No need to update Deployment")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.NoSSLVerify to true
	rs.Spec.NoSSLVerify = true
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.NoSSLVerify to false
	rs.Spec.NoSSLVerify = false
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncCreateWithCACertSecret(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment
	caCertSecret := "foo-secret"
	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch),
		rootsyncSecretType(configsync.AuthToken), rootsyncSecretRef(secretName),
		rootsyncCACert(v1beta1.GitSource, caCertSecret))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	certSecret.Data[CACertSecretKey] = []byte("test-data")
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, gitSecret, certSecret)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		caCertSecretMutator(secretName, caCertSecret),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, secretName, "https_proxy"),
		envVarMutator(gitSyncUsername, secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, secretName, GitSecretConfigKeyToken),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRootSyncUpdateCACertSecret(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	caCertSecret := "foo-secret"
	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthToken), rootsyncSecretRef(secretName))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	gitSecret := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs.Namespace))
	gitSecret.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")
	certSecret := secretObj(t, caCertSecret, GitSecretConfigKeyToken, v1beta1.GitSource, core.Namespace(rs.Namespace))
	certSecret.Data[CACertSecretKey] = []byte("test-data")
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, gitSecret, certSecret)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(secretName),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, secretName, "https_proxy"),
		envVarMutator(gitSyncUsername, secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, secretName, GitSecretConfigKeyToken),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Unset rs.Spec.CACertSecretRef
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

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.CACertSecretRef
	rs.Spec.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		caCertSecretMutator(secretName, caCertSecret),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, secretName, "https_proxy"),
		envVarMutator(gitSyncUsername, secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, secretName, GitSecretConfigKeyToken),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Unset rs.Spec.CACertSecretRef
	rs.Spec.CACertSecretRef = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(secretName),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, secretName, "https_proxy"),
		envVarMutator(gitSyncUsername, secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, secretName, GitSecretConfigKeyToken),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Fatalf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncReconcileAdmissionWebhook(t *testing.T) {
	testCases := []struct {
		name                   string
		rootSync               *v1beta1.RootSync
		existingObjects        []client.Object
		expectedWebhookEnabled bool
	}{
		{
			name:                   "flag false, admission webhook disabled",
			expectedWebhookEnabled: false,
		},
		{
			name: "flag false, admission webhook enabled",
			existingObjects: []client.Object{
				fake.AdmissionWebhookObject(webhookconfiguration.Name),
			},
			expectedWebhookEnabled: true,
		},
	}
	parseDeployment = parsedDeployment
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rootSync := rootSyncWithGit(
				rootsyncName,
				rootsyncRef(gitRevision),
				rootsyncBranch(branch),
				rootsyncSecretType(GitSecretConfigKeySSH),
				rootsyncSecretRef(rootsyncSSHKey),
			)
			reqNamespacedName := namespacedName(rootSync.Name, rootSync.Namespace)
			fakeClient, _, testReconciler := setupRootReconciler(t, rootSync, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rootSync.Namespace)))

			for _, o := range tc.existingObjects {
				t.Log("creating obj", o.GetObjectKind().GroupVersionKind().Kind)
				if err := fakeClient.Create(context.Background(), o); err != nil {
					t.Fatal(err)
				}
			}

			ctx := context.Background()
			if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
				t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
			}

			testutil.AssertEqual(t, tc.expectedWebhookEnabled, testReconciler.webhookEnabled)
		})
	}
}

func TestRootSyncReconcileWithInvalidCACertSecret(t *testing.T) {
	caCertSecret := "foo-secret"
	testCases := map[string]struct {
		rootSync *v1beta1.RootSync
	}{
		"git": {
			rootSync: rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch),
				rootsyncSecretType(configsync.AuthNone),
				rootsyncCACert(v1beta1.GitSource, caCertSecret)),
		},
		"oci": {
			rootSync: rootSyncWithOCI(rootsyncName, rootsyncOCIAuthType(configsync.AuthNone),
				rootsyncCACert(v1beta1.OciSource, caCertSecret)),
		},
		"helm": {
			rootSync: rootSyncWithHelm(rootsyncName, rootsyncHelmAuthType(configsync.AuthNone),
				rootsyncCACert(v1beta1.HelmSource, caCertSecret)),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			reqNamespacedName := namespacedName(tc.rootSync.Name, tc.rootSync.Namespace)
			certSecret := fake.SecretObject(caCertSecret, core.Namespace(tc.rootSync.Namespace))
			fakeClient, _, testReconciler := setupRootReconciler(t, tc.rootSync, certSecret)

			// reconcile
			ctx := context.Background()
			if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
				t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
			}

			// rootsync should be in stalled status
			wantRs := fake.RootSyncObjectV1Beta1(rootsyncName)
			rootsync.SetStalled(wantRs, "Validation", fmt.Errorf("caCertSecretRef was set, but %s key is not present in %s Secret", CACertSecretKey, caCertSecret))
			validateRootSyncStatus(t, wantRs, fakeClient)
		})
	}
}

func TestRootSyncValidateCACertSecret(t *testing.T) {
	caCertSecret := "foo-secret"
	testCases := map[string]struct {
		objs []client.Object
		err  string
	}{
		"caCertSecretRef set but missing Secret": {
			err: fmt.Sprintf("Secret %s not found, create one to allow client connections with CA certificate", caCertSecret),
		},
		"caCertSecretRef set but invalid Secret": {
			objs: []client.Object{
				fake.SecretObject(caCertSecret, core.Namespace(configsync.ControllerNamespace)),
			},
			err: fmt.Sprintf("caCertSecretRef was set, but %s key is not present in %s Secret", CACertSecretKey, caCertSecret),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			_, _, testReconciler := setupRootReconciler(t, tc.objs...)
			ctx := context.Background()

			// validation should return an error
			err := testReconciler.validateCACertSecret(ctx, configsync.ControllerNamespace, caCertSecret)
			require.Equal(t, tc.err, err.Error(), "unexpected function error")
		})
	}
}

func TestRootSyncCreateWithOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey), rootsyncOverrideGitSyncDepth(5))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRootSyncUpdateOverrideGitSyncDepth(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("ServiceAccount, ClusterRoleBinding and Deployment successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test overriding the git sync depth to a positive value
	var depth int64 = 5
	rs.Spec.SafeOverride().GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test overriding the git sync depth to 0
	depth = 0
	rs.Spec.SafeOverride().GitSyncDepth = &depth
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.Override.GitSyncDepth to nil.
	rs.Spec.SafeOverride().GitSyncDepth = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Clear rs.Spec.Override
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
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

func TestRootSyncCreateWithOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey), rootsyncOverrideReconcileTimeout(metav1.Duration{Duration: 50 * time.Second}))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")
}

func TestRootSyncUpdateOverrideReconcileTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rootContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("ServiceAccount, ClusterRoleBinding and Deployment successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test overriding the reconcile timeout to 50s
	reconcileTimeout := metav1.Duration{Duration: 50 * time.Second}
	rs.Spec.SafeOverride().ReconcileTimeout = &reconcileTimeout
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)

	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.Override.ReconcileTimeout to nil.
	rs.Spec.SafeOverride().ReconcileTimeout = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Clear rs.Spec.Override
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
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

func TestRootSyncCreateWithOverrideAPIServerTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey), rootsyncOverrideReconcileTimeout(metav1.Duration{Duration: 50 * time.Second}))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully created")
}

func TestRootSyncUpdateOverrideAPIServerTimeout(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rootContainerEnv := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("ServiceAccount, ClusterRoleBinding and Deployment successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test overriding the reconcile timeout to 50s
	reconcileTimeout := metav1.Duration{Duration: 50 * time.Second}
	rs.Spec.SafeOverride().ReconcileTimeout = &reconcileTimeout
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	updatedRootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(updatedRootDeployment)] = updatedRootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Set rs.Spec.Override.ReconcileTimeout to nil.
	rs.Spec.SafeOverride().ReconcileTimeout = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	rootContainerEnv = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Clear rs.Spec.Override
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
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

func validateReconcilerClusterRoleBindings(fakeClient *syncerFake.Client, rootSyncName string, want map[v1beta1.RootSyncRoleRef]rbacv1.ClusterRoleBinding) error {
	got := &rbacv1.ClusterRoleBindingList{}
	ctx := context.Background()

	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(ManagedObjectLabelMap(
			configsync.RootSyncKind,
			types.NamespacedName{Name: rootSyncName, Namespace: configsync.ControllerNamespace},
		)),
	}
	err := fakeClient.List(ctx, got, opts)
	if err != nil {
		return err
	}
	if len(want) != len(got.Items) {
		return fmt.Errorf("want %d ClusterRoleBindings, got %d", len(want), len(got.Items))
	}
	gotCRBMap := make(map[v1beta1.RootSyncRoleRef]rbacv1.ClusterRoleBinding)
	for idx, crb := range got.Items {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind: crb.RoleRef.Kind,
			Name: crb.RoleRef.Name,
		}
		gotCRBMap[roleRef] = got.Items[idx]
	}
	for roleRef, wantCRB := range want {
		gotCRB, ok := gotCRBMap[roleRef]
		if !ok {
			return fmt.Errorf("ClusterRoleBinding for roleRef %v not found", roleRef)
		}
		wantCRB.Name = gotCRB.Name
		if diff := cmp.Diff(wantCRB, gotCRB, cmpopts.EquateEmpty()); diff != "" {
			return fmt.Errorf("ClusterRoleBinding[%s] diff: %s", wantCRB.Name, diff)
		}
	}
	return nil
}

func validateReconcilerRoleBindings(fakeClient *syncerFake.Client, syncKind string, rsRef types.NamespacedName, want map[v1beta1.RootSyncRoleRef]rbacv1.RoleBinding) error {
	got := &rbacv1.RoleBindingList{}
	ctx := context.Background()

	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(ManagedObjectLabelMap(syncKind, rsRef)),
	}
	err := fakeClient.List(ctx, got, opts)
	if err != nil {
		return err
	}
	if len(want) != len(got.Items) {
		return fmt.Errorf("want %d RoleBindings, got %d", len(want), len(got.Items))
	}
	gotRBMap := make(map[v1beta1.RootSyncRoleRef]rbacv1.RoleBinding)
	for idx, rb := range got.Items {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind:      rb.RoleRef.Kind,
			Name:      rb.RoleRef.Name,
			Namespace: rb.Namespace,
		}
		gotRBMap[roleRef] = got.Items[idx]
	}
	for roleRef, wantRB := range want {
		gotRB, ok := gotRBMap[roleRef]
		if !ok {
			return fmt.Errorf("ClusterRoleBinding for roleRef %v not found", roleRef)
		}
		wantRB.Name = gotRB.Name
		if diff := cmp.Diff(wantRB, gotRB, cmpopts.EquateEmpty()); diff != "" {
			return fmt.Errorf("ClusterRoleBinding[%s] diff: %s", wantRB.Name, diff)
		}
	}
	return nil
}

func TestRootSyncCreateWithOverrideRoleRefs(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	clusterRoleRef1 := v1beta1.RootSyncRoleRef{
		Kind: "ClusterRole",
		Name: "my-cluster-role",
	}
	clusterRoleRef2 := v1beta1.RootSyncRoleRef{
		Kind:      "ClusterRole",
		Name:      "my-tenant-role",
		Namespace: "tenant-ns",
	}
	roleRef := v1beta1.RootSyncRoleRef{
		Kind:      "Role",
		Name:      "my-cluster-role",
		Namespace: "foo-ns",
	}
	rootSyncNN := types.NamespacedName{
		Name:      rootsyncName,
		Namespace: configsync.ControllerNamespace,
	}
	rs := rootSyncWithOCI(rootsyncName,
		rootsyncOverrideRoleRefs(clusterRoleRef1, clusterRoleRef2, roleRef),
		rootsyncOCIAuthType(configsync.AuthGCENode))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, _, testReconciler := setupRootReconciler(t, rs,
		clusterrole(t, clusterRoleRef1.Name), clusterrole(t, clusterRoleRef2.Name),
		role(roleRef.Name))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantClusterRoleBindings := defaultRootReconcilerClusterRoleBindingMap()
	wantClusterRoleBindings[clusterRoleRef1] = *rootReconcilerClusterRoleBinding(
		rootReconcilerName, clusterRoleRef1.Name)
	wantRoleBindings := make(map[v1beta1.RootSyncRoleRef]rbacv1.RoleBinding)
	wantRoleBindings[clusterRoleRef2] = *rootReconcilerRoleBinding(
		clusterRoleRef2)
	wantRoleBindings[roleRef] = *rootReconcilerRoleBinding(
		roleRef)

	if err := validateReconcilerClusterRoleBindings(fakeClient, rootsyncName, wantClusterRoleBindings); err != nil {
		t.Fatalf("ClusterRoleBinding validation failed. err: %v", err)
	}
	t.Log("ClusterRoleBindings successfully created")
	if err := validateReconcilerRoleBindings(fakeClient, configsync.RootSyncKind, rootSyncNN, wantRoleBindings); err != nil {
		t.Fatalf("RoleBinding validation failed. err: %v", err)
	}
	defaultCrb := clusterrolebinding(
		RootSyncBaseClusterRoleBindingName,
		RootSyncBaseClusterRoleName,
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
	)
	defaultCrb.Subjects = addSubjectByName(nil, rootReconcilerName)

	if err := validateClusterRoleBinding(defaultCrb, fakeClient); err != nil {
		t.Fatal(err)
	}
	t.Log("RoleBindings successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("Failed to get RootSync: %q", err)
	}

	rs.Spec.SafeOverride().RoleRefs = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("Failed to update RootSync: %q", err)
	}
	t.Log("RootSync updated to remove RoleRefs override")

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	if err := validateReconcilerClusterRoleBindings(fakeClient, rootReconcilerName, nil); err != nil {
		t.Fatalf("ClusterRoleBinding validation failed. err: %v", err)
	}
	if err := validateReconcilerRoleBindings(fakeClient, configsync.RootSyncKind, rootSyncNN, nil); err != nil {
		t.Fatalf("RoleBinding validation failed. err: %v", err)
	}
	if err := validateResourceDeleted(core.IDOf(defaultCrb), fakeClient); err != nil {
		t.Fatal(err)
	}
	clusterAdminCRB := clusterrolebinding(
		RootSyncLegacyClusterRoleBindingName,
		"cluster-admin",
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
	)
	clusterAdminCRB.Subjects = addSubjectByName(nil, rootReconcilerName)
	if err := validateClusterRoleBinding(clusterAdminCRB, fakeClient); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationToIndividualClusterRoleBindingsWhenDefaultRootSyncExists(t *testing.T) {
	parseDeployment = parsedDeployment

	// set up a situation as it was before syncs had individual clusterrolebindings
	// (i.e. before https://github.com/GoogleContainerTools/kpt-config-sync/pull/938)
	// two syncs: root-sync and my-custom-sync
	rs1 := rootSyncWithOCI(configsync.RootSyncName,
		rootsyncOCIAuthType(configsync.AuthGCENode))
	rs1ReconcilerName := core.RootReconcilerName(rs1.Name)

	rs2 := rootSyncWithOCI("my-custom-sync",
		rootsyncOCIAuthType(configsync.AuthGCENode))
	rs2ReconcilerName := core.RootReconcilerName(rs2.Name)

	oldBinding := clusterrolebinding(
		RootSyncLegacyClusterRoleBindingName,
		"cluster-admin",
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
	)
	oldBinding.Subjects = addSubjectByName(nil, rs1ReconcilerName)
	oldBinding.Subjects = addSubjectByName(oldBinding.Subjects, rs2ReconcilerName)

	fakeClient, _, testReconciler := setupRootReconciler(t, rs1, rs2, oldBinding)

	ctx := context.Background()

	if _, err := testReconciler.Reconcile(ctx, namespacedName(rs1.Name, rs1.Namespace)); err != nil {
		t.Fatalf("unexpected reconciler error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, namespacedName(rs2.Name, rs2.Namespace)); err != nil {
		t.Fatalf("unexpected reconciler error: %v", err)
	}

	if err := validateClusterRoleBinding(oldBinding, fakeClient); err != nil {
		t.Fatal(err)
	}
}

func TestRootSyncSwitchAuthTypes(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthGCPServiceAccount), rootsyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources with GCPServiceAccount auth type.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	labels := map[string]string{
		metadata.SyncNamespaceLabel:       rs.Namespace,
		metadata.SyncNameLabel:            rs.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	wantServiceAccount := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.GCPServiceAccountEmail),
		core.Labels(labels),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

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

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating RootSync resources with SSH auth type.
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: rootsyncSSHKey}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating RootSync resources with None auth type.
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = &v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncReconcilerRestart(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

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
	t.Logf("Applying Patch: %s", string(patchData))
	_, err = fakeDynamicClient.Resource(kinds.DeploymentResource()).
		Namespace(rootDeployment.Namespace).
		Patch(ctx, rootDeployment.Name, types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	if err != nil {
		t.Fatalf("failed to patch the deployment: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("failed to reconcile: %v", err)
	}

	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setReplicas(1), // Change reverted
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Fatalf("Deployment validation failed. err: %v", err)
	}
	t.Log("Deployment successfully updated")
}

// This test reconcilers multiple RootSyncs with different auth types.
// - rs1: "my-root-sync", auth type is ssh.
// - rs2: uses the default "root-sync" name and auth type is gcenode
// - rs3: "my-rs-3", auth type is gcpserviceaccount
// - rs4: "my-rs-4", auth type is cookiefile with proxy
// - rs5: "my-rs-5", auth type is token with proxy
func TestMultipleRootSyncs(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs1 := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName1 := namespacedName(rs1.Name, rs1.Namespace)

	rs2 := rootSyncWithGit(configsync.RootSyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthGCENode))
	reqNamespacedName2 := namespacedName(rs2.Name, rs2.Namespace)

	rs3 := rootSyncWithGit("my-rs-3", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthGCPServiceAccount), rootsyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName3 := namespacedName(rs3.Name, rs3.Namespace)

	rs4 := rootSyncWithGit("my-rs-4", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthCookieFile), rootsyncSecretRef(reposyncCookie))
	reqNamespacedName4 := namespacedName(rs4.Name, rs4.Namespace)
	secret4 := secretObjWithProxy(t, reposyncCookie, "cookie_file", core.Namespace(rs4.Namespace))

	rs5 := rootSyncWithGit("my-rs-5", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthToken), rootsyncSecretRef(secretName))
	reqNamespacedName5 := namespacedName(rs5.Name, rs5.Namespace)
	secret5 := secretObjWithProxy(t, secretName, GitSecretConfigKeyToken, core.Namespace(rs5.Namespace))
	secret5.Data[GitSecretConfigKeyTokenUsername] = []byte("test-user")

	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs1, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs1.Namespace)))

	rootReconcilerName2 := core.RootReconcilerName(rs2.Name)
	rootReconcilerName3 := core.RootReconcilerName(rs3.Name)
	rootReconcilerName4 := core.RootReconcilerName(rs4.Name)
	rootReconcilerName5 := core.RootReconcilerName(rs5.Name)

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs1), rs1); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	wantRs1 := fake.RootSyncObjectV1Beta1(rs1.Name)
	wantRs1.Spec = rs1.Spec
	wantRs1.Status.Reconciler = rootReconcilerName
	rootsync.SetReconciling(wantRs1, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName))
	controllerutil.AddFinalizer(wantRs1, metadata.ReconcilerManagerFinalizer)
	validateRootSyncStatus(t, wantRs1, fakeClient)

	label1 := map[string]string{
		metadata.SyncNamespaceLabel:       rs1.Namespace,
		metadata.SyncNameLabel:            rs1.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	serviceAccount1 := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(label1),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(serviceAccount1): serviceAccount1}

	crb := clusterrolebinding(
		RootSyncLegacyClusterRoleBindingName,
		"cluster-admin",
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
		core.Labels(map[string]string{
			metadata.SyncKindLabel:            configsync.RootSyncKind,
			metadata.SyncNamespaceLabel:       configsync.ControllerNamespace,
			metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
		}),
	)
	crb.Subjects = addSubjectByName(crb.Subjects, rootReconcilerName)
	rootContainerEnv1 := testReconciler.populateContainerEnvs(ctx, rs1, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment1 := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv1),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment1): rootDeployment1}

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("ServiceAccount, ClusterRoleBinding and Deployment successfully created")

	// Test reconciler rs2: root-sync
	if err := fakeClient.Create(ctx, rs2); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs2), rs2); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	wantRs2 := fake.RootSyncObjectV1Beta1(rs2.Name)
	wantRs2.Spec = rs2.Spec
	wantRs2.Status.Reconciler = rootReconcilerName2
	rootsync.SetReconciling(wantRs2, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName2))
	controllerutil.AddFinalizer(wantRs2, metadata.ReconcilerManagerFinalizer)
	validateRootSyncStatus(t, wantRs2, fakeClient)

	label2 := map[string]string{
		metadata.SyncNamespaceLabel:       rs2.Namespace,
		metadata.SyncNameLabel:            rs2.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	rootContainerEnv2 := testReconciler.populateContainerEnvs(ctx, rs2, rootReconcilerName2)
	rootDeployment2 := rootSyncDeployment(rootReconcilerName2,
		setServiceAccountName(rootReconcilerName2),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv2),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(rootDeployment2)] = rootDeployment2
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount2 := fake.ServiceAccountObject(
		rootReconcilerName2,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(label2),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount2)] = serviceAccount2
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	crb.Subjects = addSubjectByName(crb.Subjects, rootReconcilerName2)
	crb.ResourceVersion = "2"
	crb.Generation = 2
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

	// Test reconciler rs3: my-rs-3
	if err := fakeClient.Create(ctx, rs3); err != nil {
		t.Fatal(err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs3), rs3); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	wantRs3 := fake.RootSyncObjectV1Beta1(rs3.Name)
	wantRs3.Spec = rs3.Spec
	wantRs3.Status.Reconciler = rootReconcilerName3
	rootsync.SetReconciling(wantRs3, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName3))
	controllerutil.AddFinalizer(wantRs3, metadata.ReconcilerManagerFinalizer)
	validateRootSyncStatus(t, wantRs3, fakeClient)

	label3 := map[string]string{
		metadata.SyncNamespaceLabel:       rs3.Namespace,
		metadata.SyncNameLabel:            rs3.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	rootContainerEnv3 := testReconciler.populateContainerEnvs(ctx, rs3, rootReconcilerName3)
	rootDeployment3 := rootSyncDeployment(rootReconcilerName3,
		setServiceAccountName(rootReconcilerName3),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv3),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(rootDeployment3)] = rootDeployment3
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Error(err)
	}

	serviceAccount3 := fake.ServiceAccountObject(
		rootReconcilerName3,
		core.Namespace(configsync.ControllerNamespace),
		core.Annotation(GCPSAAnnotationKey, rs3.Spec.GCPServiceAccountEmail),
		core.Labels(label3),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount3)] = serviceAccount3
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	crb.Subjects = addSubjectByName(crb.Subjects, rootReconcilerName3)
	crb.ResourceVersion = "3"
	crb.Generation = 3
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

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
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs4), rs4); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	wantRs4 := fake.RootSyncObjectV1Beta1(rs4.Name)
	wantRs4.Spec = rs4.Spec
	wantRs4.Status.Reconciler = rootReconcilerName4
	rootsync.SetReconciling(wantRs4, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName4))
	controllerutil.AddFinalizer(wantRs4, metadata.ReconcilerManagerFinalizer)
	validateRootSyncStatus(t, wantRs4, fakeClient)

	label4 := map[string]string{
		metadata.SyncNamespaceLabel:       rs4.Namespace,
		metadata.SyncNameLabel:            rs4.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	rootContainerEnvs4 := testReconciler.populateContainerEnvs(ctx, rs4, rootReconcilerName4)
	rootDeployment4 := rootSyncDeployment(rootReconcilerName4,
		setServiceAccountName(rootReconcilerName4),
		secretMutator(reposyncCookie),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, reposyncCookie, "https_proxy"),
		containerEnvMutator(rootContainerEnvs4),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(rootDeployment4)] = rootDeployment4
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}

	serviceAccount4 := fake.ServiceAccountObject(
		rootReconcilerName4,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(label4),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount4)] = serviceAccount4
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	crb.Subjects = addSubjectByName(crb.Subjects, rootReconcilerName4)
	crb.ResourceVersion = "4"
	crb.Generation = 4
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

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
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs5), rs5); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	wantRs5 := fake.RootSyncObjectV1Beta1(rs5.Name)
	wantRs5.Spec = rs5.Spec
	wantRs5.Status.Reconciler = rootReconcilerName5
	rootsync.SetReconciling(wantRs5, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName5))
	controllerutil.AddFinalizer(wantRs5, metadata.ReconcilerManagerFinalizer)
	validateRootSyncStatus(t, wantRs5, fakeClient)

	label5 := map[string]string{
		metadata.SyncNamespaceLabel:       rs5.Namespace,
		metadata.SyncNameLabel:            rs5.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	rootContainerEnvs5 := testReconciler.populateContainerEnvs(ctx, rs5, rootReconcilerName5)
	rootDeployment5 := rootSyncDeployment(rootReconcilerName5,
		setServiceAccountName(rootReconcilerName5),
		secretMutator(secretName),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(gitSyncHTTPSProxy, secretName, "https_proxy"),
		envVarMutator(gitSyncUsername, secretName, GitSecretConfigKeyTokenUsername),
		envVarMutator(gitSyncPassword, secretName, GitSecretConfigKeyToken),
		containerEnvMutator(rootContainerEnvs5),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments[core.IDOf(rootDeployment5)] = rootDeployment5
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	serviceAccount5 := fake.ServiceAccountObject(
		rootReconcilerName5,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(label5),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(serviceAccount5)] = serviceAccount5
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Error(err)
	}

	crb.Subjects = addSubjectByName(crb.Subjects, rootReconcilerName5)
	crb.ResourceVersion = "5"
	crb.Generation = 5
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployments, ServiceAccounts, and ClusterRoleBindings successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs1), rs1); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating Deployment resources for rs1: my-root-sync
	rs1.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs1); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName1); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs1), rs1); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	// No changes to the CRB
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}

	rootsync.SetReconciling(wantRs1, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName))
	validateRootSyncStatus(t, wantRs1, fakeClient)

	rootContainerEnv1 = testReconciler.populateContainerEnvs(ctx, rs1, rootReconcilerName)
	rootDeployment1 = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv1),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment1)] = rootDeployment1
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs2), rs2); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating Deployment resources for rs2: root-sync
	rs2.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs2); err != nil {
		t.Fatalf("failed to update the repo sync request, got error: %v, want error: nil", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName2); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs2), rs2); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	// No changes to the CRB
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}

	rootsync.SetReconciling(wantRs2, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName2))
	validateRootSyncStatus(t, wantRs2, fakeClient)

	rootContainerEnv2 = testReconciler.populateContainerEnvs(ctx, rs2, rootReconcilerName2)
	rootDeployment2 = rootSyncDeployment(rootReconcilerName2,
		setServiceAccountName(rootReconcilerName2),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv2),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment2)] = rootDeployment2
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs3), rs3); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating  Deployment resources for rs3: my-rs-3
	rs3.Spec.Git.Revision = gitUpdatedRevision
	rs3.Spec.Git.Revision = gitUpdatedRevision
	if err := fakeClient.Update(ctx, rs3); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName3); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// No changes to the CRB
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}

	rootsync.SetReconciling(wantRs3, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName3))
	validateRootSyncStatus(t, wantRs3, fakeClient)

	rootContainerEnv3 = testReconciler.populateContainerEnvs(ctx, rs3, rootReconcilerName3)
	rootDeployment3 = rootSyncDeployment(rootReconcilerName3,
		setServiceAccountName(rootReconcilerName3),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnv3),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment3)] = rootDeployment3
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test garbage collecting ClusterRoleBinding after all RootSyncs are deleted
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

	// Subject for rs1 is removed from ClusterRoleBinding.Subjects
	crb.Subjects = deleteSubjectByName(crb.Subjects, rootReconcilerName)
	crb.ResourceVersion = "6"
	crb.Generation = 6
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	validateRootGeneratedResourcesDeleted(t, fakeClient, rootReconcilerName)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully deleted")

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

	// Subject for rs2 is removed from ClusterRoleBinding.Subjects
	crb.Subjects = deleteSubjectByName(crb.Subjects, rootReconcilerName2)
	crb.ResourceVersion = "7"
	crb.Generation = 7
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	validateRootGeneratedResourcesDeleted(t, fakeClient, rootReconcilerName2)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully deleted")

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

	// Subject for rs3 is removed from ClusterRoleBinding.Subjects
	crb.Subjects = deleteSubjectByName(crb.Subjects, rootReconcilerName3)
	crb.ResourceVersion = "8"
	crb.Generation = 8
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	validateRootGeneratedResourcesDeleted(t, fakeClient, rootReconcilerName3)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully deleted")

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

	// Subject for rs4 is removed from ClusterRoleBinding.Subjects
	crb.Subjects = deleteSubjectByName(crb.Subjects, rootReconcilerName4)
	crb.ResourceVersion = "9"
	crb.Generation = 9
	if err := validateClusterRoleBinding(crb, fakeClient); err != nil {
		t.Error(err)
	}
	validateRootGeneratedResourcesDeleted(t, fakeClient, rootReconcilerName4)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully deleted")

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

	// Verify the ClusterRoleBinding of the root-reconciler is deleted
	if err := validateResourceDeleted(core.IDOf(crb), fakeClient); err != nil {
		t.Error(err)
	}
	validateRootGeneratedResourcesDeleted(t, fakeClient, rootReconcilerName5)
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully deleted")
}

func validateRootGeneratedResourcesDeleted(t *testing.T, fakeClient *syncerFake.Client, reconcilerName string) {
	t.Helper()

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

	// ReconcilerManager doesn't manage the RootSync Secret
}

func TestMapSecretToRootSyncs(t *testing.T) {
	testSecretName := "ssh-test"
	caCertSecret := "cert-pub"
	rs1 := rootSyncWithGit("rs-1", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	rs2 := rootSyncWithGit("rs-2", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	rs3 := rootSyncWithGit("rs-3", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(testSecretName))
	rs4 := rootSyncWithGit("rs-4", rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthNone), rootsyncCACert(v1beta1.GitSource, caCertSecret))
	rs5 := rootSyncWithOCI("rs-5", rootsyncOCIAuthType(configsync.AuthNone), rootsyncCACert(v1beta1.OciSource, caCertSecret))
	rs6 := rootSyncWithHelm("rs-6", rootsyncHelmAuthType(configsync.AuthNone), rootsyncCACert(v1beta1.HelmSource, caCertSecret))

	testCases := []struct {
		name   string
		secret client.Object
		want   []reconcile.Request
	}{
		{
			name:   "A secret from the default namespace",
			secret: fake.SecretObject("s1", core.Namespace("default")),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A secret from the %s namespace starting with %s", configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(fmt.Sprintf("%s-bookstore", core.NsReconcilerPrefix), core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A secret 'any' from the %s namespace NOT starting with %s, no mapping RootSync", configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject("any", core.Namespace(configsync.ControllerNamespace)),
			want:   nil,
		},
		{
			name:   fmt.Sprintf("A secret %q from the %s namespace NOT starting with %s", rootsyncSSHKey, configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(rootsyncSSHKey, core.Namespace(configsync.ControllerNamespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-1",
						Namespace: configsync.ControllerNamespace,
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-2",
						Namespace: configsync.ControllerNamespace,
					},
				},
			},
		},
		{
			name:   fmt.Sprintf("A secret %q from the %s namespace NOT starting with %s", testSecretName, configsync.ControllerNamespace, core.NsReconcilerPrefix+"-"),
			secret: fake.SecretObject(testSecretName, core.Namespace(configsync.ControllerNamespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-3",
						Namespace: configsync.ControllerNamespace,
					},
				},
			},
		},
		{
			name:   fmt.Sprintf("A caCertSecretRef %s from the c-m-s namespace with mapping RootSyncs", caCertSecret),
			secret: fake.SecretObject(caCertSecret, core.Namespace(configsync.ControllerNamespace)),
			want: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-4",
						Namespace: configsync.ControllerNamespace,
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-5",
						Namespace: configsync.ControllerNamespace,
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Name:      "rs-6",
						Namespace: configsync.ControllerNamespace,
					},
				},
			},
		},
	}

	_, _, testReconciler := setupRootReconciler(t, rs1, rs2, rs3, rs4, rs5, rs6)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := testReconciler.mapSecretToRootSyncs(tc.secret)
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

func TestInjectFleetWorkloadIdentityCredentialsToRootSync(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(configsync.AuthGCPServiceAccount), rootsyncGCPSAEmail(gcpSAEmail))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))
	// The membership doesn't have WorkloadIdentityPool and IdentityProvider specified, so FWI creds won't be injected.
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
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())

	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		gceNodeMutator(),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

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
			// Configuring WorkloadIdentityPool and IdentityProvider to validate if FWI creds are injected.
			WorkloadIdentityPool: workloadIdentityPool,
			IdentityProvider:     "https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster",
		},
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fleetWorkloadIdentityMutator(workloadIdentityPool),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments = map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	// compare Deployment.
	if err := validateDeployments(wantDeployments, fakeDynamicClient,
		// Validate the credentials are injected in the askpass container
		validateContainerEnv(reconcilermanager.GCENodeAskpassSidecar, gsaEmailEnvKey, gcpSAEmail),
		validateContainerEnv(reconcilermanager.GCENodeAskpassSidecar, googleApplicationCredentialsEnvKey, filepath.Join(gcpKSATokenDir, googleApplicationCredentialsFile)),
	); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating RootSync resources with SSH auth type.
	rs.Spec.Auth = configsync.AuthSSH
	rs.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: rootsyncSSHKey}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	// Test updating RootSync resources with None auth type.
	rs.Spec.Auth = configsync.AuthNone
	rs.Spec.SecretRef = &v1beta1.SecretReference{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneGitContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncWithHelm(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = helmParsedDeployment
	secretName := "helm-secret"
	ctx := context.Background()

	// Test 1: creating RootSync resources with Token auth type
	rs := rootSyncWithHelm(rootsyncName,
		rootsyncHelmAuthType(configsync.AuthToken), rootsyncHelmSecretRef(secretName))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	helmSecret := secretObj(t, secretName, configsync.AuthToken, v1beta1.HelmSource, core.Namespace(rs.Namespace))
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, helmSecret)

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	// test 1: validations
	// Get RootSync to refresh the generation, which is passed as the `SYNC_GENERATION` env.
	// The generation is updated because the validateRoleRefs function resets the spec.override field.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	labels := map[string]string{
		metadata.SyncNamespaceLabel:       rs.Namespace,
		metadata.SyncNameLabel:            rs.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}
	ksaNoGSAAnnotation := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(labels),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(ksaNoGSAAnnotation): ksaNoGSAAnnotation}
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		helmSecretMutator(secretName),
		containerResourcesMutator(resourceOverrides),
		envVarMutator(helmSyncName, secretName, HelmSecretKeyUsername),
		envVarMutator(helmSyncPassword, secretName, HelmSecretKeyPassword),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test 2: updating RootSync resources with None auth type.
	rs.Spec.Helm.Auth = configsync.AuthNone
	rs.Spec.Helm.SecretRef = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 2: validations
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneHelmContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test 3: switch to authenticate with `gcpserviceaccount` type using GKE WI.
	existing := rs.DeepCopy()
	rs.Spec.Helm.Auth = configsync.AuthGCPServiceAccount
	rs.Spec.Helm.GCPServiceAccountEmail = gcpSAEmail
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 3: validations
	ksaWithGSAAnnotation := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.Helm.GCPServiceAccountEmail),
		core.Labels(labels),
		core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(ksaWithGSAAnnotation)] = ksaWithGSAAnnotation
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneHelmContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test 4: authenticate with `gcpserviceaccount` type using Fleet WI.
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

	// test 4: validations
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fwiMutator(workloadIdentityPool, reconcilermanager.HelmSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test 5: Migrate from GSA to KSA for authentication using Fleet WI.
	existing = rs.DeepCopy()
	rs.Spec.Helm.Auth = configsync.AuthK8sServiceAccount
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 5: validations
	wantServiceAccounts[core.IDOf(ksaNoGSAAnnotation)] = ksaNoGSAAnnotation
	ksaNoGSAAnnotation.ResourceVersion = "3"
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			// `service_account_impersonation_url` is removed from the annotation,
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fwiMutator(workloadIdentityPool, reconcilermanager.HelmSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("5"), setGeneration(5),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// Test 6: override helm-sync resources
	t.Log("Test overriding the cpu request and memory limits of the helm-sync container")
	overrideHelmSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.HelmSync,
			CPURequest:    resource.MustParse("200m"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	existing = rs.DeepCopy()
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideHelmSyncResources,
		},
	}
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	// test 6: validations
	resourceOverrides = setContainerResourceDefaults(overrideHelmSyncResources, ReconcilerContainerResourceDefaults())
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			// `service_account_impersonation_url` is removed from the annotation,
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fwiMutator(workloadIdentityPool, reconcilermanager.HelmSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("6"), setGeneration(6),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncWithOCI(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	ctx := context.Background()

	// test 1: authenticate with `none` type for public OCI images.
	rs := rootSyncWithOCI(rootsyncName, rootsyncOCIAuthType(configsync.AuthNone))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs)
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	// test 1: validations
	// Get RootSync to refresh the generation, which is passed as the `SYNC_GENERATION` env.
	// The generation is updated because the validateRoleRefs function resets the spec.override field.
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	labels := map[string]string{
		metadata.SyncNamespaceLabel:       rs.Namespace,
		metadata.SyncNameLabel:            rs.Name,
		metadata.SyncKindLabel:            testReconciler.syncKind,
		metadata.ConfigSyncManagedByLabel: reconcilermanager.ManagerName,
	}

	ksaNoGSAAnnotation := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Labels(labels),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	wantServiceAccounts := map[core.ID]*corev1.ServiceAccount{core.IDOf(ksaNoGSAAnnotation): ksaNoGSAAnnotation}
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Resources successfully created")

	// test 2: switch to authenticate with `gcenode` type.
	t.Log("Test updating RootSync resources with gcenode auth type.")
	rs.Spec.Oci.Auth = configsync.AuthGCENode
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 2: validations
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// test 3: switch to authenticate with `gcpserviceaccount` type using GKE WI.
	t.Log("Test updating RootSync resources with gcpserviceaccount auth type.")
	existing := rs.DeepCopy()
	rs.Spec.Oci.Auth = configsync.AuthGCPServiceAccount
	rs.Spec.Oci.GCPServiceAccountEmail = gcpSAEmail
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 3: validations
	ksaWithGSAAnnotation := fake.ServiceAccountObject(
		rootReconcilerName,
		core.Namespace(configsync.ControllerNamespace),
		core.Annotation(GCPSAAnnotationKey, rs.Spec.Oci.GCPServiceAccountEmail),
		core.Labels(labels),
		core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
	)
	wantServiceAccounts[core.IDOf(ksaWithGSAAnnotation)] = ksaWithGSAAnnotation
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		containersWithRepoVolumeMutator(noneOciContainers()),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// test 4: authenticate with `gcpserviceaccount` type using Fleet WI.
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

	// test 4: validations
	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"service_account_impersonation_url":"https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/config-sync@cs-project.iam.gserviceaccount.com:generateAccessToken","subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fwiMutator(workloadIdentityPool, reconcilermanager.OciSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("4"), setGeneration(4),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// test 5: Migrate from GSA to KSA for authentication using Fleet WI.
	existing = rs.DeepCopy()
	rs.Spec.Oci.Auth = configsync.AuthK8sServiceAccount
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 5: validations
	wantServiceAccounts[core.IDOf(ksaNoGSAAnnotation)] = ksaNoGSAAnnotation
	ksaNoGSAAnnotation.ResourceVersion = "3"
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)

	if err := validateServiceAccounts(wantServiceAccounts, fakeClient); err != nil {
		t.Errorf("ServiceAccount validation failed: %v", err)
	}
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setAnnotations(map[string]string{
			// `service_account_impersonation_url` is removed from the annotation,
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		setServiceAccountName(rootReconcilerName),
		fwiMutator(workloadIdentityPool, reconcilermanager.OciSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("5"), setGeneration(5),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	// test 6: override the CPU request and memory limits of the oci-sync container
	t.Log("Test overriding the cpu request and memory limits of the oci-sync container")
	overrideOciSyncResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.OciSync,
			CPURequest:    resource.MustParse("200m"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}

	existing = rs.DeepCopy()
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideOciSyncResources,
		},
	}
	if err := fakeClient.Patch(ctx, rs, client.MergeFrom(existing)); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}

	// test 6: validations
	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(overrideOciSyncResources, ReconcilerContainerResourceDefaults())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		setAnnotations(map[string]string{
			// `service_account_impersonation_url` is removed from the annotation,
			metadata.FleetWorkloadIdentityCredentials: `{"audience":"identitynamespace:test-gke-dev.svc.id.goog:https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster","credential_source":{"file":"/var/run/secrets/tokens/gcp-ksa/token"},"subject_token_type":"urn:ietf:params:oauth:token-type:jwt","token_url":"https://sts.googleapis.com/v1/token","type":"external_account"}`,
		}),
		fwiMutator(workloadIdentityPool, reconcilermanager.OciSync),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("6"), setGeneration(6),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootSyncSpecValidation(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := fake.RootSyncObjectV1Beta1(rootsyncName)
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, _, testReconciler := setupRootReconciler(t, rs)

	// Verify unsupported source type
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs := fake.RootSyncObjectV1Beta1(rootsyncName)
	rootsync.SetStalled(wantRs, "Validation", validate.InvalidSourceType(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing Git
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.GitSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingGitSpec(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing Oci
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingOciSpec(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingHelmSpec(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing OCI image
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingOciImage(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify invalid OCI Auth
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{Image: ociImage}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.InvalidOciAuthType(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm repo
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRootSync{}
	rs.Spec.Oci = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingHelmRepo(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify missing Helm chart
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRootSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.MissingHelmChart(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify invalid Helm Auth
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRootSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo, Chart: helmChart}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	rootsync.SetStalled(wantRs, "Validation", validate.InvalidHelmAuthType(rs))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify valid OCI spec
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Git = nil
	rs.Spec.Helm = nil
	rs.Spec.Oci = &v1beta1.Oci{Image: ociImage, Auth: configsync.AuthNone}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	// Clear the stalled condition
	rs.Status = v1beta1.RootSyncStatus{}
	if err := fakeClient.Status().Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = rootReconcilerName
	wantRs.Status.Conditions = nil // clear the stalled condition
	rootsync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName))
	validateRootSyncStatus(t, wantRs, fakeClient)

	// verify valid Helm spec
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Git = nil
	rs.Spec.Oci = nil
	rs.Spec.Helm = &v1beta1.HelmRootSync{HelmBase: v1beta1.HelmBase{Repo: helmRepo, Chart: helmChart, Auth: configsync.AuthNone}}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	// Clear the stalled condition
	rs.Status = v1beta1.RootSyncStatus{}
	if err := fakeClient.Status().Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v", err)
	}
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantRs.Spec = rs.Spec
	wantRs.Status.Reconciler = rootReconcilerName
	wantRs.Status.Conditions = nil // clear the stalled condition
	rootsync.SetReconciling(wantRs, "Deployment",
		fmt.Sprintf("Deployment (config-management-system/%s) InProgress: Replicas: 0/1", rootReconcilerName))
	validateRootSyncStatus(t, wantRs, fakeClient)
}

func TestRootSyncReconcileStaleClientCache(t *testing.T) {
	rs := fake.RootSyncObjectV1Beta1(rootsyncName)
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, _, testReconciler := setupRootReconciler(t, rs)
	ctx := context.Background()

	rs.ResourceVersion = "1"
	rs = fake.RootSyncObjectV1Beta1(rootsyncName)
	err := fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	oldRS := rs.DeepCopy()

	// Reconcile should succeed and update the RootSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect Stalled condition with True status, because the RootSync is invalid
	rs = fake.RootSyncObjectV1Beta1(rootsyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "KNV1061: RootSyncs must specify spec.sourceType", "unexpected Stalled condition message")

	// Simulate stale cache (rollback to previous resource version)
	err = fakeClient.Storage().TestPut(oldRS)
	require.NoError(t, err)

	// Expect next Reconcile to succeed but NOT update the RootSync
	// This means the client cache hasn't been updated and isn't returning the latest version.
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Simulate cache update from watch event (roll forward to the latest resource version)
	err = fakeClient.Storage().TestPut(rs)
	require.NoError(t, err)

	// Reconcile should succeed but NOT update the RootSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect the same Stalled condition error message
	rs = fake.RootSyncObjectV1Beta1(rootsyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition = rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "KNV1061: RootSyncs must specify spec.sourceType", "unexpected Stalled condition message")

	// Simulate a spec update, with ResourceVersion updated by the apiserver
	rs = fake.RootSyncObjectV1Beta1(rootsyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	rs.Spec.SourceType = string(v1beta1.GitSource)
	err = fakeClient.Update(ctx, rs)
	require.NoError(t, err, "unexpected Update error")

	// Reconcile should succeed and update the RootSync
	_, err = testReconciler.Reconcile(ctx, reqNamespacedName)
	require.NoError(t, err, "unexpected Reconcile error")

	// Expect Stalled condition with True status, because the RootSync is differently invalid
	rs = fake.RootSyncObjectV1Beta1(rootsyncName)
	err = fakeClient.Get(ctx, core.ObjectNamespacedName(rs), rs)
	require.NoError(t, err, "unexpected Get error")
	reconcilingCondition = rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
	require.NotNilf(t, reconcilingCondition, "status: %+v", rs.Status)
	require.Equal(t, reconcilingCondition.Status, metav1.ConditionTrue, "unexpected Stalled condition status")
	require.Contains(t, reconcilingCondition.Message, "RootSyncs must specify spec.git when spec.sourceType is \"git\"", "unexpected Stalled condition message")
}

func TestPopulateRootContainerEnvs(t *testing.T) {
	defaults := map[string]map[string]string{
		reconcilermanager.HydrationController: {
			reconcilermanager.HydrationPollingPeriod: hydrationPollingPeriod.String(),
			reconcilermanager.NamespaceNameKey:       ":root",
			reconcilermanager.ReconcilerNameKey:      rootReconcilerName,
			reconcilermanager.ScopeKey:               ":root",
			reconcilermanager.SourceTypeKey:          string(gitSource),
			reconcilermanager.SyncDirKey:             rootsyncDir,
		},
		reconcilermanager.Reconciler: {
			reconcilermanager.ClusterNameKey:          testCluster,
			reconcilermanager.ScopeKey:                ":root",
			reconcilermanager.SyncNameKey:             rootsyncName,
			reconcilermanager.NamespaceNameKey:        ":root",
			reconcilermanager.SyncGenerationKey:       "1",
			reconcilermanager.ReconcilerNameKey:       rootReconcilerName,
			reconcilermanager.SyncDirKey:              rootsyncDir,
			reconcilermanager.SourceRepoKey:           rootsyncRepo,
			reconcilermanager.SourceTypeKey:           string(gitSource),
			filesystem.SourceFormatKey:                "",
			reconcilermanager.NamespaceStrategy:       string(configsync.NamespaceStrategyImplicit),
			reconcilermanager.StatusMode:              "enabled",
			reconcilermanager.SourceBranchKey:         "master",
			reconcilermanager.SourceRevKey:            "HEAD",
			reconcilermanager.APIServerTimeout:        restconfig.DefaultTimeout.String(),
			reconcilermanager.ReconcileTimeout:        "5m0s",
			reconcilermanager.ReconcilerPollingPeriod: "50ms",
			reconcilermanager.RenderingEnabled:        "false",
		},
		reconcilermanager.GitSync: {
			GitSyncKnownHosts: "false",
			GitSyncRepo:       rootsyncRepo,
			gitSyncRef:        "master",
			GitSyncDepth:      "1",
			gitSyncPeriod:     "15s",
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
		rootSync *v1beta1.RootSync
		expected map[string][]corev1.EnvVar
	}{
		{
			name: "no override uses default value",
			rootSync: rootSyncWithGit(rootsyncName,
				rootsyncRenderingRequired(false),
			),
			expected: createEnv(map[string]map[string]string{}),
		},
		{
			name: "override uses override value",
			rootSync: rootSyncWithGit(rootsyncName,
				rootsyncOverrideAPIServerTimeout(metav1.Duration{Duration: 40 * time.Second}),
				rootsyncRenderingRequired(false),
			),
			expected: createEnv(map[string]map[string]string{
				reconcilermanager.Reconciler: {reconcilermanager.APIServerTimeout: "40s"},
			}),
		},
		{
			name: "rendering-required annotation sets env var",
			rootSync: rootSyncWithGit(rootsyncName,
				rootsyncRenderingRequired(true),
			),
			expected: createEnv(map[string]map[string]string{
				reconcilermanager.Reconciler: {reconcilermanager.RenderingEnabled: "true"},
			}),
		},
		{
			name: "dynamic-ns-selector-enabled annotation sets env var",
			rootSync: rootSyncWithGit(rootsyncName,
				rootsyncRenderingRequired(false),
				rootsyncDynamicNSSelectorEnabled(true),
			),
			expected: createEnv(map[string]map[string]string{
				reconcilermanager.Reconciler: {reconcilermanager.DynamicNSSelectorEnabled: "true"},
			}),
		},
	}

	ctx := context.Background()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, testReconciler := setupRootReconciler(t, tc.rootSync, secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(tc.rootSync.Namespace)))

			env := testReconciler.populateContainerEnvs(ctx, tc.rootSync, rootReconcilerName)

			for container, vars := range env {
				if diff := cmp.Diff(tc.expected[container], vars, cmpopts.EquateEmpty(), cmpopts.SortSlices(func(a, b corev1.EnvVar) bool { return a.Name < b.Name })); diff != "" {
					t.Errorf("%s/%s: unexpected env; diff: %s", tc.name, container, diff)
				}
			}
		})
	}
}

func TestUpdateRootReconcilerLogLevelWithOverride(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaults())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

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
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			LogLevels: overrideLogLevel,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerArgsMutator(containerArgs),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
}

func TestCreateAndUpdateRootReconcilerWithOverrideOnAutopilot(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH),
		rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	fakeClient, fakeDynamicClient, testReconciler := setupRootReconciler(t, util.FakeAutopilotWebhookObject(), rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test creating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatal(err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs := testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides := setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaultsForAutopilot())
	rootDeployment := rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("1"), setGeneration(1),
	)
	wantDeployments := map[core.ID]*appsv1.Deployment{core.IDOf(rootDeployment): rootDeployment}

	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully created")

	// Test overriding the CPU resources of the reconciler and hydration-container and the memory resources of the git-sync container
	overrideSelectedResources := []v1beta1.ContainerResourcesSpec{
		{
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("1"),
			CPULimit:      resource.MustParse("1.2"),
		},
		{
			ContainerName: reconcilermanager.HydrationController,
			CPULimit:      resource.MustParse("0.8"),
		},
		{
			ContainerName: reconcilermanager.GitSync,
			MemoryRequest: resource.MustParse("800Gi"),
			MemoryLimit:   resource.MustParse("888Gi"),
		},
	}

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = &v1beta1.RootSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			Resources: overrideSelectedResources,
		},
	}
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(overrideSelectedResources, ReconcilerContainerResourceDefaultsForAutopilot())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("2"), setGeneration(2),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")

	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}
	rs.Spec.Override = nil
	if err := fakeClient.Update(ctx, rs); err != nil {
		t.Fatalf("failed to update the root sync request, got error: %v, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error upon request update, got error: %q, want error: nil", err)
	}
	if err := fakeClient.Get(ctx, client.ObjectKeyFromObject(rs), rs); err != nil {
		t.Fatalf("failed to get the root sync: %v", err)
	}

	rootContainerEnvs = testReconciler.populateContainerEnvs(ctx, rs, rootReconcilerName)
	resourceOverrides = setContainerResourceDefaults(nil, ReconcilerContainerResourceDefaultsForAutopilot())
	rootDeployment = rootSyncDeployment(rootReconcilerName,
		setServiceAccountName(rootReconcilerName),
		secretMutator(rootsyncSSHKey),
		containerResourcesMutator(resourceOverrides),
		containerEnvMutator(rootContainerEnvs),
		setUID("1"), setResourceVersion("3"), setGeneration(3),
	)
	wantDeployments[core.IDOf(rootDeployment)] = rootDeployment
	if err := validateDeployments(wantDeployments, fakeDynamicClient); err != nil {
		t.Errorf("Deployment validation failed. err: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Log("Deployment successfully updated")
}

func TestRootReconcilerGetKnownHosts(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	_, _, testReconciler := setupRootReconciler(t, rs, secretObjWithKnownHosts(t, rootsyncSSHKey, core.Namespace(rs.Namespace)))

	// Test performing reconcile
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	isKnownHosts := testReconciler.isKnownHostsEnabled(rs.Spec.Git.Auth)

	require.Equal(t, true, isKnownHosts)
}

func TestRootReconcilerWithoutKnownHosts(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	reqNamespacedName := namespacedName(rs.Name, rs.Namespace)
	_, _, testReconciler := setupRootReconciler(t, rs, secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace)))

	// Test performing reconcile
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	isKnownHosts := testReconciler.isKnownHostsEnabled(rs.Spec.Git.Auth)

	require.Equal(t, false, isKnownHosts)
}

func validateRootSyncStatus(t *testing.T, want *v1beta1.RootSync, fakeClient *syncerFake.Client) {
	t.Helper()

	key := client.ObjectKeyFromObject(want)
	got := &v1beta1.RootSync{}
	ctx := context.Background()
	err := fakeClient.Get(ctx, key, got)
	require.NoError(t, err, "RootSync[%s] not found", key)

	asserter := testutil.NewAsserter(
		cmpopts.IgnoreFields(v1beta1.RootSyncCondition{}, "LastUpdateTime", "LastTransitionTime"))
	// cmpopts.SortSlices(func(x, y v1beta1.RootSyncCondition) bool { return x.Message < y.Message })
	asserter.Equal(t, want.Status.Conditions, got.Status.Conditions, "Unexpected status conditions")
}

type depMutator func(*appsv1.Deployment)

func rootSyncDeployment(reconcilerName string, muts ...depMutator) *appsv1.Deployment {
	dep := fake.DeploymentObject(
		core.Namespace(configsync.ControllerNamespace),
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

func setReplicas(replicas int32) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Replicas = pointer.Int32(replicas)
	}
}

func setResourceVersion(rv string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.ResourceVersion = rv
	}
}

func setUID(uid types.UID) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.UID = uid
	}
}

func setGeneration(gen int64) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Generation = gen
	}
}

func setServiceAccountName(name string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.ServiceAccountName = name
	}
}

func secretMutator(secretName string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = deploymentSecretVolumes(secretName, "")
		dep.Spec.Template.Spec.Containers = secretMountContainers("")
	}
}

func helmSecretMutator(secretName string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = helmDeploymentSecretVolumes(secretName)
		dep.Spec.Template.Spec.Containers = helmSecretMountContainers()
	}
}

func caCertSecretMutator(secretName, caCertSecretName string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = deploymentSecretVolumes(secretName, caCertSecretName)
		dep.Spec.Template.Spec.Containers = secretMountContainers(caCertSecretName)
	}
}

func envVarMutator(envName, secretName, key string) depMutator {
	return func(dep *appsv1.Deployment) {
		for i, con := range dep.Spec.Template.Spec.Containers {
			if con.Name == reconcilermanager.GitSync || con.Name == reconcilermanager.HelmSync {
				dep.Spec.Template.Spec.Containers[i].Env = append(dep.Spec.Template.Spec.Containers[i].Env, corev1.EnvVar{
					Name: envName,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: secretName,
							},
							Key: key,
						},
					},
				})
			}
		}
	}
}

func containerEnvMutator(containerEnvs map[string][]corev1.EnvVar) depMutator {
	return func(dep *appsv1.Deployment) {
		for i, con := range dep.Spec.Template.Spec.Containers {
			dep.Spec.Template.Spec.Containers[i].Env = append(dep.Spec.Template.Spec.Containers[i].Env,
				containerEnvs[con.Name]...)
		}
	}
}

func containerArgsMutator(containerArgs map[string][]string) depMutator {
	return func(dep *appsv1.Deployment) {
		for i, con := range dep.Spec.Template.Spec.Containers {
			dep.Spec.Template.Spec.Containers[i].Args = containerArgs[con.Name]
		}
	}
}

func gceNodeMutator() depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "repo"}}
		dep.Spec.Template.Spec.Containers = gceNodeContainers()
	}
}

func fwiVolume(workloadIdentityPool string) corev1.Volume {
	return corev1.Volume{
		Name: gcpKSAVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							Audience:          workloadIdentityPool,
							ExpirationSeconds: &expirationSeconds,
							Path:              gsaTokenPath,
						},
					},
					{
						DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{
							{
								Path: googleApplicationCredentialsFile,
								FieldRef: &corev1.ObjectFieldSelector{
									APIVersion: "v1",
									FieldPath:  fmt.Sprintf("metadata.annotations['%s']", metadata.FleetWorkloadIdentityCredentials),
								},
							},
						}},
					},
				},
				DefaultMode: &defaultMode,
			},
		},
	}
}

func fleetWorkloadIdentityMutator(workloadIdentityPool string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: "repo"},
			fwiVolume(workloadIdentityPool),
		}
		dep.Spec.Template.Spec.Containers = fleetWorkloadIdentityContainers()
	}
}

func fleetWorkloadIdentityContainers() []corev1.Container {
	containers := noneGitContainers()
	containers = append(containers, corev1.Container{
		Name: reconcilermanager.GCENodeAskpassSidecar,
		Env: []corev1.EnvVar{{
			Name:  googleApplicationCredentialsEnvKey,
			Value: filepath.Join(gcpKSATokenDir, googleApplicationCredentialsFile),
		}},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      gcpKSAVolumeName,
			ReadOnly:  true,
			MountPath: gcpKSATokenDir,
		}},
		Args: defaultArgs(),
	})
	return containers
}

func fwiMutator(workloadIdentityPool, syncContainerName string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{Name: "repo"},
			fwiVolume(workloadIdentityPool),
		}
		dep.Spec.Template.Spec.Containers = fwiContainers(syncContainerName)
	}
}

func fwiContainers(syncContainerName string) []corev1.Container {
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name: syncContainerName,
			Args: defaultArgs(),
			Env: []corev1.EnvVar{{
				Name:  googleApplicationCredentialsEnvKey,
				Value: filepath.Join(gcpKSATokenDir, googleApplicationCredentialsFile),
			}},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
				{
					Name:      gcpKSAVolumeName,
					ReadOnly:  true,
					MountPath: gcpKSATokenDir,
				}},
		},
	}
}

func containersWithRepoVolumeMutator(containers []corev1.Container) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "repo"}}
		dep.Spec.Template.Spec.Containers = containers
	}
}

func setAnnotations(annotations map[string]string) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Template.Annotations = annotations
	}
}

func containerResourcesMutator(overrides []v1beta1.ContainerResourcesSpec) depMutator {
	return func(dep *appsv1.Deployment) {
		for i, container := range dep.Spec.Template.Spec.Containers {
			for _, override := range overrides {
				if override.ContainerName == container.Name {
					mutateContainerResourceRequestsLimits(&container, override)
					dep.Spec.Template.Spec.Containers[i] = container
				}
			}
		}
	}
}

func mutateContainerResourceRequestsLimits(container *corev1.Container, resourcesSpec v1beta1.ContainerResourcesSpec) {
	if !resourcesSpec.CPURequest.IsZero() {
		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		container.Resources.Requests[corev1.ResourceCPU] = resourcesSpec.CPURequest
	} else {
		delete(container.Resources.Requests, corev1.ResourceCPU)
	}

	if !resourcesSpec.CPULimit.IsZero() {
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}
		container.Resources.Limits[corev1.ResourceCPU] = resourcesSpec.CPULimit
	} else {
		delete(container.Resources.Limits, corev1.ResourceCPU)
	}

	if !resourcesSpec.MemoryRequest.IsZero() {
		if container.Resources.Requests == nil {
			container.Resources.Requests = make(corev1.ResourceList)
		}
		container.Resources.Requests[corev1.ResourceMemory] = resourcesSpec.MemoryRequest
	} else {
		delete(container.Resources.Requests, corev1.ResourceMemory)
	}

	if !resourcesSpec.MemoryLimit.IsZero() {
		if container.Resources.Limits == nil {
			container.Resources.Limits = make(corev1.ResourceList)
		}
		container.Resources.Limits[corev1.ResourceMemory] = resourcesSpec.MemoryLimit
	} else {
		delete(container.Resources.Limits, corev1.ResourceMemory)
	}
}

func defaultArgs() []string {
	return []string{
		"-v=0",
	}
}

func defaultGitSyncArgs() []string {
	return []string{
		"-v=5",
	}
}

func defaultContainers() []corev1.Container {
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.GitSync,
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
				{Name: "git-creds", MountPath: "/etc/git-secret", ReadOnly: true},
			},
			Args: defaultGitSyncArgs(),
		},
		{
			Name: reconcilermanager.GCENodeAskpassSidecar,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.OciSync,
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
			},
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HelmSync,
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
				{Name: "helm-creds", MountPath: "/etc/helm-secret", ReadOnly: true},
			},
			Args: defaultArgs(),
		},
	}
}

func secretMountContainers(caCertSecret string) []corev1.Container {
	gitSyncVolumeMounts := []corev1.VolumeMount{
		{Name: "repo", MountPath: "/repo"},
		{Name: "git-creds", MountPath: "/etc/git-secret", ReadOnly: true},
	}
	if caCertSecret != "" {
		gitSyncVolumeMounts = append(gitSyncVolumeMounts, corev1.VolumeMount{
			Name: "ca-cert", MountPath: "/etc/ca-cert", ReadOnly: true,
		})
	}
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name:         reconcilermanager.GitSync,
			VolumeMounts: gitSyncVolumeMounts,
			Args:         defaultGitSyncArgs(),
		},
	}
}

func helmSecretMountContainers() []corev1.Container {
	helmSyncVolumeMounts := []corev1.VolumeMount{
		{Name: "repo", MountPath: "/repo"},
		{Name: "helm-creds", MountPath: "/etc/helm-secret", ReadOnly: true},
	}
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name:         reconcilermanager.HelmSync,
			VolumeMounts: helmSyncVolumeMounts,
			Args:         defaultArgs(),
		},
	}
}

func noneGitContainers() []corev1.Container {
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.GitSync,
			Args: defaultGitSyncArgs(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
			}},
	}
}

func noneOciContainers() []corev1.Container {
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.OciSync,
			Args: defaultArgs(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
			}},
	}
}

func noneHelmContainers() []corev1.Container {
	return []corev1.Container{
		{
			Name: reconcilermanager.Reconciler,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HydrationController,
			Args: defaultArgs(),
		},
		{
			Name: reconcilermanager.HelmSync,
			Args: defaultArgs(),
			VolumeMounts: []corev1.VolumeMount{
				{Name: "repo", MountPath: "/repo"},
			}},
	}
}

func gceNodeContainers() []corev1.Container {
	containers := noneGitContainers()
	containers = append(containers, corev1.Container{
		Name: reconcilermanager.GCENodeAskpassSidecar,
		Args: defaultArgs(),
	})
	return containers
}

func deploymentSecretVolumes(secretName, caCertSecretName string) []corev1.Volume {
	volumes := []corev1.Volume{
		{Name: "repo"},
		{Name: "git-creds", VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		}},
	}
	if useCACert(caCertSecretName) {
		volumes = append(volumes, corev1.Volume{
			Name: "ca-cert", VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: caCertSecretName,
					Items: []corev1.KeyToPath{
						{
							Key:  CACertSecretKey,
							Path: CACertSecretKey,
						},
					},
					DefaultMode: &defaultMode},
			},
		})
	}
	return volumes
}

func helmDeploymentSecretVolumes(secretName string) []corev1.Volume {
	volumes := []corev1.Volume{
		{Name: "repo"},
		{Name: "helm-creds", VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		}},
	}
	return volumes
}
