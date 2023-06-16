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
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/util/log"
	watchutil "kpt.dev/configsync/pkg/util/watch"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestRootSyncReconcilerDeploymentLifecycle validates that the
// RootSyncReconciler works with the ControllerManager.
// - Create a root-reconciler Deployment when a RootSync is created
// - Delete the root-reconciler Deployment when the RootSync is deleted
func TestRootSyncReconcilerDeploymentLifecycle(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	t.Log("building root-reconciler-controller")
	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	secretObj := secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace))

	fakeClient, _, testReconciler := setupRootReconciler(t, core.Scheme, secretObj)

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

	reconcilerKey := core.RootReconcilerObjectKey(rs.Name)

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

	t.Log("verifying the reconciler-manager finalizer is present")
	rsKey := client.ObjectKeyFromObject(rs)
	rs = &v1beta1.RootSync{}
	err = fakeClient.Get(ctx, rsKey, rs)
	require.NoError(t, err)
	require.True(t, controllerutil.ContainsFinalizer(rs, metadata.ReconcilerManagerFinalizer))

	t.Log("deleting sync object and watching for NotFound")
	err = watchutil.DeleteAndWait(ctx, fakeClient, rs, 10*time.Second)
	require.NoError(t, err)

	// All managed objects should have been deleted by the reconciler-manager finalizer.
	// Only the user Secret should remain.
	secretObj.SetUID("1")
	t.Log("verifying all managed objects were deleted")
	fakeClient.Check(t, secretObj)
}

// TestReconcileInvalidRootSyncLifecycle validates that the RootSyncReconciler
// handles the lifecycle of an invalid RootSync object.
// - Surface an error for an invalid RootSync object without generating any resources.
// - Delete the RootSync object.
func TestReconcileInvalidRootSyncLifecycle(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	t.Log("building RootSyncReconciler")
	// rs is an invalid RootSync as its auth type is set to `token`, but the token key is not configured in the secret.
	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeyToken), rootsyncSecretRef(rootsyncSSHKey))
	secretObj := secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace))

	fakeClient, _, testReconciler := setupRootReconciler(t, core.Scheme, secretObj)

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

	t.Log("watching for RootSync status update")
	watchCtx, watchCancel := context.WithTimeout(ctx, 10*time.Second)
	defer watchCancel()

	watcher, err := watchObjects(watchCtx, fakeClient, &v1beta1.RootSyncList{})
	require.NoError(t, err)

	t.Log("creating RootSync")
	err = fakeClient.Create(ctx, rs)
	require.NoError(t, err)

	var rsObj *v1beta1.RootSync
	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, core.ObjectNamespacedName(rs), func(event watch.Event) error {
		t.Logf("RootSync %s", event.Type)
		if event.Type == watch.Modified {
			rsObj = event.Object.(*v1beta1.RootSync)
			for _, cond := range rsObj.Status.Conditions {
				if cond.Reason == "Validation" && cond.Message == `git secretType was set as "token" but token key is not present in root-ssh-key secret` {
					return nil
				}
			}
			return fmt.Errorf("RootSync status not updated yet")
		}
		// keep watching
		return fmt.Errorf("RootSync object %s", event.Type)
	})
	require.NoError(t, err)
	if rsObj == nil {
		t.Fatal("timed out waiting for RootSync to become stalled")
	}

	t.Log("only the stalled RootSync and user Secret should be present, no other generated resources")
	secretObj.SetUID("1")
	fakeClient.Check(t, secretObj, rsObj)

	t.Log("deleting sync object and watching for NotFound")
	err = watchutil.DeleteAndWait(ctx, fakeClient, rs, 10*time.Second)
	require.NoError(t, err)
	t.Log("only the user Secret should be present")
	fakeClient.Check(t, secretObj)
}

