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

package applier

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/applyset"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/dependson"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeKptApplier struct {
	events      []event.Event
	objsToApply object.UnstructuredSet
}

var _ KptApplier = &fakeKptApplier{}

func newFakeKptApplier(events []event.Event) *fakeKptApplier {
	return &fakeKptApplier{
		events: events,
	}
}

func (a *fakeKptApplier) Run(_ context.Context, _ inventory.Info, objsToApply object.UnstructuredSet, _ apply.ApplierOptions) <-chan event.Event {
	a.objsToApply = objsToApply
	events := make(chan event.Event, len(a.events))
	go func() {
		for _, e := range a.events {
			events <- e
		}
		close(events)
	}()
	return events
}

func TestApply(t *testing.T) {
	syncScope := declared.Scope("test-namespace")
	syncName := "rs"
	resourceManager := declared.ResourceManager(syncScope, syncName)

	deploymentObj := newDeploymentObj()
	deploymentObjMeta := object.UnstructuredToObjMetadata(deploymentObj)
	deploymentObjID := core.IDOf(deploymentObj)

	testObj1 := newTestObj("test-1")
	testObj1Meta := object.UnstructuredToObjMetadata(testObj1)
	testObj1ID := core.IDOf(testObj1)

	abandonObj := deploymentObj.DeepCopy()
	abandonObj.SetName("abandon-me")
	abandonObj.SetAnnotations(map[string]string{
		common.LifecycleDeleteAnnotation: common.PreventDeletion, // not removed
		metadata.ResourceManagementKey:   metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:           core.GKNN(abandonObj),
		metadata.ResourceManagerKey:      resourceManager,
		metadata.OwningInventoryKey:      "anything",
		metadata.SyncTokenAnnotationKey:  "anything",
		"example-to-not-delete":          "anything", // not removed
	})
	abandonObj.SetLabels(map[string]string{
		metadata.ManagedByKey: metadata.ManagedByValue,
		metadata.SystemLabel:  "anything",
		metadata.ArchLabel:    "anything",
		// TODO: remove ApplySet metadata on abandon (b/355534413)
		metadata.ApplySetPartOfLabel: "anything", // not removed
		"example-to-not-delete":      "anything", // not removed
	})
	abandonObjID := core.IDOf(abandonObj)

	testObj2 := newTestObj("test-2")
	testObj2ID := core.IDOf(testObj2)
	testObj3 := newTestObj("test-3")
	testObj3ID := core.IDOf(testObj3)

	objs := []client.Object{deploymentObj, testObj1}

	namespaceObj := k8sobjects.UnstructuredObject(kinds.Namespace(),
		core.Name(syncScope.SyncNamespace()))
	namespaceObjMeta := object.UnstructuredToObjMetadata(namespaceObj)
	namespaceObjID := core.IDOf(namespaceObj)

	inventoryID := core.ID{
		GroupKind: live.ResourceGroupGVK.GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      syncName,
			Namespace: syncScope.SyncNamespace(),
		},
	}

	// Use sentinel errors so erors.Is works for comparison.
	// testError := errors.New("test error")
	etcdError := errors.New("etcdserver: request is too large") // satisfies util.IsRequestTooLargeError

	testcases := []struct {
		name                    string
		serverObjs              []client.Object
		events                  []event.Event
		expectedError           status.MultiError
		expectedObjectStatusMap ObjectStatusMap
		expectedSyncStats       *stats.SyncStats
		expectedServerObjs      []client.Object
	}{
		{
			name: "unknown type for some resource",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj1, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: ErrorForResourceWithResource(errors.New("unknown type"), testObj1ID, testObj1),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationFailed},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplyFailed, 1).
				WithApplyEvents(event.ApplyPending, 1),
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formApplySkipEvent(testObj1Meta, testObj1.DeepCopy(), &inventory.PolicyPreventedActuationError{
					Strategy: actuation.ActuationStrategyApply,
					Policy:   inventory.PolicyMustMatch,
					Status:   inventory.NoMatch,
				}),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: KptManagementConflictError(testObj1),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationSkipped},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplySkipped, 1).
				WithApplyEvents(event.ApplyPending, 1),
		},
		{
			name: "inventory object is too large",
			events: []event.Event{
				formErrorEvent(etcdError),
			},
			expectedError:           largeResourceGroupError(etcdError, inventoryID),
			expectedObjectStatusMap: ObjectStatusMap{},
			expectedSyncStats: stats.NewSyncStats().
				WithErrorEvents(1),
		},
		{
			name: "failed to apply",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj1, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: ErrorForResourceWithResource(errors.New("failed apply"), testObj1ID, testObj1),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationFailed},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplyFailed, 1).
				WithApplyEvents(event.ApplyPending, 1),
		},
		{
			name: "failed to prune",
			events: []event.Event{
				formPruneEvent(event.PruneFailed, testObj1, errors.New("failed pruning")),
				formPruneEvent(event.PruneSuccessful, testObj2, nil),
			},
			expectedError: PruneErrorForResource(errors.New("failed pruning"), testObj1ID),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationFailed},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithPruneEvents(event.PruneFailed, 1).
				WithPruneEvents(event.PruneSuccessful, 1),
		},
		{
			name: "skipped pruning",
			events: []event.Event{
				formPruneEvent(event.PruneSuccessful, testObj1, nil),
				formPruneEvent(event.PruneSkipped, namespaceObj, &filter.NamespaceInUseError{
					Namespace: "test-namespace",
				}),
				formPruneEvent(event.PruneSuccessful, testObj2, nil),
			},
			expectedError: SkipErrorForResource(
				errors.New("namespace still in use: test-namespace"),
				idFrom(namespaceObjMeta),
				actuation.ActuationStrategyDelete),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID:     &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
				namespaceObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
				testObj2ID:     &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithPruneEvents(event.PruneSuccessful, 2).
				WithPruneEvents(event.PruneSkipped, 1),
		},
		{
			name: "all passed",
			events: []event.Event{
				formApplyEvent(event.ApplySuccessful, testObj1, nil),
				formApplyEvent(event.ApplySuccessful, deploymentObj, nil),
				formApplyEvent(event.ApplyPending, testObj2, nil),
				formPruneEvent(event.PruneSuccessful, testObj3, nil),
			},
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationSucceeded},
				deploymentObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationSucceeded},
				testObj2ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationPending},
				testObj3ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplySuccessful, 2).
				WithApplyEvents(event.ApplyPending, 1).
				WithPruneEvents(event.PruneSuccessful, 1),
		},
		{
			name: "all failed",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj1, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyFailed, deploymentObj, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
				formPruneEvent(event.PruneSuccessful, testObj3, nil),
			},
			expectedError: status.Append(
				ErrorForResourceWithResource(errors.New("unknown type"), testObj1ID, testObj1),
				ErrorForResourceWithResource(errors.New("failed apply"), deploymentObjID, deploymentObj)),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationFailed},
				deploymentObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationFailed},
				testObj2ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationPending},
				testObj3ID:      &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplyFailed, 2).
				WithApplyEvents(event.ApplyPending, 1).
				WithPruneEvents(event.PruneSuccessful, 1),
		},
		{
			name: "failed dependency during apply",
			events: []event.Event{
				formApplySkipEventWithDependency(deploymentObjMeta, deploymentObj.DeepCopy()),
			},
			expectedError: SkipErrorForResource(
				errors.New("dependency apply reconcile timeout: namespace_name_group_kind"),
				deploymentObjID,
				actuation.ActuationStrategyApply),
			expectedObjectStatusMap: ObjectStatusMap{
				deploymentObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyApply, Actuation: actuation.ActuationSkipped},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithApplyEvents(event.ApplySkipped, 1),
		},
		{
			name: "failed dependency during prune",
			events: []event.Event{
				formPruneSkipEventWithDependency(deploymentObjMeta),
			},
			expectedError: SkipErrorForResource(
				errors.New("dependent delete actuation failed: namespace_name_group_kind"),
				deploymentObjID,
				actuation.ActuationStrategyDelete),
			expectedObjectStatusMap: ObjectStatusMap{
				deploymentObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithPruneEvents(event.PruneSkipped, 1),
		},
		{
			name: "abandon object",
			serverObjs: []client.Object{
				abandonObj,
			},
			events: []event.Event{
				formPruneSkipEventWithDetach(abandonObj),
			},
			expectedError: nil,
			expectedObjectStatusMap: ObjectStatusMap{
				abandonObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithPruneEvents(event.PruneSkipped, 1),
			expectedServerObjs: []client.Object{
				func() client.Object {
					obj := abandonObj.DeepCopy()
					// all configsync annotations removed
					core.RemoveAnnotations(obj,
						metadata.ResourceManagementKey,
						metadata.ResourceIDKey,
						metadata.ResourceManagerKey,
						metadata.OwningInventoryKey,
						metadata.SyncTokenAnnotationKey)
					// all configsync labels removed
					core.RemoveLabels(obj,
						metadata.ManagedByKey,
						metadata.SystemLabel,
						metadata.ArchLabel)
					obj.SetUID("1")
					obj.SetResourceVersion("2")
					obj.SetGeneration(1)
					return obj
				}(),
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			rsObj := &unstructured.Unstructured{}
			rsObj.SetGroupVersionKind(kinds.RepoSyncV1Beta1())
			rsObj.SetNamespace(syncScope.SyncNamespace())
			rsObj.SetName(syncName)
			tc.serverObjs = append(tc.serverObjs, rsObj)

			expectedRSObj := rsObj.DeepCopy()
			expectedRSObj.SetUID("1")
			expectedRSObj.SetResourceVersion("1")
			expectedRSObj.SetGeneration(1)
			tc.expectedServerObjs = append(tc.expectedServerObjs, expectedRSObj)

			fakeClient := testingfake.NewClient(t, core.Scheme, tc.serverObjs...)
			cs := &ClientSet{
				KptApplier: newFakeKptApplier(tc.events),
				Client:     fakeClient,
				Mapper:     fakeClient.RESTMapper(),
				// TODO: Add tests to cover status mode
			}
			applier, err := NewNamespaceSupervisor(cs, syncScope, syncName, 5*time.Minute)
			require.NoError(t, err)

			var errs status.MultiError
			eventHandler := func(event Event) {
				if errEvent, ok := event.(ErrorEvent); ok {
					if errs == nil {
						errs = errEvent.Error
					} else {
						errs = status.Append(errs, errEvent.Error)
					}
				}
			}

			ctx := context.Background()
			resources := &declared.Resources{}
			_, err = resources.UpdateDeclared(ctx, objs, "")
			require.NoError(t, err)

			objectStatusMap, syncStats := applier.Apply(context.Background(), eventHandler, resources)

			testutil.AssertEqual(t, tc.expectedError, errs)
			testutil.AssertEqual(t, tc.expectedObjectStatusMap, objectStatusMap)
			testutil.AssertEqual(t, tc.expectedSyncStats, syncStats)

			fakeClient.Check(t, tc.expectedServerObjs...)
		})
	}
}

