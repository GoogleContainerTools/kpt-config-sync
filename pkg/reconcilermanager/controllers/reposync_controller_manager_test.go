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
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestRepoSyncReconcilerDeploymentLifecycle validates that the
// RootSyncReconciler works with the ControllerManager.
// - Create a root-reconciler Deployment when a RootSync is created
// - Delete the root-reconciler Deployment when the RootSync is deleted
func TestRepoSyncReconcilerDeploymentLifecycle(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	t.Log("building RepoSyncReconciler")
	rs := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	secretObj := secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace))

	fakeClient, _, testReconciler := setupNSReconciler(t, secretObj)

	defer logObjectYAMLIfFailed(t, fakeClient, rs)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errCh := startControllerManager(ctx, t, fakeClient, testReconciler)

	// Wait for manager to exit before returning
	defer func() {
		cancel()
		t.Log("waiting for controller-manager to stop")
		for err := range errCh {
			require.NoError(t, err)
		}
	}()

	reconcilerKey := core.NsReconcilerObjectKey(rs.Namespace, rs.Name)

	t.Log("watching for reconciler deployment creation")
	watchCtx, watchCancel := context.WithTimeout(ctx, 10*time.Second)
	defer watchCancel()

	watcher, err := watchObjects(watchCtx, fakeClient, &appsv1.DeploymentList{})
	require.NoError(t, err)

	// Create RootSync
	err = fakeClient.Create(ctx, rs)
	require.NoError(t, err)

	var reconcilerObj *appsv1.Deployment
	err = watchObjectUntil(ctx, watcher, reconcilerKey, func(event watch.Event) error {
		t.Logf("reconciler deployment %s", event.Type)
		if event.Type == watch.Added || event.Type == watch.Modified {
			reconcilerObj = event.Object.(*appsv1.Deployment)
			// success! deployment was applied.
			// Since there's no deployment controller,
			// don't wait for availability.
			return nil
		}
		// keep watching
		return errors.Errorf("reconciler deployment %s", event.Type)
	})
	require.NoError(t, err)
	if reconcilerObj == nil {
		t.Fatal("timed out waiting for reconciler deployment to be applied")
	}

	t.Log("watching for reconciler deployment delete")
	watchCtx2, watchCancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer watchCancel2()

	watcher, err = watchObjects(watchCtx2, fakeClient, &appsv1.DeploymentList{})
	require.NoError(t, err)

	// Delete RootSync
	rs.ResourceVersion = "" // we don't care what the RV is when deleting
	err = fakeClient.Delete(ctx, rs)
	require.NoError(t, err)

	err = watchObjectUntil(ctx, watcher, reconcilerKey, func(event watch.Event) error {
		t.Logf("reconciler deployment %s", event.Type)
		if event.Type == watch.Deleted {
			reconcilerObj = event.Object.(*appsv1.Deployment)
			// success! deployment was deleted.
			return nil
		}
		// keep watching
		return errors.Errorf("reconciler deployment %s", event.Type)
	})
	require.NoError(t, err)
}