// TestReconcileRootSyncLifecycleValidToInvalid validates that the RootSyncReconciler handles
// the lifecycle of an RootSync object changing from valid to invalid state.
// - Create a ns-reconciler Deployment when a valid RootSync is created
// - Surface an error when the RootSync object becomes invalid without deleting the generated resources
// - Delete the RootSync object and its generated dependencies.
func TestReconcileRootSyncLifecycleValidToInvalid1(t *testing.T) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	t.Log("building RootSyncReconciler")
	rs := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	secretObj := secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(rs.Namespace))

	fakeClient, _, testReconciler := setupRootReconciler(t, core.Scheme, secretObj)

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

	reconcilerKey := core.RootReconcilerObjectKey(rs.Name)

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

	t.Log("verifying the reconciler-manager finalizer is present")
	rsKey := client.ObjectKeyFromObject(rs)
	rs = &v1beta1.RootSync{}
	err = fakeClient.Get(ctx, rsKey, rs)
	require.NoError(t, err)
	require.True(t, controllerutil.ContainsFinalizer(rs, metadata.ReconcilerManagerFinalizer))

	t.Log("watching for RootSync status update")
	watcher, err = watchObjects(watchCtx, fakeClient, &v1beta1.RootSyncList{})
	require.NoError(t, err)

	t.Log("updating RootSync to make it invalid")
	existing := rs.DeepCopy()
	rs.Spec.Auth = configsync.AuthToken
	err = fakeClient.Patch(ctx, rs, client.MergeFrom(existing))
	require.NoError(t, err)

	var rsObj *v1beta1.RootSync
	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, core.ObjectNamespacedName(rs), func(event watch.Event) error {
		t.Logf("RootSync %s", event.Type)
		if event.Type == watch.Modified {
			rsObj = event.Object.(*v1beta1.RootSync)
			for _, cond := range rsObj.Status.Conditions {
				if cond.Reason == "Validation" && cond.Message == `git secretType was set as "token" but token key is not present in root-ssh-key secret` {
					return nil
				}
			}
			return fmt.Errorf("RootSync status not updated yet")
		}
		// keep watching
		return fmt.Errorf("RootSync object %s", event.Type)
	})
	require.NoError(t, err)
	if rsObj == nil {
		t.Fatal("timed out waiting for RootSync to become stalled")
	}

	t.Log("verifying the reconciler deployment object still exists")
	err = fakeClient.Get(ctx, reconcilerKey, &appsv1.Deployment{})
	require.NoError(t, err)

	t.Log("deleting sync object and watching for NotFound")
	err = watchutil.DeleteAndWait(ctx, fakeClient, rs, 10*time.Second)
	require.NoError(t, err)

	// All managed objects should have been deleted by the reconciler-manager finalizer.
	// Only the user Secret should remain.
	t.Log("verifying all managed objects were deleted")
	secretObj.SetUID("1")
	fakeClient.Check(t, secretObj)
}