func TestApplyMutationIgnoredObjects(t *testing.T) {
	rootSyncName := "my-rs"
	syncScope := declared.RootScope
	applySetID := applyset.IDFromSync(rootSyncName, syncScope)
	testGitCommit := "example-commit"
	gitContextOutput := fmt.Sprintf(`{"repo":%q,"branch":%q,"rev":%q}`,
		"example-repo", "example-branch", testGitCommit)
	resourceManager := declared.ResourceManager(syncScope, rootSyncName)

	testcases := []struct {
		name                       string
		serverObjs                 []client.Object
		declaredObjs               []client.Object
		cachedIgnoredObjs          []client.Object
		expectedObjsToApply        object.UnstructuredSet
		expectedItemsInIgnoreCache []client.Object
	}{
		{
			name: "unmanaged object exists on the cluster",
			serverObjs: []client.Object{
				createServerObject(
					k8sobjects.NamespaceObject("umanaged-ns",
						syncertest.ManagementDisabled,
						core.Label("foo", "bar"),
					)),
			},
			declaredObjs: []client.Object{
				newNamespaceObj(
					core.Name("umanaged-ns"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.ApplySetPartOfLabel, applySetID),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, gitContextOutput),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation),
			},
			expectedObjsToApply: object.UnstructuredSet{
				asUnstructuredSanitizedObj(k8sobjects.NamespaceObject("umanaged-ns",
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, gitContextOutput),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					core.Annotation(metadata.ResourceManagerKey, resourceManager),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation,
					core.Generation(1),
					core.UID("1"),
					core.ResourceVersion("1"),
					core.Label("foo", "bar"))),
			},
			expectedItemsInIgnoreCache: []client.Object{
				createServerObject(k8sobjects.NamespaceObject("umanaged-ns",
					syncertest.ManagementDisabled,
					core.Label("foo", "bar"),
					core.Generation(1),
					core.UID("1"),
					core.ResourceVersion("1"))),
			},
		},
		{
			name: "managed object exists on the cluster without the ignore mutation annotation",
			serverObjs: []client.Object{
				createServerObject(k8sobjects.NamespaceObject("managed-ns",
					syncertest.ManagementEnabled,
					core.Label("foo", "bar"))),
			},
			declaredObjs: []client.Object{
				k8sobjects.NamespaceObject("managed-ns",
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation),
			},
			expectedObjsToApply: object.UnstructuredSet{
				asUnstructuredSanitizedObj(k8sobjects.NamespaceObject("managed-ns",
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation,
					core.Generation(1),
					core.UID("1"),
					core.ResourceVersion("1"),
					core.Label("foo", "bar"))),
			},
			expectedItemsInIgnoreCache: []client.Object{
				createServerObject(k8sobjects.NamespaceObject("managed-ns",
					syncertest.ManagementDisabled,
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label("foo", "bar"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.ResourceIDKey, "_namespace_managed-ns"),
					core.Generation(1),
					core.UID("1"),
					core.ResourceVersion("1")))},
		},
		{
			name: "managed and mutation-ignored object was previously deleted",
			serverObjs: []client.Object{
				k8sobjects.NamespaceObject("other-ns",
					syncertest.ManagementEnabled,
					core.Label("foo", "baz")),
			},
			declaredObjs: []client.Object{
				newNamespaceObj(core.Name("deleted-ns"),
					core.Label("foo", "bar"),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation)},
			cachedIgnoredObjs: []client.Object{
				&queue.Deleted{
					Object: newNamespaceObj(
						core.Name("deleted-ns"),
						syncertest.ManagementEnabled,
						syncertest.IgnoreMutationAnnotation)}},
			expectedItemsInIgnoreCache: []client.Object{
				&queue.Deleted{
					Object: newNamespaceObj(
						core.Name("deleted-ns"),
						syncertest.ManagementEnabled,
						syncertest.IgnoreMutationAnnotation)}},
			expectedObjsToApply: object.UnstructuredSet{
				asUnstructuredSanitizedObj(newNamespaceObj(core.Name("deleted-ns"),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation,
					core.Label("foo", "bar"))),
			},
		},
		{
			name:       "mutation-ignored object doesn't currently exist on the cluster",
			serverObjs: []client.Object{},
			declaredObjs: []client.Object{
				newNamespaceObj(core.Name("test-ns"),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation)},
			cachedIgnoredObjs:          []client.Object{},
			expectedItemsInIgnoreCache: nil,
			expectedObjsToApply: object.UnstructuredSet{
				asUnstructuredSanitizedObj(newNamespaceObj(core.Name("test-ns"),
					syncertest.ManagementEnabled,
					syncertest.IgnoreMutationAnnotation)),
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			rsObj := &unstructured.Unstructured{}
			rsObj.SetGroupVersionKind(kinds.RepoSyncV1Beta1())
			rsObj.SetNamespace(syncScope.SyncNamespace())
			rsObj.SetName(rootSyncName)
			tc.serverObjs = append(tc.serverObjs, rsObj)

			syncScope := declared.Scope("test-namespace")
			syncName := "rs"
			fakeClient := testingfake.NewClient(t, core.Scheme, tc.serverObjs...)
			fakeKptApplier := newFakeKptApplier([]event.Event{})
			cs := &ClientSet{
				KptApplier: fakeKptApplier,
				Client:     fakeClient,
				Mapper:     fakeClient.RESTMapper(),
				// TODO: Add tests to cover status mode
			}
			var errs status.MultiError
			eventHandler := func(event Event) {
				if errEvent, ok := event.(ErrorEvent); ok {
					if errs == nil {
						errs = errEvent.Error
					} else {
						errs = status.Append(errs, errEvent.Error)
					}
				}
			}

			applier, err := NewNamespaceSupervisor(cs, syncScope, syncName, 5*time.Minute)
			require.NoError(t, err)

			resources := &declared.Resources{}
			_, err = resources.UpdateDeclared(context.Background(), tc.declaredObjs, "")
			require.NoError(t, err)
			resources.UpdateIgnored(tc.cachedIgnoredObjs...)

			applier.Apply(context.Background(), eventHandler, resources)

			testutil.AssertEqual(t, tc.expectedObjsToApply, fakeKptApplier.objsToApply)
			testutil.AssertEqual(t, tc.expectedItemsInIgnoreCache, resources.IgnoredObjects())
		})
	}
}

