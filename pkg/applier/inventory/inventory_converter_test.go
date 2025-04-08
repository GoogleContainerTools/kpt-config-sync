// Copyright 2025 Google LLC
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

package inventory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

const (
	invName       = "inv-name"
	invNamespace  = configsync.ControllerNamespace
	invID         = "inv-id"
	invGeneration = int64(42)
)

// resourceGroupFactory is a convenience test helper for constructing a ResourceGroup
// object and casting it to unstructured for test usage
type resourceGroupFactory struct {
	spec   v1alpha1.ResourceGroupSpec
	status v1alpha1.ResourceGroupStatus
}

func (f resourceGroupFactory) build(t *testing.T) *unstructured.Unstructured {
	rg := &v1alpha1.ResourceGroup{}
	rg.Name = invName
	rg.Namespace = invNamespace
	rg.SetLabels(map[string]string{
		common.InventoryLabel: invID,
	})
	rg.SetGeneration(invGeneration)
	rg.Spec = f.spec
	rg.Status = f.status
	uObj, err := kinds.ToUnstructured(rg, core.Scheme)
	require.NoError(t, err)
	return uObj
}

func TestInventoryFromUnstructured(t *testing.T) {
	testCases := map[string]struct {
		fromObj         *unstructured.Unstructured
		wantErr         error
		wantObjRefs     object.ObjMetadataSet
		wantObjStatuses []actuation.ObjectStatus
	}{
		"nil object returns err": {
			fromObj: nil,
			wantErr: fmt.Errorf("unstructured ResourceGroup object is nil"),
		},
		"ResourceGroup that has spec but no status": {
			fromObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
			}.build(t),
			wantObjRefs: object.ObjMetadataSet{
				{
					Name:      "obj1-name",
					Namespace: "obj1-ns",
					GroupKind: schema.GroupKind{
						Group: "obj1-group",
						Kind:  "obj1-kind",
					},
				},
				{
					Name:      "obj2-name",
					Namespace: "obj2-ns",
					GroupKind: schema.GroupKind{
						Group: "obj2-group",
						Kind:  "obj2-kind",
					},
				},
			},
			wantObjStatuses: nil,
		},
		"ResourceGroup with spec and status": {
			fromObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:     v1alpha1.Current,
							SourceHash: "abc-123",
							Strategy:   v1alpha1.Apply,
							Actuation:  v1alpha1.ActuationSucceeded,
							Reconcile:  v1alpha1.ReconcileSucceeded,
						},
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj2-name",
								Namespace: "obj2-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj2-group",
									Kind:  "obj2-kind",
								},
							},
							Status:     v1alpha1.Current,
							SourceHash: "abc-123",
							Strategy:   v1alpha1.Apply,
							Actuation:  v1alpha1.ActuationSucceeded,
							Reconcile:  v1alpha1.ReconcileSucceeded,
						},
					},
				},
			}.build(t),
			wantObjRefs: object.ObjMetadataSet{
				{
					Name:      "obj1-name",
					Namespace: "obj1-ns",
					GroupKind: schema.GroupKind{
						Group: "obj1-group",
						Kind:  "obj1-kind",
					},
				},
				{
					Name:      "obj2-name",
					Namespace: "obj2-ns",
					GroupKind: schema.GroupKind{
						Group: "obj2-group",
						Kind:  "obj2-kind",
					},
				},
			},
			// Converter does not currently populate status to Inventory
			wantObjStatuses: nil,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ic := NewInventoryConverter(declared.RootScope, "test-sync", metadata.StatusEnabled)
			inv, err := ic.InventoryFromUnstructured(tc.fromObj)
			if tc.wantErr != nil {
				require.Equal(t, tc.wantErr, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, invName, inv.GetName())
			require.Equal(t, invNamespace, inv.GetNamespace())
			require.Equal(t, invID, inv.GetID().String())
			testutil.AssertEqual(t, tc.wantObjRefs, inv.GetObjectRefs())
			testutil.AssertEqual(t, tc.wantObjStatuses, inv.GetObjectStatuses())
		})
	}
}

// inventoryFactory is a convenience test helper for constructing a UnstructuredInventory
type inventoryFactory struct {
	objRefs     object.ObjMetadataSet
	objStatuses []actuation.ObjectStatus
}

func (f inventoryFactory) build(t *testing.T) *inventory.UnstructuredInventory {
	emptyRG := resourceGroupFactory{}.build(t)
	ui := inventory.NewUnstructuredInventory(emptyRG)
	ui.SetObjectRefs(f.objRefs)
	ui.SetObjectStatuses(f.objStatuses)
	return ui
}

