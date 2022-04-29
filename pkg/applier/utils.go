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
	"encoding/json"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func partitionObjs(objs []client.Object) ([]client.Object, []client.Object) {
	var enabled []client.Object
	var disabled []client.Object
	for _, obj := range objs {
		if obj.GetAnnotations()[metadata.ResourceManagementKey] == metadata.ResourceManagementDisabled {
			disabled = append(disabled, obj)
		} else {
			enabled = append(enabled, obj)
		}
	}
	return enabled, disabled
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

func objMetaFrom(obj client.Object) object.ObjMetadata {
	return object.ObjMetadata{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
		GroupKind: obj.GetObjectKind().GroupVersionKind().GroupKind(),
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

func idFromInventory(rg *live.InventoryResourceGroup) core.ID {
	return core.ID{
		GroupKind: live.ResourceGroupGVK.GroupKind(),
		ObjectKey: client.ObjectKey{
			Name:      rg.Name(),
			Namespace: rg.Namespace(),
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
	annotations[StatusModeKey] = statusMode
	u.SetAnnotations(annotations)
	return c.Update(ctx, u)
}