func formApplyEvent(status event.ApplyEventStatus, obj *unstructured.Unstructured, err error) event.Event {
	return event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Identifier: object.UnstructuredToObjMetadata(obj),
			Resource:   obj,
			Status:     status,
			Error:      err,
		},
	}
}

func formApplySkipEvent(id object.ObjMetadata, obj *unstructured.Unstructured, err error) event.Event {
	return event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Status:     event.ApplySkipped,
			Identifier: id,
			Resource:   obj,
			Error:      err,
		},
	}
}

func formApplySkipEventWithDependency(id object.ObjMetadata, obj *unstructured.Unstructured) event.Event {
	obj.SetAnnotations(map[string]string{dependson.Annotation: "group/namespaces/namespace/kind/name"})
	e := event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Status:     event.ApplySkipped,
			Identifier: id,
			Resource:   obj,
			Error: &filter.DependencyPreventedActuationError{
				Object:       id,
				Strategy:     actuation.ActuationStrategyApply,
				Relationship: filter.RelationshipDependency,
				Relation: object.ObjMetadata{
					GroupKind: schema.GroupKind{
						Group: "group",
						Kind:  "kind",
					},
					Name:      "name",
					Namespace: "namespace",
				},
				RelationPhase:           filter.PhaseReconcile,
				RelationActuationStatus: actuation.ActuationSucceeded,
				RelationReconcileStatus: actuation.ReconcileTimeout,
			},
		},
	}
	return e
}

