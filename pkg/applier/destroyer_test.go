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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeKptDestroyer struct {
	events []event.Event
}

func newFakeKptDestroyer(events []event.Event) *fakeKptDestroyer {
	return &fakeKptDestroyer{
		events: events,
	}
}

func (a *fakeKptDestroyer) Run(_ context.Context, _ inventory.Info, _ apply.DestroyerOptions) <-chan event.Event {
	events := make(chan event.Event, len(a.events))
	go func() {
		for _, e := range a.events {
			events <- e
		}
		close(events)
	}()
	return events
}

func TestDestroy(t *testing.T) {
	deploymentObj1 := newDeploymentObj()
	deploymentObj1.SetName("deployment-1")
	deploymentObj1Meta := object.UnstructuredToObjMetadata(deploymentObj1)
	deploymentObj1ID := core.IDOf(deploymentObj1)

	deploymentObj2 := newDeploymentObj()
	deploymentObj2.SetName("deployment-2")
	deploymentObj2Meta := object.UnstructuredToObjMetadata(deploymentObj2)

	testObj1 := newTestObj("test-1")
	testObj1Meta := object.UnstructuredToObjMetadata(testObj1)
	testObj1ID := core.IDOf(testObj1)

	testObj2 := newTestObj("test-2")
	testObj2ID := core.IDOf(testObj2)

	namespaceObj := k8sobjects.UnstructuredObject(kinds.Namespace(),
		core.Name("test-namespace"))
	namespaceMeta := object.UnstructuredToObjMetadata(namespaceObj)
	namespaceObjID := core.IDOf(namespaceObj)

	inventoryID := core.ID{
		GroupKind: v1alpha1.SchemeGroupVersionKind().GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      "rs",
			Namespace: "test-namespace",
		},
	}

	// Use sentinel errors so erors.Is works for comparison.
	testError1 := errors.New("test error 1")
	testError2 := errors.New("test error 2")
	etcdError := errors.New("etcdserver: request is too large") // satisfies util.IsRequestTooLargeError

	testcases := []struct {
		name                    string
		events                  []event.Event
		expectedError           error
		expectedObjectStatusMap ObjectStatusMap
		expectedSyncStats       *stats.SyncStats
	}{
		{
			name: "unknown type for some resource",
			events: []event.Event{
				formDeleteEvent(event.DeleteFailed, testObj1, applyerror.NewUnknownTypeError(testError1)),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			expectedError: DeleteErrorForResource(testError1, testObj1ID),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationFailed},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteFailed, 1).
				WithDeleteEvents(event.DeletePending, 1),
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formDeleteSkipEvent(testObj1Meta, testObj1.DeepCopy(), &inventory.PolicyPreventedActuationError{
					Strategy: actuation.ActuationStrategyDelete,
					Policy:   inventory.PolicyMustMatch,
					Status:   inventory.NoMatch,
				}),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			// Prunes and Deletes ignore PolicyPreventedActuationErrors.
			// This allows abandoning of managed objects.
			expectedError: nil,
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteSkipped, 1).
				WithDeleteEvents(event.DeletePending, 1),
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
			name: "failed to delete",
			events: []event.Event{
				formDeleteEvent(event.DeleteFailed, testObj1, testError1),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			expectedError: DeleteErrorForResource(testError1, testObj1ID),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationFailed},
				testObj2ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationPending},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteFailed, 1).
				WithDeleteEvents(event.DeletePending, 1),
		},
		{
			name: "skipped delete",
			events: []event.Event{
				formDeleteEvent(event.DeleteSuccessful, testObj1, nil),
				formDeleteEvent(event.DeleteSkipped, namespaceObj, &filter.NamespaceInUseError{
					Namespace: "test-namespace",
				}),
				formDeleteEvent(event.DeleteSuccessful, testObj2, nil),
			},
			expectedError: SkipErrorForResource(
				errors.New("namespace still in use: test-namespace"),
				idFrom(namespaceMeta),
				actuation.ActuationStrategyDelete),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj1ID:     &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
				namespaceObjID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
				testObj2ID:     &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteSuccessful, 2).
				WithDeleteEvents(event.DeleteSkipped, 1),
		},
		{
			name: "all passed",
			events: []event.Event{
				formDeleteEvent(event.DeletePending, testObj2, nil),
				formDeleteEvent(event.DeleteSuccessful, testObj1, nil),
				formDeleteEvent(event.DeleteSuccessful, deploymentObj1, nil),
			},
			expectedObjectStatusMap: ObjectStatusMap{
				testObj2ID:       &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationPending},
				testObj1ID:       &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
				deploymentObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSucceeded},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteSuccessful, 2).
				WithDeleteEvents(event.DeletePending, 1),
		},
		{
			name: "all failed",
			events: []event.Event{
				formDeleteEvent(event.DeletePending, testObj2, nil),
				formDeleteEvent(event.DeleteFailed, testObj1, testError1),
				formDeleteEvent(event.DeleteFailed, deploymentObj1, testError2),
			},
			expectedError: status.Wrap(
				DeleteErrorForResource(testError1, testObj1ID),
				DeleteErrorForResource(testError2, deploymentObj1ID)),
			expectedObjectStatusMap: ObjectStatusMap{
				testObj2ID:       &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationPending},
				testObj1ID:       &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationFailed},
				deploymentObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationFailed},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteFailed, 2).
				WithDeleteEvents(event.DeletePending, 1),
		},
		{
			name: "failed dependency during delete",
			events: []event.Event{
				formDeleteSkipEventWithDependent(deploymentObj1.DeepCopy(), deploymentObj2.DeepCopy()),
			},
			expectedError: SkipErrorForResource(
				&filter.DependencyPreventedActuationError{
					Object:                  deploymentObj1Meta,
					Strategy:                actuation.ActuationStrategyDelete,
					Relationship:            filter.RelationshipDependent,
					Relation:                deploymentObj2Meta,
					RelationPhase:           filter.PhaseReconcile,
					RelationActuationStatus: actuation.ActuationSucceeded,
					RelationReconcileStatus: actuation.ReconcileTimeout,
				},
				deploymentObj1ID,
				actuation.ActuationStrategyDelete),
			expectedObjectStatusMap: ObjectStatusMap{
				deploymentObj1ID: &ObjectStatus{Strategy: actuation.ActuationStrategyDelete, Actuation: actuation.ActuationSkipped},
			},
			expectedSyncStats: stats.NewSyncStats().
				WithDeleteEvents(event.DeleteSkipped, 1),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := testingfake.NewClient(t, core.Scheme)
			cs := &ClientSet{
				KptDestroyer: newFakeKptDestroyer(tc.events),
				Client:       fakeClient,
				// TODO: Add tests to cover disabling objects
				// TODO: Add tests to cover status mode
			}
			destroyer := NewSupervisor(cs, "test-namespace", "rs", 5*time.Minute)

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

			objectStatusMap, syncStats := destroyer.Destroy(context.Background(), eventHandler)
			testutil.AssertEqual(t, tc.expectedObjectStatusMap, objectStatusMap)
			testutil.AssertEqual(t, tc.expectedSyncStats, syncStats)
			testerrors.AssertEqual(t, tc.expectedError, errs)
		})
	}
}

func formDeleteEvent(status event.DeleteEventStatus, obj *unstructured.Unstructured, err error) event.Event {
	return event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			Identifier: object.UnstructuredToObjMetadata(obj),
			Object:     obj,
			Status:     status,
			Error:      err,
		},
	}
}

func formDeleteSkipEvent(id object.ObjMetadata, obj *unstructured.Unstructured, err error) event.Event {
	return event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			Status:     event.DeleteSkipped,
			Identifier: id,
			Object:     obj,
			Error:      err,
		},
	}
}

func formDeleteSkipEventWithDependent(obj, dependent *unstructured.Unstructured) event.Event {
	id := object.UnstructuredToObjMetadata(obj)
	e := event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			Status:     event.DeleteSkipped,
			Identifier: id,
			Object:     obj,
			Error: &filter.DependencyPreventedActuationError{
				Object:                  id,
				Strategy:                actuation.ActuationStrategyDelete,
				Relationship:            filter.RelationshipDependent,
				Relation:                object.UnstructuredToObjMetadata(dependent),
				RelationPhase:           filter.PhaseReconcile,
				RelationActuationStatus: actuation.ActuationSucceeded,
				RelationReconcileStatus: actuation.ReconcileTimeout,
			},
		},
	}
	return e
}
