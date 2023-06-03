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
	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, reconcilerKey, func(event watch.Event) error {
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

	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, reconcilerKey, func(event watch.Event) error {
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
	var oldObj *appsv1.Deployment
	var oldValue string
	modify := func(obj client.Object) error {
		oldObj = obj.(*appsv1.Deployment)
		oldValue = oldObj.Spec.Template.Spec.ServiceAccountName
		oldObj.Spec.Template.Spec.ServiceAccountName = "seanboswell"
		return nil
	}
	validate := func(obj client.Object) error {
		newObj := obj.(*appsv1.Deployment)
		newValue := newObj.Spec.Template.Spec.ServiceAccountName
		if newValue != oldValue {
			// keep watching
			return errors.Errorf("spec.template.spec.serviceAccountName expected to be %q, but found %q",
				oldValue, newValue)
		}
		newRV, err := parseResourceVersion(newObj)
		if err != nil {
			return err
		}
		// ResourceVersion should be updated on the oldObj by the client.Update AFTER the modify func was called.
		oldRV, err := parseResourceVersion(oldObj)
		if err != nil {
			return err
		}
		if newRV <= oldRV {
			return errors.Errorf("watch event with resourceVersion %d predates expected update with resourceVersion %d",
				newRV, oldRV)
		}
		// success - change reverted
		return nil
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
	var oldObj *corev1.ServiceAccount
	var oldValue string
	modify := func(obj client.Object) error {
		oldObj = obj.(*corev1.ServiceAccount)
		oldValue = oldObj.Labels[metadata.SyncKindLabel]
		oldObj.Labels[metadata.SyncKindLabel] = "seanboswell"
		return nil
	}
	validate := func(obj client.Object) error {
		newObj := obj.(*corev1.ServiceAccount)
		newValue := newObj.Labels[metadata.SyncKindLabel]
		if newValue != oldValue {
			// keep watching
			return errors.Errorf("spec.metadata.labels[%q] expected to be %q, but found %q",
				metadata.SyncKindLabel, oldValue, newValue)
		}
		newRV, err := parseResourceVersion(newObj)
		if err != nil {
			return err
		}
		// ResourceVersion should be updated on the oldObj by the client.Update AFTER the modify func was called.
		oldRV, err := parseResourceVersion(oldObj)
		if err != nil {
			return err
		}
		if newRV <= oldRV {
			return errors.Errorf("watch event with resourceVersion %d predates expected update with resourceVersion %d",
				newRV, oldRV)
		}
		// success - change reverted
		return nil
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
	var oldObj *rbacv1.RoleBinding
	var oldValue string
	modify := func(obj client.Object) error {
		oldObj = obj.(*rbacv1.RoleBinding)
		oldValue = oldObj.RoleRef.Name
		oldObj.RoleRef.Name = "seanboswell"
		return nil
	}
	validate := func(obj client.Object) error {
		newObj := obj.(*rbacv1.RoleBinding)
		newValue := newObj.RoleRef.Name
		if newValue != oldValue {
			// keep watching
			return errors.Errorf("roleRef.name expected to be %q, but found %q",
				oldValue, newValue)
		}
		newRV, err := parseResourceVersion(newObj)
		if err != nil {
			return err
		}
		// ResourceVersion should be updated on the oldObj by the client.Update AFTER the modify func was called.
		oldRV, err := parseResourceVersion(oldObj)
		if err != nil {
			return err
		}
		if newRV <= oldRV {
			return errors.Errorf("watch event with resourceVersion %d predates expected update with resourceVersion %d",
				newRV, oldRV)
		}
		// success - change reverted
		return nil
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
	var oldObj *corev1.Secret
	var oldValue string
	modify := func(obj client.Object) error {
		oldObj = obj.(*corev1.Secret)
		oldValue = string(oldObj.Data[string(configsync.AuthSSH)])
		oldObj.Data[string(configsync.AuthSSH)] = []byte("seanboswell")
		return nil
	}
	validate := func(obj client.Object) error {
		newObj := obj.(*corev1.Secret)
		newValue := string(newObj.Data[string(configsync.AuthSSH)])
		if newValue != oldValue {
			// keep watching
			return errors.Errorf("data[%q] expected to be %q, but found %q",
				configsync.AuthSSH, oldValue, newValue)
		}
		newRV, err := parseResourceVersion(newObj)
		if err != nil {
			return err
		}
		// ResourceVersion should be updated on the oldObj by the client.Update AFTER the modify func was called.
		oldRV, err := parseResourceVersion(oldObj)
		if err != nil {
			return err
		}
		if newRV <= oldRV {
			return errors.Errorf("watch event with resourceVersion %d predates expected update with resourceVersion %d",
				newRV, oldRV)
		}
		// success - change reverted
		return nil
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
