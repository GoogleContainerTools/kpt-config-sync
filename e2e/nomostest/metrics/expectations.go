// Copyright 2023 Google LLC
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

package metrics

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SyncSetExpectations tracks metrics expectations for a set of RootSyncs and
// RepoSyncs.
type SyncSetExpectations struct {
	expectations map[SyncKind]SyncExpectations

	// t is the test environment for the test.
	// Used to exit tests early when setup fails, and for logging.
	t testing.NTB

	// scheme is the Scheme for the test suite that maps from structs to GVKs.
	scheme *runtime.Scheme
}

// SyncKind is an enum representing the Kind of Sync resources.
type SyncKind string

const (
	// RootSyncKind is the kind of the RootSync resource
	RootSyncKind = SyncKind(configsync.RootSyncKind)

	// RepoSyncKind is the kind of the RepoSync resource
	RepoSyncKind = SyncKind(configsync.RepoSyncKind)
)

// SyncExpectations is a map of sync name & namespace to ObjectExpectations.
type SyncExpectations map[types.NamespacedName]ObjectSetExpectations

// ObjectSetExpectations is a map of object IDs to expected operations.
type ObjectSetExpectations map[core.ID]Operation

// NewSyncSetExpectations constructs a new map of operations expected to be
// performed by a set of syncs (specifically, the reconciler appliers, not the
// remediators).
func NewSyncSetExpectations(t testing.NTB, scheme *runtime.Scheme) *SyncSetExpectations {
	return &SyncSetExpectations{
		expectations: make(map[SyncKind]SyncExpectations),
		t:            t,
		scheme:       scheme,
	}
}

// Reset the expected operations
func (ae *SyncSetExpectations) Reset() {
	ae.expectations = make(map[SyncKind]SyncExpectations)
}

// ResetRootSync resets the expected operations for the specified RootSync
func (ae *SyncSetExpectations) ResetRootSync(syncName string) {
	if ae.expectations != nil && ae.expectations[RootSyncKind] != nil {
		ae.expectations[RootSyncKind][rootSyncNN(syncName)] = make(ObjectSetExpectations)
	}
}

// ResetRepoSync resets the expected operations for the specified RepoSync
func (ae *SyncSetExpectations) ResetRepoSync(syncNN types.NamespacedName) {
	if ae.expectations != nil && ae.expectations[RepoSyncKind] != nil {
		ae.expectations[RepoSyncKind][syncNN] = make(ObjectSetExpectations)
	}
}

// AddObjectApply specifies that an object is expected to be declared in the
// source of truth for the specified RSync.
func (ae *SyncSetExpectations) AddObjectApply(syncKind SyncKind, syncNN types.NamespacedName, obj client.Object) {
	ae.AddObjectOperation(syncKind, syncNN, obj, UpdateOperation)
}

// AddObjectDelete specifies that an object is expected to be deleted
// and removed from the source of truth for the specified RSync.
func (ae *SyncSetExpectations) AddObjectDelete(syncKind SyncKind, syncNN types.NamespacedName, obj client.Object) {
	ae.AddObjectOperation(syncKind, syncNN, obj, DeleteOperation)
}

// AddObjectOperation specifies that an object is expected to be updated or
// deleted from the source of truth for the specified RSync.
func (ae *SyncSetExpectations) AddObjectOperation(syncKind SyncKind, syncNN types.NamespacedName, obj client.Object, operation Operation) {
	if ae.expectations[syncKind] == nil {
		ae.expectations[syncKind] = make(SyncExpectations)
	}
	if ae.expectations[syncKind][syncNN] == nil {
		ae.expectations[syncKind][syncNN] = make(ObjectSetExpectations)
	}
	if !IsObjectApplyable(obj) {
		operation = SkipOperation
	}
	id := core.IDOf(obj)
	if id.GroupKind.Empty() {
		gvk, err := kinds.Lookup(obj, ae.scheme)
		if err != nil {
			ae.t.Fatal(err)
		}
		id.GroupKind = gvk.GroupKind()
	}
	ae.expectations[syncKind][syncNN][id] = operation
}

