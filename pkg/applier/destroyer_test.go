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

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
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
	deploymentObj := newDeploymentObj()
	deploymentObj.SetName("deployment-1")
	deploymentID := object.UnstructuredToObjMetadata(deploymentObj)

	deployment2Obj := newDeploymentObj()
	deployment2Obj.SetName("deployment-2")
	deployment2ID := object.UnstructuredToObjMetadata(deployment2Obj)

	testObj := newTestObj("test-1")
	testID := object.UnstructuredToObjMetadata(testObj)

	testObj2 := newTestObj("test-2")

	namespaceObj := fake.UnstructuredObject(kinds.Namespace(),
		core.Name("test-namespace"))
	namespaceID := object.UnstructuredToObjMetadata(namespaceObj)

	uid := core.ID{
		GroupKind: live.ResourceGroupGVK.GroupKind(),
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
		name     string
		events   []event.Event
		multiErr error
	}{
		{
			name: "unknown type for some resource",
			events: []event.Event{
				formDeleteEvent(event.DeleteFailed, testObj, applyerror.NewUnknownTypeError(testError1)),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			multiErr: DeleteErrorForResource(testError1, idFrom(testID)),
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formDeleteSkipEvent(testID, testObj.DeepCopy(), &inventory.PolicyPreventedActuationError{
					Strategy: actuation.ActuationStrategyDelete,
					Policy:   inventory.PolicyMustMatch,
					Status:   inventory.NoMatch,
				}),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			// Prunes and Deletes ignore PolicyPreventedActuationErrors.
			// This allows abandoning of managed objects.
			multiErr: nil,
		},
		{
			name: "inventory object is too large",
			events: []event.Event{
				formErrorEvent(etcdError),
			},
			multiErr: largeResourceGroupError(etcdError, uid),
		},
		{
			name: "failed to delete",
			events: []event.Event{
				formDeleteEvent(event.DeleteFailed, testObj, testError1),
				formDeleteEvent(event.DeletePending, testObj2, nil),
			},
			multiErr: DeleteErrorForResource(testError1, idFrom(testID)),
		},
		{
			name: "skipped delete",
			events: []event.Event{
				formDeleteEvent(event.DeleteSuccessful, testObj, nil),
				formDeleteEvent(event.DeleteSkipped, namespaceObj, &filter.NamespaceInUseError{
					Namespace: "test-namespace",
				}),
				formDeleteEvent(event.DeleteSuccessful, testObj2, nil),
			},
			multiErr: SkipErrorForResource(
				errors.New("namespace still in use: test-namespace"),
				idFrom(namespaceID),
				actuation.ActuationStrategyDelete),
		},
		{
			name: "all passed",
			events: []event.Event{
				formDeleteEvent(event.DeletePending, testObj2, nil),
				formDeleteEvent(event.DeleteSuccessful, testObj, nil),
				formDeleteEvent(event.DeleteSuccessful, deploymentObj, nil),
			},
		},
		{
			name: "all failed",
			events: []event.Event{
				formDeleteEvent(event.DeletePending, testObj2, nil),
				formDeleteEvent(event.DeleteFailed, testObj, testError1),
				formDeleteEvent(event.DeleteFailed, deploymentObj, testError2),
			},
			multiErr: status.Wrap(
				DeleteErrorForResource(testError1, idFrom(testID)),
				DeleteErrorForResource(testError2, idFrom(deploymentID))),
		},
		{
			name: "failed dependency during delete",
			events: []event.Event{
				formDeleteSkipEventWithDependent(deploymentObj.DeepCopy(), deployment2Obj.DeepCopy()),
			},
			multiErr: SkipErrorForResource(
				&filter.DependencyPreventedActuationError{
					Object:                  deploymentID,
					Strategy:                actuation.ActuationStrategyDelete,
					Relationship:            filter.RelationshipDependent,
					Relation:                deployment2ID,
					RelationPhase:           filter.PhaseReconcile,
					RelationActuationStatus: actuation.ActuationSucceeded,
					RelationReconcileStatus: actuation.ReconcileTimeout,
				},
				idFrom(deploymentID),
				actuation.ActuationStrategyDelete),
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
			destroyer, err := NewNamespaceSupervisor(cs, "test-namespace", "rs", 5*time.Minute)
			require.NoError(t, err)

			errs := destroyer.Destroy(context.Background())
			testerrors.AssertEqual(t, tc.multiErr, errs)
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