func formPruneSkipEventWithDependency(id object.ObjMetadata) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			Status:     event.PruneSkipped,
			Identifier: id,
			Object:     &unstructured.Unstructured{},
			Error: &filter.DependencyPreventedActuationError{
				Object:       id,
				Strategy:     actuation.ActuationStrategyDelete,
				Relationship: filter.RelationshipDependent,
				Relation: object.ObjMetadata{
					GroupKind: schema.GroupKind{
						Group: "group",
						Kind:  "kind",
					},
					Name:      "name",
					Namespace: "namespace",
				},
				RelationPhase:           filter.PhaseActuation,
				RelationActuationStatus: actuation.ActuationFailed,
				RelationReconcileStatus: actuation.ReconcilePending,
			},
		},
	}
}

func formPruneSkipEventWithDetach(obj *unstructured.Unstructured) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			Status:     event.PruneSkipped,
			Identifier: object.UnstructuredToObjMetadata(obj),
			Object:     obj,
			Error: &filter.AnnotationPreventedDeletionError{
				Annotation: common.LifecycleDeleteAnnotation,
				Value:      common.PreventDeletion,
			},
		},
	}
}

func formPruneEvent(status event.PruneEventStatus, obj *unstructured.Unstructured, err error) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			Object:     obj,
			Identifier: object.UnstructuredToObjMetadata(obj),
			Error:      err,
			Status:     status,
		},
	}
}

