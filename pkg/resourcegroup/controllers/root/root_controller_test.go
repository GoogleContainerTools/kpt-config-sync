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

package root

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testcontroller"
	"sigs.k8s.io/cli-utils/pkg/common"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	rgName      = "group0"
	rgNamespace = metav1.NamespaceDefault
	inventoryID = rgNamespace + "_" + rgName
)

func TestRootReconciler(t *testing.T) {
	var reconcilerKpt *Reconciler

	// Configure controller-manager to log to the test logger
	testLogger := testcontroller.NewTestLogger(t)
	controllerruntime.SetLogger(testLogger)

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{
		// Disable metrics
		Metrics: metricsserver.Options{BindAddress: "0"},
		Logger:  testLogger.WithName("controller-manager"),
	})
	require.NoError(t, err)
	c := mgr.GetClient()

	// TODO: replace with `ctx := t.Context()` in Go 1.24.0+
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := testLogger.WithName("controllers")
	reconcilerKpt, err = NewReconciler(mgr, logger.WithName("root"))
	require.NoError(t, err)

	resolver, err := typeresolver.ForManager(mgr, logger.WithName("typeresolver"))
	require.NoError(t, err)
	reconcilerKpt.resolver = resolver

	// Start the manager
	stopTestManager := testcontroller.StartTestManager(t, mgr)
	// Block test cleanup until manager is fully stopped
	defer stopTestManager()

	reconcilerKpt.resMap = resourcemap.NewResourceMap()
	reconcilerKpt.channel = make(chan event.GenericEvent)

	resources := []v1alpha1.ObjMetadata{}

	resourceGroupKpt := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rgName,
			Namespace: rgNamespace,
			Labels: map[string]string{
				common.InventoryLabel: inventoryID,
			},
		},
		Spec: v1alpha1.ResourceGroupSpec{
			Resources: resources,
		},
	}

	t.Log("Updating ResourceGroup")
	err = c.Create(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	// Wait for the Root controller to send a channel event
	waitForEvent(t, reconcilerKpt.channel, 5*time.Second)

	t.Log("Updating ResourceGroup status") // simulating InventoryResourceGroup.Apply
	resourceGroupKpt.Status.ObservedGeneration = resourceGroupKpt.Generation
	err = c.Status().Update(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	// Wait for the Root controller to send a channel event
	waitForEvent(t, reconcilerKpt.channel, 5*time.Second)

	// Validate that the Root controller updated the resMap
	request := ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: rgNamespace, Name: rgName},
	}
	assert.NotNil(t, reconcilerKpt.resMap)
	assert.True(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))
	if t.Failed() {
		t.FailNow()
	}

	// Wait for the TypeResolver to update the watches
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Equal(t, 0, reconcilerKpt.watches.Len())
	}, 5*time.Second, time.Second)

	// update the Resourcegroup
	resources = []v1alpha1.ObjMetadata{
		{
			Name:      "statefulset",
			Namespace: rgNamespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "StatefulSet",
			},
		},
		{
			Name:      "deployment",
			Namespace: rgNamespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		{
			Name:      "deployment-2",
			Namespace: rgNamespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		{
			Name:      "daemonset",
			Namespace: rgNamespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "DaemonSet",
			},
		},
	}

	resourceGroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}
	t.Log("Updating ResourceGroup")
	err = c.Update(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	// Wait for the Root controller to send a channel event
	waitForEvent(t, reconcilerKpt.channel, 5*time.Second)

	t.Log("Updating ResourceGroup status") // simulating InventoryResourceGroup.Apply
	resourceGroupKpt.Status.ObservedGeneration = resourceGroupKpt.Generation
	err = c.Status().Update(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	require.NoError(t, err)

	// Wait for the Root controller to send a channel event
	waitForEvent(t, reconcilerKpt.channel, 5*time.Second)

	// Validate that the Root controller updated the resMap
	assert.NotNil(t, reconcilerKpt.resMap)
	assert.True(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))
	for _, resource := range resources {
		assert.True(t, reconcilerKpt.resMap.HasResource(resource))
		assert.Equal(t, []types.NamespacedName{request.NamespacedName}, reconcilerKpt.resMap.Get(resource))
	}
	if t.Failed() {
		t.FailNow()
	}

	// Wait for the TypeResolver to update the watches
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Equal(t, 3, reconcilerKpt.watches.Len())
		for _, r := range []*Reconciler{reconcilerKpt} {
			watched := r.watches.IsWatched(schema.GroupVersionKind{
				Group: "apps", Version: "v1", Kind: "Deployment"})
			assert.True(t, watched)
			watched = r.watches.IsWatched(schema.GroupVersionKind{
				Group: "apps", Version: "v1", Kind: "StatefulSet"})
			assert.True(t, watched)
			watched = r.watches.IsWatched(schema.GroupVersionKind{
				Group: "apps", Version: "v1", Kind: "DaemonSet"})
			assert.True(t, watched)
			assert.Equal(t, 3, r.watches.Len())
		}
	}, 5*time.Second, time.Second)

	t.Log("Deleting ResourceGroup")
	err = c.Delete(ctx, resourceGroupKpt)
	require.NoError(t, err)

	// Wait for the TypeResolver to update the watches
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.Equal(t, 0, reconcilerKpt.watches.Len())
	}, 5*time.Second, time.Second)

	// Validate that the Root controller didn't send a channel event
	select {
	case e := <-reconcilerKpt.channel:
		t.Fatalf("expected channel NOT to be sent an event, but got %+v", e)
	default: // success
	}

	// Validate that the Root controller updated the resMap
	assert.NotNil(t, reconcilerKpt.resMap)
	assert.False(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))
	for _, resource := range resources {
		assert.False(t, reconcilerKpt.resMap.HasResource(resource))
	}
}