// RemoveObject removes an object from the set of objects expected to be
// declared in the source of truth for the specified RSync.
func (ae *SyncSetExpectations) RemoveObject(syncKind SyncKind, syncNN types.NamespacedName, obj client.Object) {
	if ae.expectations[syncKind] == nil {
		return
	}
	if ae.expectations[syncKind][syncNN] == nil {
		return
	}
	id := core.IDOf(obj)
	if id.GroupKind.Empty() {
		gvk, err := kinds.Lookup(obj, ae.scheme)
		if err != nil {
			ae.t.Fatal(err)
		}
		id.GroupKind = gvk.GroupKind()
	}
	delete(ae.expectations[syncKind][syncNN], id)
}

// ExpectedObjectCount returns the expected number of declared resource objects
// for the specified sync.
func (ae *SyncSetExpectations) ExpectedObjectCount(syncKind SyncKind, syncNN types.NamespacedName) int {
	if ae.expectations[syncKind] == nil {
		return 0
	}
	if ae.expectations[syncKind][syncNN] == nil {
		return 0
	}
	var declared int
	for _, operation := range ae.expectations[syncKind][syncNN] {
		switch operation {
		case UpdateOperation:
			declared++
		case SkipOperation:
			// Skipped objects count towards declared resources, even though not applied
			declared++
		case DeleteOperation:
			// Deleted objects do not count towards declared resources
		}
	}
	return declared
}

// ExpectedRootSyncObjectCount returns the expected number of declared resource
// objects in the specified RootSync.
func (ae *SyncSetExpectations) ExpectedRootSyncObjectCount(syncName string) int {
	return ae.ExpectedObjectCount(RootSyncKind, rootSyncNN(syncName))
}

// ExpectedRepoSyncObjectCount returns the expected number of declared resource
// objects in the specified RepoSync.
func (ae *SyncSetExpectations) ExpectedRepoSyncObjectCount(syncNN types.NamespacedName) int {
	return ae.ExpectedObjectCount(RepoSyncKind, syncNN)
}

// ExpectedObjectOperations returns the expected operations to be performed by
// the reconciler for the specified sync.
func (ae *SyncSetExpectations) ExpectedObjectOperations(syncKind SyncKind, syncNN types.NamespacedName) []ObjectOperation {
	if ae.expectations[syncKind] == nil {
		return nil
	}
	if ae.expectations[syncKind][syncNN] == nil {
		return nil
	}
	var ops []ObjectOperation
	for _, operation := range ae.expectations[syncKind][syncNN] {
		if operation == SkipOperation {
			continue
		}
		ops = AppendOperations(ops, ObjectOperation{
			Operation: operation,
			Count:     1,
		})
	}
	return ops
}

// ExpectedRootSyncObjectOperations returns the expected operations to be
// performed by the reconciler for the specified RootSync.
func (ae *SyncSetExpectations) ExpectedRootSyncObjectOperations(syncName string) []ObjectOperation {
	return ae.ExpectedObjectOperations(RootSyncKind, rootSyncNN(syncName))
}

// ExpectedRepoSyncObjectOperations returns the expected operations to be
// performed by the reconciler for the specified RepoSync.
func (ae *SyncSetExpectations) ExpectedRepoSyncObjectOperations(syncNN types.NamespacedName) []ObjectOperation {
	return ae.ExpectedObjectOperations(RepoSyncKind, syncNN)
}

// IsObjectDeclarable returns true if the object should be included in the
// declared resources passed to the applier.
func IsObjectDeclarable(_ client.Object) bool {
	// TODO: Update if any of the following are added using WithInitialCommit:
	// - ClusterSelectors & Cluster definitions
	// - Objects excluded by the current ClusterSelectors and Cluster
	// - Objects of unknown scope
	// - Objects outside of hierarchical directories when in hierarchical mode
	// - Objects in unmanaged namespaces
	// For now, WithInitialCommit is only used in a few places, and all of the
	// objects specified are intended to be applied. So just return true.
	return true
}

// IsObjectApplyable returns true if the object will be applied by Config Sync
// when included in the source.
func IsObjectApplyable(obj client.Object) bool {
	switch {
	case obj.GetAnnotations()[metadata.ResourceManagementKey] == metadata.ResourceManagementDisabled:
		// Disabled objects count towards declared objects, but are not applied
		return false
	case obj.GetAnnotations()[metadata.LocalConfigAnnotationKey] == "true":
		// Local objects count towards declared objects, but are not applied
		return false
	default:
		return true
	}
}

