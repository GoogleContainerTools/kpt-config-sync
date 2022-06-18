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

// Package diff contains code for diffing sync-enabled resources, not
// necessarily known at compile time.
package diff

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/lifecycle"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Operation indicates what action should be taken if we detect a difference
// between declared configuration and the state of the cluster.
type Operation string

const (
	// NoOp indicates that no action should be taken.
	NoOp = Operation("no-op")

	// Create indicates the resource should be created.
	Create = Operation("create")

	// Update indicates the resource is declared and is on the API server, so we should
	// calculate a patch and apply it.
	Update = Operation("update")

	// Delete indicates the resource should be deleted.
	Delete = Operation("delete")

	// Error indicates the resource's management annotation in the API server is invalid.
	Error = Operation("error")

	// Unmanage indicates the resource's management annotation should be removed from the API Server.
	Unmanage = Operation("unmanage")

	// ManagementConflict represents the case where Declared and Actual both exist,
	// but the Actual one is managed by a Reconciler that supersedes this one.
	ManagementConflict = Operation("management-conflict")
)

// Diff is resource where Declared and Actual do not match.
// Both Declared and Actual are client.Object.
type Diff struct {
	// Declared is the resource as it exists in the repository.
	Declared client.Object
	// Actual is the resource as it exists in the cluster.
	Actual client.Object
}

// Operation returns the type of the difference between the repository and the API Server.
func (d Diff) Operation(ctx context.Context, scope declared.Scope, syncName string) Operation {
	switch {
	case d.Declared != nil && d.Actual == nil:
		// Create Branch.
		//
		// We have a declared resource, but nothing on the cluster. So we need to
		// figure out whether we want to create the resource.
		return d.createType()
	case d.Declared != nil && d.Actual != nil:
		// Update Branch.
		//
		// We have a declared and actual resource, so we need to figure out whether
		// we update the resource.
		return d.updateType(scope, syncName)
	case d.Declared == nil && d.Actual != nil:
		// Delete Branch.
		//
		// There is no declaration for the resource, but the resource exists on the
		// cluster, so we need to figure out whether we delete the resource.
		return d.deleteType(scope, syncName)
	default: // d.Declared == nil && d.Actual == nil:
		// Do nothing.
		//
		// We have a undeclared resource and nothing on the cluster.
		// This could mean that git changed after the delete was detected,
		// but it's more likely the delete was detected durring an apply run.
		return NoOp
	}
}

func (d Diff) createType() Operation {
	switch {
	case differ.ManagementEnabled(d.Declared):
		// Managed by ConfigSync and it doesn't exist, so create it.
		// For this case, we can also use `differ.ManagedByConfigSync`, since
		// the parser adds the `configsync.gke.io/resource-id` annotation to
		// all the resources in declared.Resources.
		return Create
	case differ.ManagementDisabled(d.Declared):
		// The resource doesn't exist but management is disabled, so take no action.
		return NoOp
	default:
		// There's an invalid management annotation, so there is an error in our logic.
		return Error
	}
}

func (d Diff) updateType(scope declared.Scope, syncName string) Operation {
	// We don't need to check for owner references here since that would be a
	// nomos vet error. Note that as-is, it is valid to declare something owned by
	// another object, possible causing (and being surfaced as) a resource fight.
	canManage := CanManage(scope, syncName, d.Actual, admissionv1.Update)
	switch {
	case differ.ManagementEnabled(d.Declared) && canManage:
		if d.Actual.GetAnnotations()[metadata.LifecycleMutationAnnotation] == metadata.IgnoreMutation &&
			d.Declared.GetAnnotations()[metadata.LifecycleMutationAnnotation] == metadata.IgnoreMutation {
			// The declared and actual object both have the lifecycle mutation
			// annotation set to ignore, so we should take no action as the user does
			// not want us to make changes to the object.
			//
			// If the annotation is on the actual object but not the one declared in
			// the repository, the update, which uses SSA, would not remove the annotation
			// from the actual object. However, Config Sync would not respect the annotation
			// on the actual object since the annotation is not declared in the git repository.
			//
			// If the annotation is on the declared object but not the actual one
			// on the cluster, we need to add it to the one in the cluster.
			return NoOp
		}
		return Update
	case differ.ManagementEnabled(d.Declared) && !canManage:
		// This reconciler can't manage this object but is erroneously being told to.
		return ManagementConflict
	case differ.ManagementDisabled(d.Declared) && canManage:
		if metadata.HasConfigSyncMetadata(d.Actual) {
			manager := d.Actual.GetAnnotations()[metadata.ResourceManagerKey]
			if manager == "" || manager == declared.ResourceManager(scope, syncName) {
				return Unmanage
			}
		}
		return NoOp
	case differ.ManagementDisabled(d.Declared) && !canManage:
		// Management is disabled and the object isn't owned by this reconciler, so
		// there's nothing to do.
		return NoOp
	default:
		return Error
	}
}

