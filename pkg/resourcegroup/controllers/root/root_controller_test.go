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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/typeresolver"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

const contextRootControllerKey = contextKey("root-controller")

var c client.Client
var ctx context.Context

func TestRootReconciler(t *testing.T) {
	var reconcilerKpt *Reconciler
	var namespace = metav1.NamespaceDefault

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	assert.NoError(t, err)
	c = mgr.GetClient()

	reconcilerKpt, err = NewReconciler(mgr)
	assert.NoError(t, err)

	logger := reconcilerKpt.log.WithValues("Controller", "Root")
	ctx = context.WithValue(context.TODO(), contextRootControllerKey, logger)
	resolver, err := typeresolver.NewTypeResolver(mgr, logger)
	assert.NoError(t, err)
	reconcilerKpt.resolver = resolver

	StartTestManager(t, mgr)
	time.Sleep(10 * time.Second)

	reconcilerKpt.resMap = resourcemap.NewResourceMap()
	reconcilerKpt.channel = make(chan event.GenericEvent)

	resources := []v1alpha1.ObjMetadata{}

	resourceGroupKpt := &v1alpha1.ResourceGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "group0",
			Namespace: namespace,
		},
		Spec: v1alpha1.ResourceGroupSpec{
			Resources: resources,
		},
	}

	err = c.Create(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	assert.NoError(t, err)

	// Create triggers an reconciliation,
	// wait until the reconciliation ends.
	time.Sleep(time.Second)
	assert.Equal(t, 0, reconcilerKpt.watches.Len())
	assert.NotNil(t, reconcilerKpt.resMap)

	// The resmap should be updated correctly
	request := ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: namespace, Name: "group0"},
	}
	assert.True(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))

	// There should be one event pushed to the channel.
	var e event.GenericEvent
	go func() {
		e = <-reconcilerKpt.channel
	}()
	time.Sleep(time.Second)
	assert.Equal(t, "group0", e.Object.GetName())

	// update the Resourcegroup
	resources = []v1alpha1.ObjMetadata{
		{
			Name:      "statefulset",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "StatefulSet",
			},
		},
		{
			Name:      "deployment",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		{
			Name:      "deployment-2",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "Deployment",
			},
		},
		{
			Name:      "daemonset",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "apps",
				Kind:  "DaemonSet",
			},
		},
	}

	resourceGroupKpt.Spec = v1alpha1.ResourceGroupSpec{
		Resources: resources,
	}
	// Update the resource group
	err = c.Update(ctx, resourceGroupKpt, client.FieldOwner(fake.FieldManager))
	assert.NoError(t, err)

	// The update triggers another reconcile
	// wait until it ends.
	time.Sleep(time.Second)
	assert.Equal(t, 3, reconcilerKpt.watches.Len())
	assert.NotNil(t, reconcilerKpt.resMap)

	// The resmap should be updated correctly
	assert.True(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))
	for _, resource := range resources {
		assert.True(t, reconcilerKpt.resMap.HasResource(resource))
		assert.Equal(t, []types.NamespacedName{request.NamespacedName}, reconcilerKpt.resMap.Get(resource))
	}

	// The watchmap should be updated correctly
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

	// There should be one event pushed to the channel.
	go func() { e = <-reconcilerKpt.channel }()
	time.Sleep(time.Second)
	assert.Equal(t, "group0", e.Object.GetName())

	// Delete the resource group
	err = c.Delete(ctx, resourceGroupKpt)
	assert.NoError(t, err)

	// The delete triggers another reconcile
	// wait until it ends.
	time.Sleep(2 * time.Second)
	assert.NotNil(t, reconcilerKpt.resMap)

	// The resmap should be updated correctly
	// It doesn't contain any resourcegroup or resource
	assert.False(t, reconcilerKpt.resMap.HasResgroup(request.NamespacedName))
	assert.NotNil(t, reconcilerKpt.resMap)
	for _, resource := range resources {
		assert.False(t, reconcilerKpt.resMap.HasResource(resource))
	}

	// There should be one event pushed to the channel.
	go func() { e = <-reconcilerKpt.channel }()
	time.Sleep(time.Second)
	assert.Equal(t, "group0", e.Object.GetName())
}
