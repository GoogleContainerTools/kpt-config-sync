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
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/metrics"
)

type resource = v1alpha1.ObjMetadata

// resourceSet includes a set of resources
type resourceSet struct {
	// Define this as a set to make it efficient to check whether a given resource is included
	data map[resource]struct{}
}

// Add adds res into resourceSet
func (s *resourceSet) Add(res resource) {
	s.data[res] = struct{}{}
}

// Remove removes res from resourceSet
func (s *resourceSet) Remove(res resource) {
	delete(s.data, res)
}

// Has checks whether res is in resourceSet
func (s *resourceSet) Has(res resource) bool {
	_, ok := s.data[res]
	return ok
}

// Len returns the length of resourceSet
func (s *resourceSet) Len() int {
	return len(s.data)
}

// ToSlice converts a resourceSet into a slice
func (s *resourceSet) toSlice() []resource {
	result := []resource{}
	for k := range s.data {
		result = append(result, k)
	}
	return result
}

// newresourceSet initializes a resourceSet with a list of resources
func newresourceSet(resources []resource) *resourceSet {
	result := &resourceSet{data: make(map[resource]struct{})}
	for _, res := range resources {
		result.Add(res)
	}
	return result
}

// resourceGroupSet includes a set of resource group names
type resourceGroupSet struct {
	// Define this as a set to make it efficient to check whether a given resource group is included
	data map[types.NamespacedName]struct{}
}

// Add adds a group into the resourceGroupSet
func (s *resourceGroupSet) Add(group types.NamespacedName) {
	s.data[group] = struct{}{}
}

// Remove removes a group from the resourceGroupSet
func (s *resourceGroupSet) Remove(group types.NamespacedName) {
	delete(s.data, group)
}

// Has checks whether a group is in the resourceGroupSet
func (s *resourceGroupSet) Has(group types.NamespacedName) bool {
	_, ok := s.data[group]
	return ok
}

// Len returns the length of the resourceGroupSet
func (s *resourceGroupSet) Len() int {
	return len(s.data)
}

func (s *resourceGroupSet) toSlice() []types.NamespacedName {
	result := []types.NamespacedName{}
	for group := range s.data {
		result = append(result, group)
	}
	return result
}

// newresourceGroupSet initializes a resourceGroupSet with a list of resource groups
func newresourceGroupSet(groups []types.NamespacedName) *resourceGroupSet {
	result := &resourceGroupSet{data: make(map[types.NamespacedName]struct{})}
	for _, group := range groups {
		result.Add(group)
	}
	return result
}

// CachedStatus stores the status and condition for one resource.
type CachedStatus struct {
	Status      v1alpha1.Status
	Conditions  []v1alpha1.Condition
	SourceHash  string
	InventoryID string
}

// ResourceMap maintains the following maps:
// 1) resToResgroups maps a resource to all the resource groups including it
// 2) resgroupToResources maps a resource group to its resource set
// 3) resToStatus maps a resource to its cached status
// 4) gkToResources maps a GroupKind to its resource set
// During the reconciliation of a RG in the root controller, the updates to these two maps should be atomic.
type ResourceMap struct {
	// use a lock to make sure that updating resToResgroups and resgroupToResources is atomic
	lock sync.RWMutex
	// resToResgroups maps a resource to all the resource groups including it.
	resToResgroups map[resource]*resourceGroupSet
	// resToStatus maps a resource to its reconciliation status and conditions.
	resToStatus map[resource]*CachedStatus
	// resgroupToResources maps a resource group to its resource set
	resgroupToResources map[types.NamespacedName]*resourceSet
	// gkToResources maps a GroupKind to its resource set
	gkToResources map[schema.GroupKind]*resourceSet
}

