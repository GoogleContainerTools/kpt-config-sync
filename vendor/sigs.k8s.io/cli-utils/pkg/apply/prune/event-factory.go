// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//

package prune

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// EventFactory is an abstract interface describing functions to generate
// events for pruning or deleting.
type EventFactory interface {
	CreateSuccessEvent(obj *unstructured.Unstructured) event.Event
	CreateSkippedEvent(obj *unstructured.Unstructured, reason string) event.Event
	CreateFailedEvent(id object.ObjMetadata, err error) event.Event
}

// CreateEventFactory returns the correct concrete version of
// an EventFactory based on the passed boolean.
func CreateEventFactory(isDelete bool, groupName string) EventFactory {
	if isDelete {
		return DeleteEventFactory{
			groupName: groupName,
		}
	}
	return PruneEventFactory{
		groupName: groupName,
	}
}

// PruneEventFactory implements EventFactory interface as a concrete
// representation of for prune events.
//nolint:revive // stuttering ok because Prune is a type of PruneEvent
type PruneEventFactory struct {
	groupName string
}

func (pef PruneEventFactory) CreateSuccessEvent(obj *unstructured.Unstructured) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			GroupName:  pef.groupName,
			Operation:  event.Pruned,
			Object:     obj,
			Identifier: object.UnstructuredToObjMetadata(obj),
		},
	}
}

func (pef PruneEventFactory) CreateSkippedEvent(obj *unstructured.Unstructured, reason string) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			GroupName:  pef.groupName,
			Operation:  event.PruneSkipped,
			Object:     obj,
			Identifier: object.UnstructuredToObjMetadata(obj),
			Reason:     reason,
		},
	}
}

func (pef PruneEventFactory) CreateFailedEvent(id object.ObjMetadata, err error) event.Event {
	return event.Event{
		Type: event.PruneType,
		PruneEvent: event.PruneEvent{
			GroupName:  pef.groupName,
			Identifier: id,
			Error:      err,
		},
	}
}

// DeleteEventFactory implements EventFactory interface as a concrete
// representation of for delete events.
type DeleteEventFactory struct {
	groupName string
}

func (def DeleteEventFactory) CreateSuccessEvent(obj *unstructured.Unstructured) event.Event {
	return event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			GroupName:  def.groupName,
			Operation:  event.Deleted,
			Object:     obj,
			Identifier: object.UnstructuredToObjMetadata(obj),
		},
	}
}

func (def DeleteEventFactory) CreateSkippedEvent(obj *unstructured.Unstructured, reason string) event.Event {
	return event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			GroupName:  def.groupName,
			Operation:  event.DeleteSkipped,
			Object:     obj,
			Identifier: object.UnstructuredToObjMetadata(obj),
			Reason:     reason,
		},
	}
}

func (def DeleteEventFactory) CreateFailedEvent(id object.ObjMetadata, err error) event.Event {
	return event.Event{
		Type: event.DeleteType,
		DeleteEvent: event.DeleteEvent{
			GroupName:  def.groupName,
			Identifier: id,
			Error:      err,
		},
	}
}