func formWaitEvent(status event.WaitEventStatus, id *object.ObjMetadata) event.Event {
	e := event.Event{
		Type: event.WaitType,
		WaitEvent: event.WaitEvent{
			Status: status,
		},
	}
	if id != nil {
		e.WaitEvent.Identifier = *id
	}
	return e
}

func formErrorEvent(err error) event.Event {
	e := event.Event{
		Type: event.ErrorType,
		ErrorEvent: event.ErrorEvent{
			Err: err,
		},
	}
	return e
}

func TestProcessApplyEvent(t *testing.T) {
	deploymentObj := newDeploymentObj()
	deploymentObjID := core.IDOf(deploymentObj)
	testObj1 := newTestObj("test-1")
	testObj1ID := core.IDOf(testObj1)

	ctx := context.Background()
	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	unknownTypeResources := make(map[core.ID]struct{})
	s := supervisor{}

	resourceMap := make(map[core.ID]client.Object)
	resourceMap[deploymentObjID] = deploymentObj

	err := s.processApplyEvent(ctx, formApplyEvent(event.ApplyFailed, deploymentObj, fmt.Errorf("test error")).ApplyEvent, syncStats.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap)
	expectedError := ErrorForResourceWithResource(fmt.Errorf("test error"), deploymentObjID, deploymentObj)
	testutil.AssertEqual(t, expectedError, err, "expected processPruneEvent to error on apply %s", event.ApplyFailed)

	expectedCSE := v1beta1.ConfigSyncError{
		Code:         "2009",
		ErrorMessage: "KNV2009: failed to apply Deployment.apps, test-namespace/random-name: test error\n\nsource: namespaces/foo/role.yaml\nnamespace: test-namespace\nmetadata.name: random-name\ngroup: apps\nversion: v1\nkind: Deployment\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009",
		Resources: []v1beta1.ResourceRef{{
			SourcePath: "namespaces/foo/role.yaml",
			Name:       "random-name",
			Namespace:  "test-namespace",
			GVK: metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		}},
	}
	testutil.AssertEqual(t, expectedCSE, err.ToCSE(), "expected CSEs to match")

	err = s.processApplyEvent(ctx, formApplyEvent(event.ApplySuccessful, testObj1, nil).ApplyEvent, syncStats.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap)
	assert.Nil(t, err, "expected processApplyEvent NOT to error on apply %s", event.ApplySuccessful)

	expectedApplyStatus := stats.NewSyncStats()
	expectedApplyStatus.ApplyEvent.Add(event.ApplyFailed)
	expectedApplyStatus.ApplyEvent.Add(event.ApplySuccessful)
	testutil.AssertEqual(t, expectedApplyStatus, syncStats, "expected event stats to match")

	expectedObjStatusMap := ObjectStatusMap{
		deploymentObjID: {
			Strategy:  actuation.ActuationStrategyApply,
			Actuation: actuation.ActuationFailed,
		},
		testObj1ID: {
			Strategy:  actuation.ActuationStrategyApply,
			Actuation: actuation.ActuationSucceeded,
		},
	}
	testutil.AssertEqual(t, expectedObjStatusMap, objStatusMap, "expected object status to match")

	// TODO: test handleMetrics on success
	// TODO: test unknownTypeResources on UnknownTypeError
	// TODO: test handleApplySkippedEvent on skip
}

