// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package taskrunner

import (
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Condition is a type that defines the types of conditions
// which a WaitTask can use.
type Condition string

const (
	// AllCurrent Condition means all the provided resources
	// has reached (and remains in) the Current status.
	AllCurrent Condition = "AllCurrent"

	// AllNotFound Condition means all the provided resources
	// has reached the NotFound status, i.e. they are all deleted
	// from the cluster.
	AllNotFound Condition = "AllNotFound"
)

// Meets returns true if the provided status meets the condition and
// false if it does not.
func (c Condition) Meets(s status.Status) bool {
	switch c {
	case AllCurrent:
		return s == status.CurrentStatus
	case AllNotFound:
		return s == status.NotFoundStatus
	default:
		return false
	}
}

// conditionMet tests whether the provided Condition holds true for
// all resources in the list, according to the ResourceCache.
// Resources in the cache older that the applied generation are non-matches.
func conditionMet(taskContext *TaskContext, ids object.ObjMetadataSet, c Condition) bool {
	switch c {
	case AllCurrent:
		return allMatchStatus(taskContext, ids, status.CurrentStatus)
	case AllNotFound:
		return allMatchStatus(taskContext, ids, status.NotFoundStatus)
	default:
		return noneMatchStatus(taskContext, ids, status.UnknownStatus)
	}
}

// allMatchStatus checks whether all of the resources provided have the provided status.
// Resources with older generations are considered non-matching.
func allMatchStatus(taskContext *TaskContext, ids object.ObjMetadataSet, s status.Status) bool {
	for _, id := range ids {
		cached := taskContext.ResourceCache().Get(id)
		if cached.Status != s {
			return false
		}

		applyGen, _ := taskContext.InventoryManager().AppliedGeneration(id) // generation at apply time
		cachedGen := int64(0)
		if cached.Resource != nil {
			cachedGen = cached.Resource.GetGeneration()
		}
		if cachedGen < applyGen {
			// cache too old
			return false
		}
	}
	return true
}

// allMatchStatus checks whether none of the resources provided have the provided status.
// Resources with older generations are considered matching.
func noneMatchStatus(taskContext *TaskContext, ids object.ObjMetadataSet, s status.Status) bool {
	for _, id := range ids {
		cached := taskContext.ResourceCache().Get(id)
		if cached.Status == s {
			return false
		}

		applyGen, _ := taskContext.InventoryManager().AppliedGeneration(id) // generation at apply time
		cachedGen := int64(0)
		if cached.Resource != nil {
			cachedGen = cached.Resource.GetGeneration()
		}
		if cachedGen < applyGen {
			// cache too old
			return false
		}
	}
	return true
}