// String returns the operations in json format, for logging.
func (ae *SyncSetExpectations) String() string {
	// json.Marshal doesn't support complex key types.
	// So we have to stringify manually, with sorted keys.
	// Don't bother escaping anything. Nothing should contain quotes.
	var b strings.Builder
	b.WriteRune('{')
	syncKindStrs := make([]string, 0, len(ae.expectations))
	for syncKind := range ae.expectations {
		syncKindStrs = append(syncKindStrs, string(syncKind))
	}
	sort.Strings(syncKindStrs)
	for i, syncKindStr := range syncKindStrs {
		syncKind := SyncKind(syncKindStr)
		if i > 0 {
			b.WriteRune(',')
		}
		b.WriteString(fmt.Sprintf(`%q:{`, syncKind))
		syncNNs := make([]types.NamespacedName, 0, len(ae.expectations[syncKind]))
		for syncNN := range ae.expectations[syncKind] {
			syncNNs = append(syncNNs, syncNN)
		}
		NamespacedNameSlice(syncNNs).Sort()
		for j, syncNN := range syncNNs {
			if j > 0 {
				b.WriteRune(',')
			}
			b.WriteString(fmt.Sprintf(`%q:{`, syncNN))
			ids := make([]core.ID, 0, len(ae.expectations[syncKind][syncNN]))
			for id := range ae.expectations[syncKind][syncNN] {
				ids = append(ids, id)
			}
			ObjectIDSlice(ids).Sort()
			for k, id := range ids {
				if k > 0 {
					b.WriteRune(',')
				}
				b.WriteString(fmt.Sprintf(`%q:%q`,
					idToString(id),
					string(ae.expectations[syncKind][syncNN][id])))
			}
			b.WriteRune('}')
		}
		b.WriteRune('}')
	}
	b.WriteRune('}')
	return b.String()
}

// idToString serializes an object ID to a string.
// Use depends-on-style formatting, for readability and reversibility.
func idToString(id core.ID) string {
	if id.Namespace != "" {
		return fmt.Sprintf("%s/namespaces/%s/%s/%s", id.Group, id.Namespace, id.Kind, id.Name)
	}
	return fmt.Sprintf("%s/%s/%s", id.Group, id.Kind, id.Name)
}

// NamespacedNameSlice attaches the methods of sort.Interface to
// []types.NamespacedName, sorting in increasing order, namespace > name.
type NamespacedNameSlice []types.NamespacedName

// Len returns the size of the slice
func (x NamespacedNameSlice) Len() int { return len(x) }

// Less returns true if the value at index i is less than the value at index j.
func (x NamespacedNameSlice) Less(i, j int) bool {
	if x[i].Namespace == x[j].Namespace {
		return x[i].Name < x[j].Name
	}
	return x[i].Namespace < x[j].Namespace
}

// Sort is a convenience method: x.Sort() calls sort.Sort(x).
func (x NamespacedNameSlice) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

// Sort is a convenience method: x.Sort() calls sort.Sort(x).
func (x NamespacedNameSlice) Sort() { sort.Sort(x) }

// ObjectIDSlice attaches the methods of sort.Interface to
// []core.ID, sorting in increasing order, group > kind > namespace > name.
type ObjectIDSlice []core.ID

// Len returns the size of the slice
func (x ObjectIDSlice) Len() int { return len(x) }

// Less returns true if the value at index i is less than the value at index j.
func (x ObjectIDSlice) Less(i, j int) bool {
	if x[i].Group == x[j].Group {
		if x[i].Kind == x[j].Kind {
			if x[i].Namespace == x[j].Namespace {
				return x[i].Name < x[j].Name
			}
			return x[i].Namespace < x[j].Namespace
		}
		return x[i].Kind < x[j].Kind
	}
	return x[i].Group < x[j].Group
}

// Swap the values at index i and j.
func (x ObjectIDSlice) Swap(i, j int) { x[i], x[j] = x[j], x[i] }

// Sort is a convenience method: x.Sort() calls sort.Sort(x).
func (x ObjectIDSlice) Sort() { sort.Sort(x) }

// rootSyncNN returns the NamespacedName of the RootSync object.
func rootSyncNN(name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: configsync.ControllerNamespace,
		Name:      name,
	}
}