func waitForEvent(t *testing.T, inCh <-chan event.GenericEvent, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case e := <-inCh:
		require.Equal(t, rgName, e.Object.GetName())
	case <-timer.C:
		t.Fatalf("expected channel to be sent an event: timed out after %v", timeout)
	}
}

func TestOwnedByConfigSyncPredicate(t *testing.T) {
	type testCase struct {
		name string
		got  bool
		want bool
	}

	ownedByConfigSync := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "configmanagement.gke.io",
			},
			Namespace: "foo",
			Name:      "bar",
		},
	}
	ownedByConfigSync.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("ResourceGroup"))

	ownedBySomeoneElse := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "someone-else",
			},
			Namespace: "foo",
			Name:      "bar",
		},
	}
	ownedBySomeoneElse.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("ResourceGroup"))

	notOwned := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar",
		},
	}
	notOwned.SetGroupVersionKind(v1alpha1.SchemeGroupVersion.WithKind("ResourceGroup"))

	notResourceGroup := &v1.CustomResourceDefinition{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}
	notResourceGroup.SetGroupVersionKind(v1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))

	create := func(o client.Object) bool {
		return OwnedByConfigSyncPredicate{}.Create(
			event.TypedCreateEvent[client.Object]{Object: o},
		)
	}
	update := func(o client.Object, n client.Object) bool {
		return OwnedByConfigSyncPredicate{}.Update(
			event.TypedUpdateEvent[client.Object]{ObjectOld: o, ObjectNew: n},
		)
	}

	// not allowed to redefine delete, so we use a different name
	yeet := func(o client.Object) bool {
		return OwnedByConfigSyncPredicate{}.Delete(
			event.TypedDeleteEvent[client.Object]{Object: o},
		)
	}

	generic := func(o client.Object) bool {
		return OwnedByConfigSyncPredicate{}.Generic(
			event.TypedGenericEvent[client.Object]{Object: o},
		)
	}

	testCases := []testCase{
		{name: "create owned by configsync", got: create(ownedByConfigSync), want: true},
		{name: "create owned by someone else", got: create(ownedBySomeoneElse), want: false},
		{name: "create not owned", got: create(notOwned), want: false},
		{name: "create non-resourcegroup owned by configsync", got: create(notResourceGroup), want: true},

		{name: "update both owned by configsync", got: update(ownedByConfigSync, ownedByConfigSync), want: true},
		{name: "update old owned by configsync, new owned by someone else", got: update(ownedByConfigSync, ownedBySomeoneElse), want: true},
		{name: "update old owned by someone else, new owned by configsync", got: update(ownedBySomeoneElse, ownedByConfigSync), want: true},
		{name: "update both owned by someone else", got: update(ownedBySomeoneElse, ownedBySomeoneElse), want: false},
		{name: "update old not owned, new owned by configsync", got: update(notOwned, ownedByConfigSync), want: true},
		{name: "update old owned by configsync, new not owned", got: update(ownedByConfigSync, notOwned), want: true},
		{name: "update both not owned", got: update(notOwned, notOwned), want: false},
		{name: "update old not owned, new owned by someone else", got: update(notOwned, ownedBySomeoneElse), want: false},
		{name: "update old owned by someone else, new not owned", got: update(ownedBySomeoneElse, notOwned), want: false},
		{name: "update non-resourcegroup owned by configsync", got: update(notResourceGroup, notResourceGroup), want: true},

		{name: "delete owned by configsync", got: yeet(ownedByConfigSync), want: true},
		{name: "delete owned by someone else", got: yeet(ownedBySomeoneElse), want: false},
		{name: "delete not owned", got: yeet(notOwned), want: false},
		{name: "delete non-resourcegroup owned by configsync", got: yeet(notResourceGroup), want: true},

		{name: "generic owned by configsync", got: generic(ownedByConfigSync), want: true},
		{name: "generic owned by someone else", got: generic(ownedBySomeoneElse), want: false},
		{name: "generic not owned", got: generic(notOwned), want: false},
		{name: "generic non-resourcegroup owned by configsync", got: generic(notResourceGroup), want: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if testCase.got != testCase.want {
				t.Errorf("%s = %v, want %v", testCase.name, testCase.got, testCase.want)
			}
		})
	}
}