func TestInventoryToUnstructured(t *testing.T) {

	testCases := map[string]struct {
		fromObj    *unstructured.Unstructured
		toInv      *inventory.UnstructuredInventory
		statusMode metadata.StatusMode
		wantErr    error
		wantObj    *unstructured.Unstructured
	}{
		"nil fromObj and toInv returns err": {
			statusMode: metadata.StatusEnabled,
			wantErr:    fmt.Errorf("unstructured object is nil"),
		},
		"nil fromObj returns err": {
			statusMode: metadata.StatusEnabled,
			toInv:      inventoryFactory{}.build(t),
			wantErr:    fmt.Errorf("unstructured object is nil"),
		},
		"nil toInv returns err": {
			statusMode: metadata.StatusEnabled,
			fromObj:    resourceGroupFactory{}.build(t),
			wantErr:    fmt.Errorf("UnstructuredInventory object is nil"),
		},
		"empty ResourceGroup object and inventory": {
			statusMode: metadata.StatusEnabled,
			fromObj:    resourceGroupFactory{}.build(t),
			toInv:      inventoryFactory{}.build(t),
			wantObj: resourceGroupFactory{
				status: v1alpha1.ResourceGroupStatus{ObservedGeneration: invGeneration},
			}.build(t),
		},
		"empty ResourceGroup object and non-empty inventory": {
			statusMode: metadata.StatusEnabled,
			fromObj:    resourceGroupFactory{}.build(t),
			toInv: inventoryFactory{
				objRefs: object.ObjMetadataSet{
					{
						Name:      "obj1-name",
						Namespace: "obj1-ns",
						GroupKind: schema.GroupKind{
							Group: "obj1-group",
							Kind:  "obj1-kind",
						},
					},
					{
						Name:      "obj2-name",
						Namespace: "obj2-ns",
						GroupKind: schema.GroupKind{
							Group: "obj2-group",
							Kind:  "obj2-kind",
						},
					},
				},
				objStatuses: []actuation.ObjectStatus{
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							Group:     "obj1-group",
							Kind:      "obj1-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationSucceeded,
						Reconcile:  actuation.ReconcileSucceeded,
						UID:        "obj1-uid",
						Generation: int64(21),
					},
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							Group:     "obj2-group",
							Kind:      "obj2-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationPending,
						Reconcile:  actuation.ReconcilePending,
						UID:        "obj2-uid",
						Generation: int64(12),
					},
				},
			}.build(t),
			wantObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationSucceeded,
							Reconcile: v1alpha1.ReconcileSucceeded,
						},
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj2-name",
								Namespace: "obj2-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj2-group",
									Kind:  "obj2-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationPending,
							Reconcile: v1alpha1.ReconcilePending,
						},
					},
				},
			}.build(t),
		},
		"non-empty ResourceGroup object and empty inventory": {
			statusMode: metadata.StatusEnabled,
			fromObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationSucceeded,
							Reconcile: v1alpha1.ReconcileSucceeded,
						},
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj2-name",
								Namespace: "obj2-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj2-group",
									Kind:  "obj2-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationPending,
							Reconcile: v1alpha1.ReconcilePending,
						},
					},
				},
			}.build(t),
			toInv: inventoryFactory{}.build(t),
			wantObj: resourceGroupFactory{
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
				},
			}.build(t),
		},
		"non-empty ResourceGroup object and non-empty inventory": {
			statusMode: metadata.StatusEnabled,
			fromObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationSucceeded,
							Reconcile: v1alpha1.ReconcileSucceeded,
						},
					},
				},
			}.build(t),
			toInv: inventoryFactory{
				objRefs: object.ObjMetadataSet{
					{
						Name:      "obj1-name",
						Namespace: "obj1-ns",
						GroupKind: schema.GroupKind{
							Group: "obj1-group",
							Kind:  "obj1-kind",
						},
					},
					{
						Name:      "obj2-name",
						Namespace: "obj2-ns",
						GroupKind: schema.GroupKind{
							Group: "obj2-group",
							Kind:  "obj2-kind",
						},
					},
				},
				objStatuses: []actuation.ObjectStatus{
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							Group:     "obj1-group",
							Kind:      "obj1-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationSucceeded,
						Reconcile:  actuation.ReconcileSucceeded,
						UID:        "obj1-uid",
						Generation: int64(21),
					},
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							Group:     "obj2-group",
							Kind:      "obj2-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationPending,
						Reconcile:  actuation.ReconcilePending,
						UID:        "obj2-uid",
						Generation: int64(12),
					},
				},
			}.build(t),
			wantObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationSucceeded,
							Reconcile: v1alpha1.ReconcileSucceeded,
						},
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj2-name",
								Namespace: "obj2-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj2-group",
									Kind:  "obj2-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationPending,
							Reconcile: v1alpha1.ReconcilePending,
						},
					},
				},
			}.build(t),
		},
		"omit status from ResourceGroup when StatusDisabled": {
			statusMode: metadata.StatusDisabled,
			fromObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
					ResourceStatuses: []v1alpha1.ResourceStatus{
						{
							ObjMetadata: v1alpha1.ObjMetadata{
								Name:      "obj1-name",
								Namespace: "obj1-ns",
								GroupKind: v1alpha1.GroupKind{
									Group: "obj1-group",
									Kind:  "obj1-kind",
								},
							},
							Status:    v1alpha1.Unknown,
							Strategy:  v1alpha1.Apply,
							Actuation: v1alpha1.ActuationSucceeded,
							Reconcile: v1alpha1.ReconcileSucceeded,
						},
					},
				},
			}.build(t),
			toInv: inventoryFactory{
				objRefs: object.ObjMetadataSet{
					{
						Name:      "obj1-name",
						Namespace: "obj1-ns",
						GroupKind: schema.GroupKind{
							Group: "obj1-group",
							Kind:  "obj1-kind",
						},
					},
					{
						Name:      "obj2-name",
						Namespace: "obj2-ns",
						GroupKind: schema.GroupKind{
							Group: "obj2-group",
							Kind:  "obj2-kind",
						},
					},
				},
				objStatuses: []actuation.ObjectStatus{
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							Group:     "obj1-group",
							Kind:      "obj1-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationSucceeded,
						Reconcile:  actuation.ReconcileSucceeded,
						UID:        "obj1-uid",
						Generation: int64(21),
					},
					{
						ObjectReference: actuation.ObjectReference{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							Group:     "obj2-group",
							Kind:      "obj2-kind",
						},
						Strategy:   actuation.ActuationStrategyApply,
						Actuation:  actuation.ActuationPending,
						Reconcile:  actuation.ReconcilePending,
						UID:        "obj2-uid",
						Generation: int64(12),
					},
				},
			}.build(t),
			wantObj: resourceGroupFactory{
				spec: v1alpha1.ResourceGroupSpec{
					Resources: []v1alpha1.ObjMetadata{
						{
							Name:      "obj1-name",
							Namespace: "obj1-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj1-group",
								Kind:  "obj1-kind",
							},
						},
						{
							Name:      "obj2-name",
							Namespace: "obj2-ns",
							GroupKind: v1alpha1.GroupKind{
								Group: "obj2-group",
								Kind:  "obj2-kind",
							},
						},
					},
				},
				status: v1alpha1.ResourceGroupStatus{
					ObservedGeneration: invGeneration,
					Conditions: []v1alpha1.Condition{
						{
							Type:   v1alpha1.Stalled,
							Status: v1alpha1.FalseConditionStatus,
						},
					},
				},
			}.build(t),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ic := NewInventoryConverter(declared.RootScope, invName, tc.statusMode)
			rg, err := ic.InventoryToUnstructured(tc.fromObj, tc.toInv)
			if tc.wantErr != nil {
				require.Equal(t, tc.wantErr, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, invName, rg.GetName())
			require.Equal(t, invNamespace, rg.GetNamespace())
			// Set expected annotations
			tc.wantObj.SetAnnotations(map[string]string{
				metadata.ManagementModeAnnotationKey: metadata.ManagementEnabled.String(),
				metadata.StatusModeAnnotationKey:     tc.statusMode.String(),
			})
			// Set expected labels
			tc.wantObj.SetLabels(map[string]string{
				common.InventoryLabel:       invID,
				metadata.ManagedByKey:       metadata.ManagedByValue,
				metadata.SyncNameLabel:      invName,
				metadata.SyncNamespaceLabel: configsync.ControllerNamespace,
				metadata.SyncKindLabel:      configsync.RootSyncKind,
			})
			testutil.AssertEqual(t, tc.wantObj, rg)
		})
	}
}

func TestNewInventoryConverter(t *testing.T) {
	testCases := map[string]struct {
		scope             declared.Scope
		wantSyncNamespace string
		wantSyncKind      string
	}{
		"RootSync": {
			scope:             declared.RootScope,
			wantSyncKind:      configsync.RootSyncKind,
			wantSyncNamespace: configsync.ControllerNamespace,
		},
		"RepoSync": {
			scope:             declared.Scope("some-ns"),
			wantSyncKind:      configsync.RepoSyncKind,
			wantSyncNamespace: "some-ns",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ic := NewInventoryConverter(tc.scope, invName, metadata.StatusEnabled)
			require.Equal(t, tc.wantSyncKind, ic.syncKind)
			require.Equal(t, tc.wantSyncNamespace, ic.syncNamespace)
		})
	}
}