// TestRepoSyncReconcilerDeploymentDriftProtection validates that changes to
// specific managed fields of the reconciler deployment are reverted if changed
// by another client.
func TestRepoSyncReconcilerDeploymentDriftProtection(t *testing.T) {
	exampleObj := &appsv1.Deployment{}
	objKeyFunc := func(rs client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed reconciler deployment
		return core.NsReconcilerObjectKey(rs.Namespace, rs.Name)
	}
	var observedNames, expectedNames []string
	modify := func(obj client.Object) error {
		reconcilerObj := obj.(*appsv1.Deployment)
		initialName := reconcilerObj.Spec.Template.Spec.ServiceAccountName
		observedNames = []string{initialName}
		driftName := "seanboswell"
		expectedNames = []string{initialName, driftName, initialName}
		reconcilerObj.Spec.Template.Spec.ServiceAccountName = driftName
		return nil
	}
	validate := func(obj client.Object) error {
		reconcilerObj := obj.(*appsv1.Deployment)
		saName := reconcilerObj.Spec.Template.Spec.ServiceAccountName
		// Record new value, if different from last known value
		if saName != observedNames[len(observedNames)-1] {
			t.Logf("observed ServiceAccountName change: %s", saName)
			observedNames = append(observedNames, saName)
		}
		if cmp.Equal(expectedNames, observedNames) {
			// success - change observed and reverted
			return nil
		}
		// keep watching
		return errors.Errorf("spec.template.spec.serviceAccountName changes expected %+v, but found %+v",
			expectedNames, observedNames)
	}
	testRepoSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

// TestRepoSyncReconcilerServiceAccountDriftProtection validates that changes to
// specific managed fields of the reconciler service account are reverted if
// changed by another client.
func TestRepoSyncReconcilerServiceAccountDriftProtection(t *testing.T) {
	exampleObj := &corev1.ServiceAccount{}
	objKeyFunc := func(rs client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed service account
		return core.NsReconcilerObjectKey(rs.Namespace, rs.Name)
	}
	var observedValues, expectedValues []string
	modify := func(obj client.Object) error {
		saObj := obj.(*corev1.ServiceAccount)
		initialValue := saObj.Labels[metadata.SyncKindLabel]
		observedValues = []string{initialValue}
		driftValue := "seanboswell"
		expectedValues = []string{initialValue, driftValue, initialValue}
		saObj.Labels[metadata.SyncKindLabel] = driftValue
		return nil
	}
	validate := func(obj client.Object) error {
		saObj := obj.(*corev1.ServiceAccount)
		observedValue := saObj.Labels[metadata.SyncKindLabel]
		// Record new value, if different from last known value
		if observedValue != observedValues[len(observedValues)-1] {
			t.Logf("observed label change: %s", observedValue)
			observedValues = append(observedValues, observedValue)
		}
		if cmp.Equal(expectedValues, observedValues) {
			// success - change observed and reverted
			return nil
		}
		// keep watching
		return errors.Errorf("spec.metadata.labels[%q] changes expected %+v, but found %+v",
			metadata.SyncKindLabel, expectedValues, observedValues)
	}
	testRepoSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

// TestRepoSyncReconcilerRoleBindingDriftProtection validates that changes to
// specific managed fields of the reconciler role binding are reverted if
// changed by another client.
func TestRepoSyncReconcilerRoleBindingDriftProtection(t *testing.T) {
	exampleObj := &rbacv1.RoleBinding{}
	objKeyFunc := func(syncRef client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed robe binding
		return client.ObjectKey{
			Namespace: syncRef.Namespace,
			Name:      RepoSyncPermissionsName(),
		}
	}
	var observedValues, expectedValues []string
	modify := func(obj client.Object) error {
		crbObj := obj.(*rbacv1.RoleBinding)
		initialValue := crbObj.RoleRef.Name
		observedValues = []string{initialValue}
		driftValue := "seanboswell"
		expectedValues = []string{initialValue, driftValue, initialValue}
		crbObj.RoleRef.Name = driftValue
		return nil
	}
	validate := func(obj client.Object) error {
		crbObj := obj.(*rbacv1.RoleBinding)
		observedValue := crbObj.RoleRef.Name
		// Record new value, if different from last known value
		if observedValue != observedValues[len(observedValues)-1] {
			t.Logf("observed roleRef.name change: %s", observedValue)
			observedValues = append(observedValues, observedValue)
		}
		if cmp.Equal(expectedValues, observedValues) {
			// success - change observed and reverted
			return nil
		}
		// keep watching
		return errors.Errorf("roleRef.name changes expected %+v, but found %+v",
			expectedValues, observedValues)
	}
	testRepoSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

// TestRepoSyncReconcilerAuthSecretDriftProtection validates that changes to
// specific managed fields of the reconciler auth secret are reverted if changed
// by another client.
func TestRepoSyncReconcilerAuthSecretDriftProtection(t *testing.T) {
	exampleObj := &corev1.Secret{}
	objKeyFunc := func(syncRef client.ObjectKey) client.ObjectKey {
		reconcilerRef := core.NsReconcilerObjectKey(syncRef.Namespace, syncRef.Name)
		// reconciler-manager managed auth secret
		return client.ObjectKey{
			Namespace: reconcilerRef.Namespace,
			Name:      ReconcilerResourceName(reconcilerRef.Name, reposyncSSHKey),
		}
	}
	var observedValues, expectedValues []string
	modify := func(obj client.Object) error {
		crbObj := obj.(*corev1.Secret)
		initialValue := string(crbObj.Data[string(configsync.AuthSSH)])
		observedValues = []string{initialValue}
		driftValue := "seanboswell"
		expectedValues = []string{initialValue, driftValue, initialValue}
		crbObj.Data[string(configsync.AuthSSH)] = []byte(driftValue)
		return nil
	}
	validate := func(obj client.Object) error {
		crbObj := obj.(*corev1.Secret)
		observedValue := string(crbObj.Data[string(configsync.AuthSSH)])
		// Record new value, if different from last known value
		if observedValue != observedValues[len(observedValues)-1] {
			t.Logf("observed roleRef.name change: %s", observedValue)
			observedValues = append(observedValues, observedValue)
		}
		if cmp.Equal(expectedValues, observedValues) {
			// success - change observed and reverted
			return nil
		}
		// keep watching
		return errors.Errorf("data[%q] changes expected %+v, but found %+v",
			configsync.AuthSSH, expectedValues, observedValues)
	}
	testRepoSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

func testRepoSyncDriftProtection(t *testing.T, exampleObj client.Object, objKeyFunc func(client.ObjectKey) client.ObjectKey, modify, validate func(client.Object) error) {
	t.Log("building RepoSyncReconciler")
	syncObj := repoSync(reposyncNs, reposyncName, reposyncRef(gitRevision), reposyncBranch(branch), reposyncSecretType(configsync.AuthSSH), reposyncSecretRef(reposyncSSHKey))
	secretObj := secretObj(t, reposyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(syncObj.Namespace))
	fakeClient, _, testReconciler := setupNSReconciler(t, secretObj)
	testDriftProtection(t, fakeClient, testReconciler, syncObj, exampleObj, objKeyFunc, modify, validate)
}