func TestProcessPruneEvent(t *testing.T) {
	deploymentObj := newDeploymentObj()
	deploymentID := object.UnstructuredToObjMetadata(deploymentObj)
	testObj := newTestObj("test-1")
	testID := object.UnstructuredToObjMetadata(testObj)

	ctx := context.Background()
	syncStats := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	cs := &ClientSet{}
	s := supervisor{
		clientSet: cs,
	}

	//TODO: Refactor
	testObj2 := k8sobjects.UnstructuredObject(schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}, core.Namespace("test-namespace"), core.Name("test-1"), core.Annotation(metadata.SourcePathAnnotationKey, "foo/test.yaml"), syncertest.IgnoreMutationAnnotation)

	resources := &declared.Resources{}
	resources.UpdateIgnored(testObj2)

	err := s.processPruneEvent(ctx, formPruneEvent(event.PruneFailed, deploymentObj, fmt.Errorf("test error")).PruneEvent, syncStats.PruneEvent, objStatusMap, resources)
	expectedError := PruneErrorForResource(fmt.Errorf("test error"), idFrom(deploymentID))
	testerrors.AssertEqual(t, expectedError, err, "expected processPruneEvent to error on prune %s", event.PruneFailed)

	err = s.processPruneEvent(ctx, formPruneEvent(event.PruneSuccessful, testObj, nil).PruneEvent, syncStats.PruneEvent, objStatusMap, resources)
	assert.Nil(t, err, "expected processPruneEvent NOT to error on prune %s", event.PruneSuccessful)

	expectedApplyStatus := stats.NewSyncStats()
	expectedApplyStatus.PruneEvent.Add(event.PruneFailed)
	expectedApplyStatus.PruneEvent.Add(event.PruneSuccessful)
	testutil.AssertEqual(t, expectedApplyStatus, syncStats, "expected event stats to match")

	expectedObjStatusMap := ObjectStatusMap{
		idFrom(deploymentID): {
			Strategy:  actuation.ActuationStrategyDelete,
			Actuation: actuation.ActuationFailed,
		},
		idFrom(testID): {
			Strategy:  actuation.ActuationStrategyDelete,
			Actuation: actuation.ActuationSucceeded,
		},
	}
	testutil.AssertEqual(t, expectedObjStatusMap, objStatusMap, "expected object status to match")

	testutil.AssertEqual(t, 0, resources.GetIgnoredObjsCache().Len())

	// TODO: test handleMetrics on success
	// TODO: test PruneErrorForResource on failed
	// TODO: test SpecialNamespaces on skip
	// TODO: test handlePruneSkippedEvent on skip
}

