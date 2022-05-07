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
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/dependson"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeApplier struct {
	initErr error
	events  []event.Event
}

func newFakeApplier(err error, events []event.Event) *fakeApplier {
	return &fakeApplier{
		initErr: err,
		events:  events,
	}
}

func (a *fakeApplier) Run(_ context.Context, _ inventory.Info, _ object.UnstructuredSet, _ apply.ApplierOptions) <-chan event.Event {
	events := make(chan event.Event, len(a.events))
	go func() {
		for _, e := range a.events {
			events <- e
		}
		close(events)
	}()
	return events
}

func TestSync(t *testing.T) {
	deploymentObj := newDeploymentObj()
	deploymentID := object.UnstructuredToObjMetadata(deploymentObj)

	testObj := newTestObj()
	testID := object.UnstructuredToObjMetadata(testObj)
	testGVK := testObj.GroupVersionKind()

	objs := []client.Object{deploymentObj, testObj}

	namespaceID := &object.ObjMetadata{
		Name:      "test-namespace",
		GroupKind: kinds.Namespace().GroupKind(),
	}

	uid := core.ID{
		GroupKind: live.ResourceGroupGVK.GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      "rs",
			Namespace: "test-namespace",
		},
	}

	testcases := []struct {
		name     string
		initErr  error
		events   []event.Event
		multiErr status.MultiError
		gvks     map[schema.GroupVersionKind]struct{}
	}{
		{
			name:     "applier init error",
			initErr:  errors.New("init error"),
			multiErr: Error(errors.New("init error")),
		},
		{
			name: "unknown type for some resource",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, &testID, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyPending, nil, nil),
			},
			multiErr: ErrorForResource(errors.New("unknown type"), idFrom(testID)),
			gvks:     map[schema.GroupVersionKind]struct{}{kinds.Deployment(): {}},
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formApplySkipEvent(testID, testObj.DeepCopy(), &inventory.PolicyPreventedActuationError{
					Strategy: actuation.ActuationStrategyApply,
					Policy:   inventory.PolicyMustMatch,
					Status:   inventory.NoMatch,
				}),
				formApplyEvent(event.ApplyPending, nil, nil),
			},
			multiErr: KptManagementConflictError(testObj),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "inventory object is too large",
			events: []event.Event{
				formErrorEvent("etcdserver: request is too large"),
			},
			multiErr: largeResourceGroupError(fmt.Errorf("etcdserver: request is too large"), uid),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "failed to apply",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, &testID, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, nil, nil),
			},
			multiErr: ErrorForResource(errors.New("failed apply"), idFrom(testID)),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "failed to prune",
			events: []event.Event{
				formPruneEvent(event.PruneFailed, &testID, errors.New("failed pruning")),
				formPruneEvent(event.PruneSuccessful, nil, nil),
			},
			multiErr: ErrorForResource(errors.New("failed pruning"), idFrom(testID)),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "skipped pruning",
			events: []event.Event{
				formPruneEvent(event.PruneSuccessful, &testID, nil),
				formPruneEvent(event.PruneSkipped, namespaceID, &filter.NamespaceInUseError{
					Namespace: "test-namespace",
				}),
				formPruneEvent(event.PruneSuccessful, nil, nil),
			},
			multiErr: SkipErrorForResource(
				errors.New("namespace still in use: test-namespace"),
				idFrom(*namespaceID),
				actuation.ActuationStrategyDelete),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "all passed",
			events: []event.Event{
				formApplyEvent(event.ApplySuccessful, &testID, nil),
				formApplyEvent(event.ApplySuccessful, &deploymentID, nil),
				formApplyEvent(event.ApplyPending, nil, nil),
				formPruneEvent(event.PruneSuccessful, nil, nil),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
		{
			name: "all failed",
			events: []event.Event{
				formApplyEvent(event.ApplyFailed, &testID, applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(event.ApplyFailed, &deploymentID, applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(event.ApplyPending, nil, nil),
				formPruneEvent(event.PruneSuccessful, nil, nil),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
			},
			multiErr: status.Append(ErrorForResource(errors.New("unknown type"), idFrom(testID)), ErrorForResource(errors.New("failed apply"), idFrom(deploymentID))),
		},
		{
			name: "failed dependency during apply",
			events: []event.Event{
				formApplySkipEventWithDependency(deploymentID, deploymentObj.DeepCopy()),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
			multiErr: status.Append(SkipErrorForResource(
				errors.New("dependency apply reconcile timeout: namespace_name_group_kind"),
				idFrom(deploymentID),
				actuation.ActuationStrategyApply),
				nil),
		},
		{
			name: "failed dependency during prune",
			events: []event.Event{
				formPruneSkipEventWithDependency(deploymentID),
			},
			multiErr: SkipErrorForResource(
				errors.New("dependent delete actuation failed: namespace_name_group_kind"),
				idFrom(deploymentID),
				actuation.ActuationStrategyDelete),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				testGVK:            {},
			},
		},
	}

	for _, tc := range testcases {
		u := &unstructured.Unstructured{}
		u.SetNamespace("test-namespace")
		u.SetName("rs")
		fakeClient := testingfake.NewClient(t, runtime.NewScheme(), u)
		cfg := &rest.Config{} // unused by test applier
		applierFunc := func(c client.Client, _ *rest.Config, _ string) (*clientSet, error) {
			return &clientSet{
				kptApplier: newFakeApplier(tc.initErr, tc.events),
				client:     fakeClient,
			}, tc.initErr
		}

		var errs status.MultiError
		applier, err := NewNamespaceApplier(fakeClient, cfg, "test-namespace", "rs", "", 5*time.Minute)
		if err != nil {
			errs = Error(err)
		} else {
			applier.clientSetFunc = applierFunc
			var gvks map[schema.GroupVersionKind]struct{}
			gvks, errs = applier.sync(context.Background(), objs)
			if diff := cmp.Diff(tc.gvks, gvks, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("%s: Diff of GVK map from Apply(): %s", tc.name, diff)
			}
		}
		if tc.multiErr == nil {
			if errs != nil {
				t.Errorf("%s: unexpected error %v", tc.name, errs)
			}
		} else if errs == nil {
			t.Errorf("%s: expected some error, but not happened", tc.name)
		} else {
			actualErrs := errs.Errors()
			expectedErrs := tc.multiErr.Errors()
			if len(actualErrs) != len(expectedErrs) {
				t.Errorf("%s: number of error is not as expected %v", tc.name, actualErrs)
			} else {
				for i, actual := range actualErrs {
					expected := expectedErrs[i]
					if !strings.Contains(actual.Error(), expected.Error()) || reflect.TypeOf(expected) != reflect.TypeOf(actual) {
						t.Errorf("%s:\nexpected error:\n%v\nbut got:\n%v", tc.name,
							indent(expected.Error(), 1),
							indent(actual.Error(), 1))
					}
				}
			}
		}
	}
}

func formApplyEvent(status event.ApplyEventStatus, id *object.ObjMetadata, err error) event.Event {
	e := event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Status: status,
			Error:  err,
		},
	}
	if id != nil {
		e.ApplyEvent.Identifier = *id
		e.ApplyEvent.Resource = &unstructured.Unstructured{}
	}
	return e
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

