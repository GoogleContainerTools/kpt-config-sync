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
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/object/mutation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func partitionObjs(objs []client.Object) ([]client.Object, []client.Object) {
	var enabled []client.Object
	var disabled []client.Object
	for _, obj := range objs {
		// i.e. managed by Config Sync
		if obj.GetAnnotations()[metadata.ResourceManagementKey] == metadata.ResourceManagementDisabled {
			disabled = append(disabled, obj)
		} else {
			enabled = append(enabled, obj)
		}
	}
	return enabled, disabled
}

// handleIgnoredObjects gets the cached cluster state of all mutation-ignored objects that are declared and applies the CS metadata on top of them
// prior to sending them to the applier
// Returns all objects that will be applied
func handleIgnoredObjects(enabled []client.Object, resources *declared.Resources) []client.Object {
	var allObjs []client.Object

	for _, dObj := range enabled {
		cachedObj, found := resources.GetIgnored(core.IDOf(dObj))
		_, deleted := cachedObj.(*queue.Deleted)

		if found && !deleted {
			metadata.UpdateConfigSyncMetadata(dObj, cachedObj)
			allObjs = append(allObjs, cachedObj)
		} else {
			allObjs = append(allObjs, dObj)
		}
	}

	return allObjs
}

func toUnstructured(objs []client.Object) ([]*unstructured.Unstructured, status.MultiError) {
	var errs status.MultiError
	var unstructureds []*unstructured.Unstructured
	for _, obj := range objs {
		u, err := syncerreconcile.AsUnstructuredSanitized(obj)
		if err != nil {
			// This should never happen.
			errs = status.Append(errs, status.InternalErrorBuilder.Wrap(err).
				Sprintf("converting %v to unstructured.Unstructured", core.IDOf(obj)).Build())
		}
		unstructureds = append(unstructureds, u)
	}
	return unstructureds, errs
}

// ObjMetaFromObject constructs an ObjMetadata representing the Object.
//
// Errors if the GroupKind is not set and not registered in core.Scheme.
func ObjMetaFromObject(obj client.Object, scheme *runtime.Scheme) (object.ObjMetadata, error) {
	gvk, err := kinds.Lookup(obj, scheme)
	if err != nil {
		return object.ObjMetadata{},
			fmt.Errorf("ObjMetaFromObject: failed to lookup GroupKind of %T: %w", obj, err)
	}
	return object.ObjMetadata{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		GroupKind: gvk.GroupKind(),
	}, nil
}

// ObjMetaFromUnstructured constructs an ObjMetadata representing the Object.
func ObjMetaFromUnstructured(obj *unstructured.Unstructured) object.ObjMetadata {
	return object.ObjMetadata{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		GroupKind: obj.GetObjectKind().GroupVersionKind().GroupKind(),
	}
}

func objMetaFromID(id core.ID) object.ObjMetadata {
	return object.ObjMetadata{
		Namespace: id.Namespace,
		Name:      id.Name,
		GroupKind: id.GroupKind,
	}
}

func idFrom(identifier object.ObjMetadata) core.ID {
	return core.ID{
		GroupKind: identifier.GroupKind,
		ObjectKey: client.ObjectKey{
			Name:      identifier.Name,
			Namespace: identifier.Namespace,
		},
	}
}

func coreIDFromInventoryInfo(rg *inventory.SingleObjectInfo) core.ID {
	return core.ID{
		GroupKind: v1alpha1.SchemeGroupVersionKind().GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      rg.GetName(),
			Namespace: rg.GetNamespace(),
		},
	}
}

func removeFrom(all []object.ObjMetadata, toRemove []client.Object) []object.ObjMetadata {
	m := map[object.ObjMetadata]bool{}
	for _, a := range all {
		m[a] = true
	}

	for _, r := range toRemove {
		meta := object.ObjMetadata{
			Namespace: r.GetNamespace(),
			Name:      r.GetName(),
			GroupKind: r.GetObjectKind().GroupVersionKind().GroupKind(),
		}
		delete(m, meta)
	}
	var results []object.ObjMetadata
	for key := range m {
		results = append(results, key)
	}
	return results
}

func getObjectSize(u *unstructured.Unstructured) (int, error) {
	data, err := json.Marshal(u)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func annotateStatusMode(ctx context.Context, c client.Client, u *unstructured.Unstructured, statusMode string) error {
	err := c.Get(ctx, client.ObjectKey{Name: u.GetName(), Namespace: u.GetNamespace()}, u)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	annotations := u.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[metadata.StatusModeKey] = statusMode
	u.SetAnnotations(annotations)
	return c.Update(ctx, u, client.FieldOwner(configsync.FieldManager))
}

func refsFromIDs(ids ...core.ID) []mutation.ResourceReference {
	if len(ids) == 0 {
		return nil
	}
	refs := make([]mutation.ResourceReference, len(ids))
	for i, id := range ids {
		refs[i] = mutation.ResourceReferenceFromObjMetadata(objMetaFromID(id))
	}
	return refs
}

func stringsFromRefs(refs ...mutation.ResourceReference) []string {
	if len(refs) == 0 {
		return nil
	}
	strs := make([]string, len(refs))
	for i, ref := range refs {
		strs[i] = ref.String()
	}
	return strs
}

const (
	commaSpaceDelimiter          = ", "
	commaEscapedNewlineDelimiter = ",\\n"
)

// joinIDs joins the object IDs (GKNN) using ResourceReference string format
// and the specified delimiter.
//
// ResourceReference string format is used because the normal ID string format,
// includes commas and spaces that make it harder to parse in a list delimited
// by commas.
//
// This can be used to build depends-on annotations when commaDelimiter is used,
// or for log messages with commaSpaceDelimiter or commaEscapedNewlineDelimiter.
func joinIDs(delimiter string, ids ...core.ID) string {
	if len(ids) == 0 {
		return ""
	}
	refs := refsFromIDs(ids...)
	refStrs := stringsFromRefs(refs...)
	sort.Strings(refStrs)
	return strings.Join(refStrs, delimiter)
}
