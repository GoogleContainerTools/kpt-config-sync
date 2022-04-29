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

package fake

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EventRecorder tracks the set of events emitted as part of a Kubernetes reconcile.
type EventRecorder struct {
	t      *testing.T
	events []Event
}

// NewEventRecorder initializes a new fake.EventRecorder.
func NewEventRecorder(t *testing.T) *EventRecorder {
	return &EventRecorder{t: t}
}

func (e *EventRecorder) event(object runtime.Object, eventtype, reason string) {
	o, ok := object.(client.Object)
	if !ok {
		e.t.Errorf("object is not a valid Kubernetes type %+v", object)
		return
	}
	e.events = append(e.events, Event{
		GroupVersionKind: o.GetObjectKind().GroupVersionKind(),
		ObjectKey:        client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()},
		EventType:        eventtype,
		Reason:           reason,
	})
}

// Event implements record.EventRecorder.
func (e *EventRecorder) Event(object runtime.Object, eventtype, reason, _ string) {
	e.event(object, eventtype, reason)
}

// Eventf implements record.EventRecorder.
func (e *EventRecorder) Eventf(object runtime.Object, eventtype, reason, _ string, _ ...interface{}) {
	e.event(object, eventtype, reason)
}

// PastEventf implements record.EventRecorder.
func (e *EventRecorder) PastEventf(object runtime.Object, _ v1.Time, eventtype, reason, _ string, _ ...interface{}) {
	e.event(object, eventtype, reason)
}

// AnnotatedEventf implements record.EventRecorder.
func (e *EventRecorder) AnnotatedEventf(object runtime.Object, _ map[string]string, eventtype, reason, _ string, _ ...interface{}) {
	e.event(object, eventtype, reason)
}

var _ record.EventRecorder = &EventRecorder{}

// Event represents a K8S Event that was emitted as result of the reconcile.
type Event struct {
	schema.GroupVersionKind
	client.ObjectKey
	EventType, Reason string
}

// NewEvent instantiates a new Event.
func NewEvent(o client.Object, eventtype, reason string) *Event {
	return &Event{
		GroupVersionKind: o.GetObjectKind().GroupVersionKind(),
		ObjectKey:        client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()},
		EventType:        eventtype,
		Reason:           reason,
	}
}

// Check ensures the EventRecorder got the correct set of updates to Syncs.
func (e *EventRecorder) Check(t *testing.T, wantEvents ...Event) {
	t.Helper()
	if diff := cmp.Diff(wantEvents, e.events, cmpopts.EquateEmpty()); diff != "" {
		t.Error("diff to EventRecorder.Event() calls", diff)
	}
}