// TestRootSyncReconcilerDeploymentDriftProtection validates that changes to
// specific managed fields of the reconciler deployment are reverted if changed
// by another client.
func TestRootSyncReconcilerDeploymentDriftProtection(t *testing.T) {
	exampleObj := &appsv1.Deployment{}
	objKeyFunc := func(rs client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed reconciler deployment
		return core.RootReconcilerObjectKey(rs.Name)
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
	testRootSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

// TestRootSyncReconcilerServiceAccountDriftProtection validates that changes to
// specific managed fields of the reconciler service account are reverted if
// changed by another client.
func TestRootSyncReconcilerServiceAccountDriftProtection(t *testing.T) {
	exampleObj := &corev1.ServiceAccount{}
	objKeyFunc := func(rs client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed service account
		return core.RootReconcilerObjectKey(rs.Name)
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
	testRootSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

// TestRootSyncReconcilerClusterRoleBindingDriftProtection validates that
// changes to specific managed fields of the reconciler cluster role binding are
// reverted if changed by another client.
func TestRootSyncReconcilerClusterRoleBindingDriftProtection(t *testing.T) {
	exampleObj := &rbacv1.ClusterRoleBinding{}
	objKeyFunc := func(_ client.ObjectKey) client.ObjectKey {
		// reconciler-manager managed cluster role binding
		return client.ObjectKey{Name: RootSyncPermissionsName()}
	}
	var oldObj *rbacv1.ClusterRoleBinding
	var oldValue string
	modify := func(obj client.Object) error {
		oldObj = obj.(*rbacv1.ClusterRoleBinding)
		oldValue = oldObj.RoleRef.Name
		oldObj.RoleRef.Name = "seanboswell"
		return nil
	}
	validate := func(obj client.Object) error {
		newObj := obj.(*rbacv1.ClusterRoleBinding)
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
	testRootSyncDriftProtection(t, exampleObj, objKeyFunc, modify, validate)
}

func testRootSyncDriftProtection(t *testing.T, exampleObj client.Object, objKeyFunc func(client.ObjectKey) client.ObjectKey, modify, validate func(client.Object) error) {
	t.Log("building RootSyncReconciler")
	syncObj := rootSyncWithGit(rootsyncName, rootsyncRef(gitRevision), rootsyncBranch(branch), rootsyncSecretType(GitSecretConfigKeySSH), rootsyncSecretRef(rootsyncSSHKey))
	secretObj := secretObj(t, rootsyncSSHKey, configsync.AuthSSH, v1beta1.GitSource, core.Namespace(syncObj.Namespace))
	fakeClient, _, testReconciler := setupRootReconciler(t, core.Scheme, secretObj)
	testDriftProtection(t, fakeClient, testReconciler, syncObj, exampleObj, objKeyFunc, modify, validate)
}

func testDriftProtection(t *testing.T, fakeClient *syncerFake.Client, testReconciler Controller, syncObj, exampleObj client.Object, objKeyFunc func(client.ObjectKey) client.ObjectKey, modify, validate func(client.Object) error) {
	// Mock out parseDeployment for testing.
	parseDeployment = parsedDeployment

	defer logObjectYAMLIfFailed(t, fakeClient, syncObj)

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

	key := objKeyFunc(client.ObjectKeyFromObject(syncObj))

	t.Logf("watching %s %s until created", kinds.ObjectSummary(exampleObj), key)
	watchCtx, watchCancel := context.WithTimeout(ctx, 10*time.Second)
	defer watchCancel()

	// Start watching
	gvk, err := kinds.Lookup(exampleObj, fakeClient.Scheme())
	require.NoError(t, err)
	exampleObjList, err := kinds.NewTypedListForItemGVK(gvk, fakeClient.Scheme())
	require.NoError(t, err)
	watcher, err := watchObjects(watchCtx, fakeClient, exampleObjList)
	require.NoError(t, err)

	// Create RootSync
	err = fakeClient.Create(ctx, syncObj)
	require.NoError(t, err)

	// Consume watch events until success or timeout
	var obj client.Object
	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, key, func(event watch.Event) error {
		t.Logf("reconciler %s %s", kinds.ObjectSummary(exampleObj), event.Type)
		if event.Type == watch.Added || event.Type == watch.Modified {
			obj = event.Object.(client.Object)
			// success! object was applied.
			return nil
		}
		// keep watching
		return errors.Errorf("reconciler %s %s", kinds.ObjectSummary(exampleObj), event.Type)
	})
	require.NoError(t, err)
	if obj == nil {
		t.Fatalf("timed out waiting for reconciler %s to be applied", kinds.ObjectSummary(exampleObj))
	}

	t.Logf("watching reconciler %s %s until drift revert",
		kinds.ObjectSummary(exampleObj), key)
	watchCtx2, watchCancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer watchCancel2()

	// Start watching
	watcher, err = watchObjects(watchCtx2, fakeClient, exampleObjList)
	require.NoError(t, err)

	// Update object to apply unwanted drift
	err = modify(obj)
	require.NoError(t, err)
	err = fakeClient.Update(ctx, obj)
	require.NoError(t, err)

	// Consume watch events until success or timeout
	err = watchObjectUntil(ctx, fakeClient.Scheme(), watcher, key, func(event watch.Event) error {
		t.Logf("reconciler %s %s", kinds.ObjectSummary(exampleObj), event.Type)
		if event.Type == watch.Added || event.Type == watch.Modified {
			return validate(event.Object.(client.Object))
		}
		// keep watching
		return errors.Errorf("reconciler %s %s", kinds.ObjectSummary(exampleObj), event.Type)
	})
	require.NoError(t, err)
}

func startControllerManager(ctx context.Context, t *testing.T, fakeClient *syncerFake.Client, testReconciler Controller) <-chan error {
	t.Helper()

	// start sub-context so we can cancel & stop the manager in case of pre-return error
	ctx, cancel := context.WithCancel(ctx)

	fakeCache := syncerFake.NewCache(fakeClient, syncerFake.CacheOptions{})

	t.Log("building controller-manager")
	mgr, err := controllerruntime.NewManager(&rest.Config{}, controllerruntime.Options{
		Scheme: core.Scheme,
		Logger: testr.New(t),
		BaseContext: func() context.Context {
			return ctx
		},
		NewCache: func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
			return fakeCache, nil
		},
		NewClient: func(_ cache.Cache, _ *rest.Config, _ client.Options, _ ...client.Object) (client.Client, error) {
			return fakeClient, nil
		},
		MapperProvider: func(_ *rest.Config) (meta.RESTMapper, error) {
			return fakeClient.RESTMapper(), nil
		},
	})
	require.NoError(t, err)

	err = mgr.SetFields(fakeClient) // Replace cluster.apiReader
	require.NoError(t, err)

	t.Log("registering root-reconciler-controller")
	err = testReconciler.SetupWithManager(mgr, false)
	require.NoError(t, err)

	errCh := make(chan error)

	// Start manager in the background
	go func() {
		t.Log("starting controller-manager")
		errCh <- mgr.Start(ctx)
		close(errCh)
		cancel()
	}()

	if !fakeCache.WaitForCacheSync(ctx) {
		// stop manager & drain error channel
		cancel()
		defer func() {
			//nolint:revive // empty-block is fine for draining a channel to unblock the producer
			for range errCh {
			}
		}()
		t.Fatal("Failed to sync informer cache")
	}

	return errCh
}

func logObjectYAMLIfFailed(t *testing.T, fakeClient *syncerFake.Client, obj client.Object) {
	if t.Failed() {
		err := fakeClient.Get(context.Background(), client.ObjectKeyFromObject(obj), obj)
		require.NoError(t, err)
		t.Logf("%s YAML:\n%s", kinds.ObjectSummary(obj),
			log.AsYAMLWithScheme(obj, fakeClient.Scheme()))
	}
}

func watchObjects(ctx context.Context, fakeClient *syncerFake.Client, exampleList client.ObjectList) (watch.Interface, error) {
	watcher, err := fakeClient.Watch(ctx, exampleList)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		watcher.Stop()
	}()
	return watcher, nil
}

func watchObjectUntil(ctx context.Context, scheme *runtime.Scheme, watcher watch.Interface, key client.ObjectKey, condition func(watch.Event) error) error {
	// Wait until added or modified
	var conditionErr error
	doneCh := ctx.Done()
	resultCh := watcher.ResultChan()
	var lastKnown client.Object
	for {
		select {
		case <-doneCh:
			return errors.Wrap(ctx.Err(), "context done before condition was met")
		case event, open := <-resultCh:
			if !open {
				if conditionErr != nil {
					return errors.Wrap(conditionErr, "watch stopped before condition was met")
				}
				return errors.New("watch stopped before any events were received")
			}
			if event.Type == watch.Error {
				statusErr := apierrors.FromObject(event.Object)
				return errors.Wrap(statusErr, "watch event error")
			}
			obj := event.Object.(client.Object)
			if key != client.ObjectKeyFromObject(obj) {
				// not the right object
				continue
			}
			klog.V(5).Infof("Watch Event %s Diff (- Removed, + Added):\n%s",
				kinds.ObjectSummary(obj),
				log.AsYAMLDiffWithScheme(lastKnown, obj, scheme))
			lastKnown = obj
			conditionErr = condition(event)
			if conditionErr == nil {
				// success - condition met
				return nil
			}
			// wait for next event - condition not met
		}
	}
}

func parseResourceVersion(obj client.Object) (int, error) {
	rv, err := strconv.Atoi(obj.GetResourceVersion())
	if err != nil {
		return -1, errors.Wrapf(err, "invalid ResourceVersion %q for object %s",
			obj.GetResourceVersion(), kinds.ObjectSummary(obj))
	}
	return rv, nil
}
