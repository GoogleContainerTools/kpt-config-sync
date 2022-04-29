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
	"sigs.k8s.io/cli-utils/pkg/apply"
	applyerror "sigs.k8s.io/cli-utils/pkg/apply/error"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
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
	resources, cache := prepareResources()
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
				formApplyEvent(fakeID(), applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(nil, nil),
			},
			multiErr: ErrorForResource(errors.New("unknown type"), idFrom(*fakeID())),
			gvks:     map[schema.GroupVersionKind]struct{}{kinds.Deployment(): {}},
		},
		{
			name: "conflict error for some resource",
			events: []event.Event{
				formApplyEvent(fakeID(), inventory.NewInventoryOverlapError(errors.New("conflict"))),
				formApplyEvent(nil, nil),
			},
			multiErr: KptManagementConflictError(resources[1]),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
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
				fakeKind():         {},
			},
		},
		{
			name: "failed to apply",
			events: []event.Event{
				formApplyEvent(fakeID(), applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(nil, nil),
			},
			multiErr: ErrorForResource(errors.New("failed apply"), idFrom(*fakeID())),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
			},
		},
		{
			name: "failed to prune",
			events: []event.Event{
				formPruneEvent(event.Pruned, fakeID(), errors.New("failed pruning")),
				formPruneEvent(event.Pruned, nil, nil),
			},
			multiErr: ErrorForResource(errors.New("failed pruning"), idFrom(*fakeID())),
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
			},
		},
		{
			name: "skipped pruning",
			events: []event.Event{
				formPruneEvent(event.Pruned, fakeID(), nil),
				formPruneEvent(event.PruneSkipped, deploymentID(), nil),
				formPruneEvent(event.Pruned, nil, nil),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
			},
		},
		{
			name: "all passed",
			events: []event.Event{
				formApplyEvent(fakeID(), nil),
				formApplyEvent(deploymentID(), nil),
				formApplyEvent(nil, nil),
				formPruneEvent(event.Pruned, nil, nil),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
			},
		},
		{
			name: "all failed",
			events: []event.Event{
				formApplyEvent(fakeID(), applyerror.NewUnknownTypeError(errors.New("unknown type"))),
				formApplyEvent(deploymentID(), applyerror.NewApplyRunError(errors.New("failed apply"))),
				formApplyEvent(nil, nil),
				formPruneEvent(event.Pruned, nil, nil),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
			},
			multiErr: status.Append(ErrorForResource(errors.New("unknown type"), idFrom(*fakeID())), ErrorForResource(errors.New("failed apply"), idFrom(*deploymentID()))),
		},
		{
			name: "failed dependency during apply",
			events: []event.Event{
				formApplySkipEventWithDependency(deploymentID()),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Deployment(): {},
				fakeKind():         {},
			},
			multiErr: status.Append(ErrorForResource(errors.New("dependencies are not reconciled: [Kind, random/random]"), idFrom(*deploymentID())), nil),
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
			gvks, errs = applier.sync(context.Background(), resources, cache)
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
						t.Errorf("%s: expected error %v but got %v", tc.name, expected, actual)
					}
				}
			}
		}
	}
}

func prepareResources() ([]client.Object, map[core.ID]client.Object) {
	u1 := fake.UnstructuredObject(kinds.Deployment(), core.Namespace("test-namespace"), core.Name("random-name"))
	u2 := fake.UnstructuredObject(schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}, core.Namespace("test-namespace"), core.Name("random-name"))
	objs := []client.Object{u1, u2}
	cache := map[core.ID]client.Object{}
	for _, u := range objs {
		cache[core.IDOf(u)] = u
	}
	return objs, cache
}

func fakeID() *object.ObjMetadata {
	return &object.ObjMetadata{
		Namespace: "test-namespace",
		Name:      "random-name",
		GroupKind: schema.GroupKind{
			Group: "configsync.test",
			Kind:  "Test",
		},
	}
}

func deploymentID() *object.ObjMetadata {
	return &object.ObjMetadata{
		Namespace: "test-namespace",
		Name:      "random-name",
		GroupKind: kinds.Deployment().GroupKind(),
	}
}

func fakeKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "configsync.test",
		Version: "v1",
		Kind:    "Test",
	}
}

func formApplyEvent(id *object.ObjMetadata, err error) event.Event {
	e := event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Error: err,
		},
	}
	if id != nil {
		e.ApplyEvent.Identifier = *id
	}
	return e
}

func formApplySkipEventWithDependency(id *object.ObjMetadata) event.Event {
	u := &unstructured.Unstructured{}
	u.SetAnnotations(map[string]string{dependson.Annotation: "/namespaces/random/Kind/random"})
	e := event.Event{
		Type: event.ApplyType,
		ApplyEvent: event.ApplyEvent{
			Operation:  event.Unchanged,
			Identifier: *id,
			Resource:   u,
		},
	}
	return e
}

func formPruneEvent(op event.PruneEventOperation, id *object.ObjMetadata, err error) event.Event {
	e := event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			Error:     err,
			Operation: op,
		},
	}
	if id != nil {
		e.PruneEvent.Identifier = *id
	}
	return e
}

func formWaitEvent(op event.WaitEventOperation, id *object.ObjMetadata) event.Event {
	e := event.Event{
		Type: event.WaitType,
		WaitEvent: event.WaitEvent{
			Operation: op,
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
	status := newApplyStats()
	processWaitEvent(formWaitEvent(event.ReconcileFailed, deploymentID()).WaitEvent, status.objsReconciled)
	processWaitEvent(formWaitEvent(event.Reconciled, fakeID()).WaitEvent, status.objsReconciled)
	if len(status.objsReconciled) != 1 {
		t.Fatalf("expected %d object to be recocniled but got %d", 1, len(status.objsReconciled))
	}
}