// Reconcile takes a resourcegroup name and all the resources belonging to it, and
// updates the resToResgroups map and resgroupToResources map atomically. The updates include:
// 1) calculate the diff between resgroupToResources[`group`] and `resources`;
// 2) update resToResgroups and gkToResources using the diff;
//   - for resources only in resgroupToResources[`group`], remove <resource, group> from resToResgroups and gkToResources;
//   - for resources only in `resources`, add <resource, group> into resToResgroups and gkToResources;
//   - for resources in both resgroupToResources[`group`] and `resources`, do nothing.
//
// 3) set resgroupToResources[group] to resources, or delete group from resgroupToResources if `resources` is empty.
//
// Returns:
//
//	a slice of GroupKinds that should be watched
//
// To set the resources managed by a RG to be empty, call ResourceMap.Reconcile(group, []resource{}, false).
//
// To delete a RG, call ResourceMap.Reconcile(group, []resource{}, true).
func (m *ResourceMap) Reconcile(ctx context.Context, group types.NamespacedName, resources []resource, deleteRG bool) []schema.GroupKind {
	if deleteRG {
		resources = []resource{}
	}
	// calculate the diff between resgroupToResources[`group`] and `resources`
	oldResources := []resource{}
	if v, ok := m.resgroupToResources[group]; ok {
		oldResources = v.toSlice()
	}
	toAdd, toDelete := diffResources(oldResources, resources)

	// use a lock to make sure that updating resToResgroups and resgroupToResources is atomic
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, res := range toDelete {
		if groups, ok := m.resToResgroups[res]; ok {
			groups.Remove(group)
			if groups.Len() == 0 {
				// delete res from m.resToResgroups if no resource group includes it any more
				delete(m.resToResgroups, res)
				delete(m.resToStatus, res)

				// delete res from m.gkToResources[gk] if no resource group includes it any more
				gk := res.GK()
				m.gkToResources[gk].Remove(res)

				if m.gkToResources[gk].Len() == 0 {
					// delete gk from m.gkToResources if there is no resource group includes a gk resource
					delete(m.gkToResources, gk)
				}
			}
		}
	}

	for _, res := range toAdd {
		if groups, ok := m.resToResgroups[res]; ok {
			groups.Add(group)
		} else {
			m.resToResgroups[res] = newresourceGroupSet([]types.NamespacedName{group})

			gk := res.GK()
			// add res to m.gkToResources[gk]
			if gkResources, ok := m.gkToResources[gk]; !ok {
				m.gkToResources[gk] = newresourceSet([]resource{res})
			} else {
				gkResources.Add(res)
			}
		}
	}

	gks := map[schema.GroupKind]bool{}
	for res := range m.resToResgroups {
		gk := res.GK()
		gks[gk] = true
	}

	if len(resources) == 0 && deleteRG {
		delete(m.resgroupToResources, group)
	} else {
		m.resgroupToResources[group] = newresourceSet(resources)
	}

	metrics.RecordResourceGroupTotal(ctx, int64(len(m.resgroupToResources)))
	var gkSlice []schema.GroupKind
	for gk := range gks {
		gkSlice = append(gkSlice, gk)
	}
	return gkSlice
}

// diffResources calculate the diff between two sets of resources.
func diffResources(oldResources, newResources []resource) (toAdd, toDelete []resource) {
	newSet := newresourceSet(newResources)
	for _, res := range oldResources {
		if !newSet.Has(res) {
			toDelete = append(toDelete, res)
		}
	}

	oldSet := newresourceSet(oldResources)
	for _, res := range newResources {
		if !oldSet.Has(res) {
			toAdd = append(toAdd, res)
		}
	}
	return toAdd, toDelete
}

// HasResource checks whether a resource is in the ResourceMap
func (m *ResourceMap) HasResource(res resource) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, ok := m.resToResgroups[res]
	return ok
}

// HasResgroup checks whether a resourcegroup is in the ResourceMap
func (m *ResourceMap) HasResgroup(group types.NamespacedName) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()
	_, ok := m.resgroupToResources[group]
	return ok
}

// Get returns the resourceGroupSet for res
func (m *ResourceMap) Get(res resource) []types.NamespacedName {
	m.lock.RLock()
	defer m.lock.RUnlock()
	if v, ok := m.resToResgroups[res]; ok {
		return v.toSlice()
	}
	return []types.NamespacedName{}
}

// GetStatus returns the cached status for the input resource.
func (m *ResourceMap) GetStatus(res resource) *CachedStatus {
	m.lock.RLock()
	defer m.lock.RUnlock()
	if v, ok := m.resToStatus[res]; ok {
		return v
	}
	return nil
}

// GetStatusMap returns the map from resources to their status.
func (m *ResourceMap) GetStatusMap() map[resource]*CachedStatus {
	return m.resToStatus
}

// SetStatus sets the status and conditions for a resource.
func (m *ResourceMap) SetStatus(res resource, resStatus *CachedStatus) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.resToStatus[res] = resStatus
}

// GetResources get the set of resources for the given group kind.
func (m *ResourceMap) GetResources(gk schema.GroupKind) []resource {
	m.lock.Lock()
	defer m.lock.Unlock()
	set := m.gkToResources[gk]
	if set == nil {
		return nil
	}
	resources := make([]resource, len(set.data))
	i := 0
	for r := range set.data {
		resources[i] = r
		i++
	}
	return resources
}

// IsEmpty checks whether the ResourceMap is empty
func (m *ResourceMap) IsEmpty() bool {
	return len(m.resgroupToResources) == 0 && len(m.resToResgroups) == 0
}

// NewResourceMap initializes an empty ReverseMap
func NewResourceMap() *ResourceMap {
	return &ResourceMap{
		resToResgroups:      make(map[resource]*resourceGroupSet),
		resToStatus:         make(map[resource]*CachedStatus),
		resgroupToResources: make(map[types.NamespacedName]*resourceSet),
		gkToResources:       make(map[schema.GroupKind]*resourceSet),
	}
}