func formPruneEvent(status event.PruneEventStatus, id *object.ObjMetadata, err error) event.Event {
	e := event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			Error:  err,
			Status: status,
		},
	}
	if id != nil {
		e.PruneEvent.Identifier = *id
		e.PruneEvent.Object = &unstructured.Unstructured{}
	}
	return e
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

func formErrorEvent(s string) event.Event {
	e := event.Event{
		Type: event.ErrorType,
		ErrorEvent: event.ErrorEvent{
			Err: fmt.Errorf("%s", s),
		},
	}
	return e
}

func TestProcessWaitEvent(t *testing.T) {
	deploymentID := object.UnstructuredToObjMetadata(newDeploymentObj())
	testID := object.UnstructuredToObjMetadata(newTestObj())

	status := newApplyStats()
	processWaitEvent(formWaitEvent(event.ReconcileFailed, &deploymentID).WaitEvent, status.objsReconciled)
	processWaitEvent(formWaitEvent(event.ReconcileSuccessful, &testID).WaitEvent, status.objsReconciled)
	if len(status.objsReconciled) != 1 {
		t.Fatalf("expected %d object to be recocniled but got %d", 1, len(status.objsReconciled))
	}
}

func indent(in string, indentation uint) string {
	indent := strings.Repeat("\t", int(indentation))
	lines := strings.Split(in, "\n")
	return indent + strings.Join(lines, fmt.Sprintf("\n%s", indent))
}

func newDeploymentObj() *unstructured.Unstructured {
	return fake.UnstructuredObject(kinds.Deployment(),
		core.Namespace("test-namespace"), core.Name("random-name"))
}

func newTestObj() *unstructured.Unstructured {
	return fake.UnstructuredObject(schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}, core.Namespace("test-namespace"), core.Name("random-name"))
}
