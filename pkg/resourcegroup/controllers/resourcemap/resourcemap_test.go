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

package resourcemap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
)

func TestResourceMapReconcile(t *testing.T) {
	res1 := resource{
		Namespace: "ns1",
		Name:      "res1",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	res2 := resource{
		Namespace: "ns1",
		Name:      "res2",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	res3 := resource{
		Namespace: "ns1",
		Name:      "res3",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	gk := res1.GK()

	resourceMap := NewResourceMap()
	assert.True(t, resourceMap.IsEmpty())

	resgroup1 := types.NamespacedName{
		Namespace: "test-ns",
		Name:      "group1",
	}

	resgroup2 := types.NamespacedName{
		Namespace: "test-ns",
		Name:      "group2",
	}

	// TODO: replace with `ctx := t.Context()` in Go 1.24.0+
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var gks []schema.GroupKind

	gks = resourceMap.Reconcile(ctx, resgroup1, []resource{res1, res2}, false)
	assert.Equal(t, []schema.GroupKind{gk}, gks)
	assert.Len(t, resourceMap.gkToResources, 1)
	assert.Equal(t, 2, resourceMap.gkToResources[gk].Len())
	assert.True(t, resourceMap.gkToResources[gk].Has(res1))
	assert.True(t, resourceMap.gkToResources[gk].Has(res2))

	assert.False(t, resourceMap.IsEmpty())
	assert.True(t, resourceMap.HasResource(res1))
	assert.False(t, resourceMap.HasResource(res3))
	assert.True(t, resourceMap.HasResgroup(resgroup1))
	assert.False(t, resourceMap.HasResgroup(resgroup2))
	assert.Len(t, resourceMap.resgroupToResources, 1)
	assert.Len(t, resourceMap.resToResgroups, 2)
	assert.Equal(t, 1, resourceMap.resToResgroups[res1].Len())
	assert.Equal(t, 1, resourceMap.resToResgroups[res2].Len())
	assert.Equal(t, 2, resourceMap.resgroupToResources[resgroup1].Len())
	assert.Empty(t, resourceMap.resToStatus)

	resourceMap.SetStatus(res1, &CachedStatus{Status: v1alpha1.Current})
	resourceMap.SetStatus(res2, &CachedStatus{Status: v1alpha1.InProgress})

	gks = resourceMap.Reconcile(ctx, resgroup2, []resource{res1, res3}, false)
	assert.Len(t, gks, 1)
	assert.Len(t, resourceMap.gkToResources, 1)
	assert.Equal(t, 3, resourceMap.gkToResources[gk].Len())
	assert.True(t, resourceMap.gkToResources[gk].Has(res3))
	assert.Len(t, resourceMap.resToStatus, 2)
	cachedStatus := resourceMap.GetStatus(res1)
	assert.Equal(t, v1alpha1.Current, cachedStatus.Status)
	cachedStatus = resourceMap.GetStatus(res2)
	assert.Equal(t, v1alpha1.InProgress, cachedStatus.Status)
	cachedStatus = resourceMap.GetStatus(res3)
	assert.Nil(t, cachedStatus)

	assert.True(t, resourceMap.HasResource(res3))
	assert.True(t, resourceMap.HasResgroup(resgroup2))
	assert.Len(t, resourceMap.resgroupToResources, 2)
	assert.Len(t, resourceMap.resToResgroups, 3)
	assert.Equal(t, 2, resourceMap.resToResgroups[res1].Len())
	assert.Equal(t, 1, resourceMap.resToResgroups[res2].Len())
	assert.Equal(t, 1, resourceMap.resToResgroups[res3].Len())

	gks = resourceMap.Reconcile(ctx, resgroup1, []resource{res2}, false)
	assert.Len(t, gks, 1)
	assert.Len(t, resourceMap.gkToResources, 1)
	assert.Equal(t, 3, resourceMap.gkToResources[gk].Len())

	// res1 is still included in resgroup2
	assert.True(t, resourceMap.HasResource(res1))
	assert.Len(t, resourceMap.resToResgroups, 3)
	assert.Equal(t, 1, resourceMap.resToResgroups[res1].Len())

	// Set the resource set of resgroup1 to be empty
	gks = resourceMap.Reconcile(ctx, resgroup1, []resource{}, false)
	assert.Len(t, gks, 1)
	assert.Len(t, resourceMap.gkToResources, 1)
	assert.Equal(t, 2, resourceMap.gkToResources[gk].Len())

	assert.True(t, resourceMap.HasResgroup(resgroup1))
	assert.True(t, resourceMap.HasResgroup(resgroup2))
	assert.False(t, resourceMap.HasResource(res2))
	assert.Len(t, resourceMap.resgroupToResources, 2)
	assert.Len(t, resourceMap.resToResgroups, 2)
	assert.Len(t, resourceMap.resToStatus, 1)

	// Set the resource set of resgroup2 to be empty
	gks = resourceMap.Reconcile(ctx, resgroup2, []resource{}, false)
	assert.Empty(t, gks)
	assert.Empty(t, resourceMap.gkToResources)
	assert.Len(t, resourceMap.resgroupToResources, 2)
	// delete resgroup1
	gks = resourceMap.Reconcile(ctx, resgroup1, []resource{}, true)
	assert.Empty(t, gks)

	// delete resgroup2
	gks = resourceMap.Reconcile(ctx, resgroup2, []resource{}, true)
	assert.Empty(t, gks)
	assert.True(t, resourceMap.IsEmpty())
	assert.Empty(t, resourceMap.resToStatus)
}

func TestDiffResources(t *testing.T) {
	res1 := resource{
		Namespace: "ns1",
		Name:      "res1",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	res2 := resource{
		Namespace: "ns1",
		Name:      "res2",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	res3 := resource{
		Namespace: "ns1",
		Name:      "res3",
		GroupKind: v1alpha1.GroupKind{
			Group: "group1",
			Kind:  "service",
		},
	}

	toAdd, toDelete := diffResources([]resource{}, []resource{})
	assert.Empty(t, toAdd)
	assert.Empty(t, toDelete)

	toAdd, toDelete = diffResources([]resource{}, []resource{res1})
	assert.Len(t, toAdd, 1)
	assert.Empty(t, toDelete)
	assert.Equal(t, res1, toAdd[0])

	toAdd, toDelete = diffResources([]resource{res1, res2}, []resource{res1})
	assert.Empty(t, toAdd)
	assert.Len(t, toDelete, 1)
	assert.Equal(t, res2, toDelete[0])

	toAdd, toDelete = diffResources([]resource{res1, res2}, []resource{res1, res3})
	assert.Len(t, toAdd, 1)
	assert.Len(t, toDelete, 1)
	assert.Equal(t, res3, toAdd[0])
	assert.Equal(t, res2, toDelete[0])
}
