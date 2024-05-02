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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
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
	events []event.Event
}

var _ KptApplier = &fakeKptApplier{}

func newFakeKptApplier(events []event.Event) *fakeKptApplier {
	return &fakeKptApplier{
		events: events,
	}
}

func (a *fakeKptApplier) Run(_ context.Context, _ inventory.Info, _ object.UnstructuredSet, _ apply.ApplierOptions) <-chan event.Event {
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
	deploymentID := object.UnstructuredToObjMetadata(deploymentObj)

	testObj := newTestObj("test-1")
	testID := object.UnstructuredToObjMetadata(testObj)

	abandonObj := deploymentObj.DeepCopy()
	abandonObj.SetName("abandon-me")
	abandonObj.SetAnnotations(map[string]string{
		common.LifecycleDeleteAnnotation: common.PreventDeletion,
		metadata.ResourceManagementKey:   metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:           core.GKNN(abandonObj),
		metadata.ResourceManagerKey:      resourceManager,
		metadata.OwningInventoryKey:      "anything",
		metadata.SyncTokenAnnotationKey:  "anything",
		"example-to-not-delete":          "anything",
	})
	abandonObj.SetLabels(map[string]string{
		metadata.ManagedByKey:   metadata.ManagedByValue,
		metadata.SystemLabel:    "anything",
		metadata.ArchLabel:      "anything",
		"example-to-not-delete": "anything",
	})

	testObj2 := newTestObj("test-2")
	testObj3 := newTestObj("test-3")

	objs := []client.Object{deploymentObj, testObj}

	namespaceObj := fake.UnstructuredObject(kinds.Namespace(),
		core.Name(string(syncScope)))
	namespaceID := object.UnstructuredToObjMetadata(namespaceObj)

	uid := core.ID{
		GroupKind: live.ResourceGroupGVK.GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      syncName,
			Namespace: string(syncScope),
		},
	}

	// Use sentinel errors so erors.Is works for comparison.
	// testError := errors.New("test error")
	etcdError := errors.New("etcdserver: request is too large") // satisfies util.IsRequestTooLargeError

	testcases := []struct {
		name               string
		serverObjs         []client.Object
		events             []event.Event
		expectedError      status.MultiError
		expectedServerObjs []client.Object
	}{
		{
			name: "unknown type for some resource",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: ErrorForResourceWithResource(errors.New("unknown type"), idFrom(testID), testObj),
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formApplySkipEvent(testID, testObj.DeepCopy(), &inventory.PolicyPreventedActuationError{
					Strategy: actuation.ActuationStrategyApply,
					Policy:   inventory.PolicyMustMatch,
					Status:   inventory.NoMatch,
				}),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: KptManagementConflictError(testObj),
		},
		{
			name: "inventory object is too large",
			events: []event.Event{
				formErrorEvent(etcdError),
			},
			expectedError: largeResourceGroupError(etcdError, uid),
		},
		{
			name: "failed to apply",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
			},
			expectedError: ErrorForResourceWithResource(errors.New("failed apply"), idFrom(testID), testObj),
		},
		{
			name: "failed to prune",
			events: []event.Event{
				formPruneEvent(event.PruneFailed, testObj, errors.New("failed pruning")),
				formPruneEvent(event.PruneSuccessful, testObj2, nil),
			},
			expectedError: PruneErrorForResource(errors.New("failed pruning"), idFrom(testID)),
		},
		{
			name: "skipped pruning",
			events: []event.Event{
				formPruneEvent(event.PruneSuccessful, testObj, nil),
				formPruneEvent(event.PruneSkipped, namespaceObj, &filter.NamespaceInUseError{
					Namespace: "test-namespace",
				}),
				formPruneEvent(event.PruneSuccessful, testObj2, nil),
			},
			expectedError: SkipErrorForResource(
				errors.New("namespace still in use: test-namespace"),
				idFrom(namespaceID),
				actuation.ActuationStrategyDelete),
		},
		{
			name: "all passed",
			events: []event.Event{
				formApplyEvent(event.ApplySuccessful, testObj, nil),
				formApplyEvent(event.ApplySuccessful, deploymentObj, nil),
				formApplyEvent(event.ApplyPending, testObj2, nil),
				formPruneEvent(event.PruneSuccessful, testObj3, nil),
			},
		},
		{
			name: "all failed",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, testObj, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyFailed, deploymentObj, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, testObj2, nil),
				formPruneEvent(event.PruneSuccessful, testObj3, nil),
			},
			expectedError: status.Wrap(
				ErrorForResourceWithResource(errors.New("unknown type"), idFrom(testID), testObj),
				ErrorForResourceWithResource(errors.New("failed apply"), idFrom(deploymentID), deploymentObj)),
		},
		{
			name: "failed dependency during apply",
			events: []event.Event{
				formApplySkipEventWithDependency(deploymentID, deploymentObj.DeepCopy()),
			},
			expectedError: SkipErrorForResource(
				errors.New("dependency apply reconcile timeout: namespace_name_group_kind"),
				idFrom(deploymentID),
				actuation.ActuationStrategyApply),
		},
		{
			name: "failed dependency during prune",
			events: []event.Event{
				formPruneSkipEventWithDependency(deploymentID),
			},
			expectedError: SkipErrorForResource(
				errors.New("dependent delete actuation failed: namespace_name_group_kind"),
				idFrom(deploymentID),
				actuation.ActuationStrategyDelete),
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
			expectedServerObjs: []client.Object{
				func() client.Object {
					obj := abandonObj.DeepCopy()
					obj.SetAnnotations(map[string]string{
						common.LifecycleDeleteAnnotation: common.PreventDeletion,
						// all configsync annotations removed
						"example-to-not-delete": "anything",
					})
					obj.SetLabels(map[string]string{
						// all configsync labels removed
						"example-to-not-delete": "anything",
					})
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
			rsObj.SetNamespace(string(syncScope))
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

			errs := applier.Apply(context.Background(), objs)
			testerrors.AssertEqual(t, tc.expectedError, errs)
			fakeClient.Check(t, tc.expectedServerObjs...)
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
	deploymentID := object.UnstructuredToObjMetadata(deploymentObj)
	testObj := newTestObj("test-1")
	testID := object.UnstructuredToObjMetadata(testObj)

	ctx := context.Background()
	s := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	unknownTypeResources := make(map[core.ID]struct{})
	eh := eventHandler{
		isDestroy: false,
	}

	resourceMap := make(map[core.ID]client.Object)
	resourceMap[idFrom(deploymentID)] = deploymentObj

	err := eh.processApplyEvent(ctx, formApplyEvent(event.ApplyFailed, deploymentObj, fmt.Errorf("test error")).ApplyEvent, s.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap)
	expectedError := ErrorForResourceWithResource(fmt.Errorf("test error"), idFrom(deploymentID), deploymentObj)
	testerrors.AssertEqual(t, expectedError, err, "expected processPruneEvent to error on apply %s", event.ApplyFailed)

	err = eh.processApplyEvent(ctx, formApplyEvent(event.ApplySuccessful, testObj, nil).ApplyEvent, s.ApplyEvent, objStatusMap, unknownTypeResources, resourceMap)
	assert.Nil(t, err, "expected processApplyEvent NOT to error on apply %s", event.ApplySuccessful)

	expectedApplyStatus := stats.NewSyncStats()
	expectedApplyStatus.ApplyEvent.Add(event.ApplyFailed)
	expectedApplyStatus.ApplyEvent.Add(event.ApplySuccessful)
	testutil.AssertEqual(t, expectedApplyStatus, s, "expected event stats to match")

	expectedObjStatusMap := ObjectStatusMap{
		idFrom(deploymentID): {
			Strategy:  actuation.ActuationStrategyApply,
			Actuation: actuation.ActuationFailed,
		},
		idFrom(testID): {
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
	s := stats.NewSyncStats()
	objStatusMap := make(ObjectStatusMap)
	cs := &ClientSet{}
	eh := eventHandler{
		isDestroy: false,
		clientSet: cs,
	}

	err := eh.processPruneEvent(ctx, formPruneEvent(event.PruneFailed, deploymentObj, fmt.Errorf("test error")).PruneEvent, s.PruneEvent, objStatusMap)
	expectedError := PruneErrorForResource(fmt.Errorf("test error"), idFrom(deploymentID))
	testerrors.AssertEqual(t, expectedError, err, "expected processPruneEvent to error on prune %s", event.PruneFailed)

	err = eh.processPruneEvent(ctx, formPruneEvent(event.PruneSuccessful, testObj, nil).PruneEvent, s.PruneEvent, objStatusMap)
	assert.Nil(t, err, "expected processPruneEvent NOT to error on prune %s", event.PruneSuccessful)

	expectedApplyStatus := stats.NewSyncStats()
	expectedApplyStatus.PruneEvent.Add(event.PruneFailed)
	expectedApplyStatus.PruneEvent.Add(event.PruneSuccessful)
	testutil.AssertEqual(t, expectedApplyStatus, s, "expected event stats to match")

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

			p := eventHandler{
				isDestroy: tc.isDestroy,
			}
			s := stats.NewSyncStats()
			objStatusMap := make(ObjectStatusMap)
			expectedApplyStatus := stats.NewSyncStats()
			expectedObjStatusMap := ObjectStatusMap{}

			for _, e := range tc.events {
				err := p.processWaitEvent(formWaitEvent(e.status, e.id).WaitEvent, s.WaitEvent, objStatusMap)
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

			testutil.AssertEqual(t, expectedApplyStatus, s, "expected event stats to match")
			testutil.AssertEqual(t, expectedObjStatusMap, objStatusMap, "expected object status to match")
		})
	}
}

func newDeploymentObj() *unstructured.Unstructured {
	return fake.UnstructuredObject(kinds.Deployment(),
		core.Namespace("test-namespace"), core.Name("random-name"), core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"))
}

func newTestObj(name string) *unstructured.Unstructured {
	return fake.UnstructuredObject(schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}, core.Namespace("test-namespace"), core.Name(name), core.Annotation(metadata.SourcePathAnnotationKey, "foo/test.yaml"))
}