func TestProcessWaitEvent(t *testing.T) {
	deploymentID := object.UnstructuredToObjMetadata(newDeploymentObj())
	testID := object.UnstructuredToObjMetadata(newTestObj("test-1"))

	type eventStatus struct {
		status         event.WaitEventStatus
		id             *object.ObjMetadata
		expectedErr    error
		expectedStatus actuation.ReconcileStatus
	}
	testCases := []struct {
		name                 string
		isDestroy            bool
		events               []eventStatus
		expectedWaitStatuses []stats.WaitEventStats
		expectedObjStatusMap ObjectStatusMap
	}{
		{
			name:      "reconcile fail/success for an apply",
			isDestroy: false,
			events: []eventStatus{
				{
					status:         event.ReconcileFailed,
					id:             &deploymentID,
					expectedErr:    nil,
					expectedStatus: actuation.ReconcileFailed,
				},
				{
					status:         event.ReconcileSuccessful,
					id:             &testID,
					expectedErr:    nil,
					expectedStatus: actuation.ReconcileSucceeded,
				},
			},
		},
		{
			name:      "reconcile fail/success for a destroy",
			isDestroy: true,
			events: []eventStatus{
				{
					status:         event.ReconcileFailed,
					id:             &deploymentID,
					expectedErr:    fmt.Errorf("KNV2009: failed to wait for Deployment.apps, test-namespace/random-name: reconcile failed\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009"),
					expectedStatus: actuation.ReconcileFailed,
				},
				{
					status:         event.ReconcileSuccessful,
					id:             &testID,
					expectedErr:    nil,
					expectedStatus: actuation.ReconcileSucceeded,
				},
			},
		},
		{
			name:      "reconcile timeout/success for a destroy",
			isDestroy: true,
			events: []eventStatus{
				{
					status:         event.ReconcileTimeout,
					id:             &deploymentID,
					expectedErr:    fmt.Errorf("KNV2009: failed to wait for Deployment.apps, test-namespace/random-name: reconcile timeout\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009"),
					expectedStatus: actuation.ReconcileTimeout,
				},
				{
					status:         event.ReconcileSuccessful,
					id:             &testID,
					expectedErr:    nil,
					expectedStatus: actuation.ReconcileSucceeded,
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			s := supervisor{}
			syncStats := stats.NewSyncStats()
			objStatusMap := make(ObjectStatusMap)
			expectedApplyStatus := stats.NewSyncStats()
			expectedObjStatusMap := ObjectStatusMap{}

			for _, e := range tc.events {
				err := s.processWaitEvent(formWaitEvent(e.status, e.id).WaitEvent, syncStats.WaitEvent, objStatusMap, tc.isDestroy)
				if e.expectedErr == nil {
					assert.Nil(t, err)
				} else {
					assert.Equal(t, err.Error(), e.expectedErr.Error())
				}
				expectedApplyStatus.WaitEvent.Add(e.status)
				expectedObjStatusMap[idFrom(*e.id)] = &ObjectStatus{
					Reconcile: e.expectedStatus,
				}
			}

			testutil.AssertEqual(t, expectedApplyStatus, syncStats, "expected event stats to match")
			testutil.AssertEqual(t, expectedObjStatusMap, objStatusMap, "expected object status to match")
		})
	}
}

func newDeploymentObj() *unstructured.Unstructured {
	return k8sobjects.UnstructuredObject(kinds.Deployment(),
		core.Namespace("test-namespace"), core.Name("random-name"), core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"))
}

func newTestObj(name string) *unstructured.Unstructured {
	return k8sobjects.UnstructuredObject(schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}, core.Namespace("test-namespace"), core.Name(name), core.Annotation(metadata.SourcePathAnnotationKey, "foo/test.yaml"))
}

func newNamespaceObj(opts ...core.MetaMutator) *unstructured.Unstructured {
	return k8sobjects.UnstructuredObject(kinds.Namespace(), opts...)
}

func asUnstructuredSanitizedObj(o client.Object) *unstructured.Unstructured {
	core.Scheme.Default(o)
	uObj, _ := reconcile.AsUnstructuredSanitized(o)

	return uObj
}

// createServerObject produces the expected output of the object stored on the (fake) server
func createServerObject(o client.Object) *unstructured.Unstructured {
	core.Scheme.Default(o)
	uObj, _ := reconcile.AsUnstructured(o)
	return uObj
}