func (d Diff) deleteType(scope declared.Scope, syncName string) Operation {
	// Degenerate cases where we never want to take any action.
	switch {
	case !metadata.HasConfigSyncMetadata(d.Actual):
		// This object has no Nomos metadata, so there's nothing to do.
		return NoOp
	case len(d.Actual.GetOwnerReferences()) > 0:
		// This object is owned by something else.
		// It may be copying our metadata, so we don't have anything to check.
		return NoOp
	case !CanManage(scope, syncName, d.Actual, admissionv1.Delete):
		// We can't manage this and there isn't a competing declaration for it so,
		// nothing to do.
		return NoOp
	case !differ.ManagedByConfigSync(d.Actual):
		// d.Actual is not managed by Config Sync, so take no action.
		return NoOp
	}

	// Anything below here has Nomos metadata and is manageable by this reconciler,
	// and is managed by Config Sync.
	switch {
	case lifecycle.HasPreventDeletion(d.Actual):
		// We aren't supposed to delete this, so just unmanage it.
		// It is never valid to have ACM metadata on non-ACM-managed objects.
		return Unmanage
	case differ.IsManageableSystemNamespace(d.Actual):
		// This is a special Namespace we never want to remove.
		return Unmanage
	default:
		// The expected path. Delete the resource from the cluster.
		return Delete
	}
}

// UnstructuredActual returns the actual as an unstructured object.
func (d Diff) UnstructuredActual() (*unstructured.Unstructured, status.Error) {
	if d.Actual == nil {
		return nil, nil
	}
	// We just want Unstructured, NOT sanitized. Sanitized removes required fields
	// such as resourceVersion, which are required for updates.
	return reconcile.AsUnstructured(d.Actual)
}

// UnstructuredDeclared returns the declared as an unstructured object.
func (d Diff) UnstructuredDeclared() (*unstructured.Unstructured, status.Error) {
	if d.Declared == nil {
		return nil, nil
	}
	return reconcile.AsUnstructuredSanitized(d.Declared)
}

// ThreeWay does a three way diff and returns the FileObjectDiff list.
// Compare between previous declared and new declared to decide the delete list.
// Compare between the new declared and the actual states to decide the create and update.
func ThreeWay(newDeclared, previousDeclared, actual map[core.ID]client.Object) []Diff {
	var diffs []Diff
	// Delete.
	for coreID, previousDecl := range previousDeclared {
		if _, ok := newDeclared[coreID]; !ok {
			toDelete := Diff{
				Declared: nil,
				Actual:   previousDecl,
			}
			diffs = append(diffs, toDelete)
		}
	}
	// Create and Update.
	for coreID, newDecl := range newDeclared {
		if actual, ok := actual[coreID]; !ok {
			toCreate := Diff{
				Declared: newDecl,
				Actual:   nil,
			}
			diffs = append(diffs, toCreate)
		} else if IsUnknown(actual) {
			klog.Infof("Skipping diff for resource in unknown state: %s", coreID)
		} else {
			toUpdate := Diff{
				Declared: newDecl,
				Actual:   actual,
			}
			diffs = append(diffs, toUpdate)
		}
	}
	return diffs
}

// GetName returns the metadata.name of the object being considered.
func (d *Diff) GetName() string {
	if d.Declared != nil {
		return d.Declared.GetName()
	}
	if d.Actual != nil {
		return d.Actual.GetName()
	}
	// No object is being considered.
	return ""
}
