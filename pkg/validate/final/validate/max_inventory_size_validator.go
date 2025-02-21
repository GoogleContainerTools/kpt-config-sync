// Copyright 2025 Google LLC
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

package validate

import (
	"fmt"
	"math"

	"encoding/binary"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

const (
	placeholderUID         = types.UID("f81d4fae-7dec-11d0-a765-00a0c91e6bf6")
	placeholderGeneration  = int64(math.MaxInt64) - 2
	placeholderName63Chars = "this-is-a-kubernetes-object-name-that-is-very-long-indeed-and-it-has-63-characters-in-total-which-is-the-maximum-allowed-length-for-a-k8s-object-name"
)

// MaxInventorySize verifies that the number of managed resources does not
// generate an inventory object that is larger than the specified number of
// bytes when serialized to JSON.
func MaxInventorySize(maxBytes int) func([]ast.FileObject) status.MultiError {
	if maxBytes <= 0 {
		return noOpValidator
	}
	return func(objs []ast.FileObject) status.MultiError {
		invObj, err := simulateInventoryObject(objs)
		if err != nil {
			err := fmt.Errorf("simulating inventory object: %w", err)
			return status.InternalWrapf(err, "Failed to validate inventory size")
		}
		invBytes, err := invObj.MarshalJSON()
		if err != nil {
			err := fmt.Errorf("serializing simulated inventory object: %w", err)
			return status.InternalWrapf(err, "Failed to validate inventory size")
		}
		invByteSize := binary.Size(invBytes)
		if invByteSize > maxBytes {
			return system.MaxInventorySizeError(maxBytes, invByteSize)
		}
		return nil
	}
}

func simulateInventoryObject(objs []ast.FileObject) (*unstructured.Unstructured, error) {
	rgObj := &unstructured.Unstructured{}

	// Add placeholder metadata
	rgObj.SetName(placeholderName63Chars)
	rgObj.SetNamespace(placeholderName63Chars)
	rgObj.SetLabels(map[string]string{
		placeholderName63Chars: placeholderName63Chars,
	})
	rgObj.SetAnnotations(map[string]string{
		placeholderName63Chars: placeholderName63Chars,
	})
	rgObj.SetUID(placeholderUID)
	rgObj.SetGeneration(placeholderGeneration)

	// Add spec & status
	storage := live.WrapInventoryObj(rgObj)
	invSpec := make(object.ObjMetadataSet, len(objs))
	invStatus := make([]actuation.ObjectStatus, len(objs))
	for i, obj := range objs {
		invSpec[i] = object.ObjMetadata{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			GroupKind: obj.GroupVersionKind().GroupKind(),
		}
		invStatus[i] = actuation.ObjectStatus{
			ObjectReference: inventory.ObjectReferenceFromObjMetadata(invSpec[i]),
			Strategy:        actuation.ActuationStrategyDelete,
			Actuation:       actuation.ActuationSucceeded,
			Reconcile:       actuation.ReconcileSucceeded,
			UID:             placeholderUID,
			Generation:      placeholderGeneration,
		}
	}
	// Store only updates private fields on the storage object.
	// No API calls are made.
	if err := storage.Store(invSpec, invStatus); err != nil {
		return nil, fmt.Errorf("storing inventory: %w", err)
	}
	// GetObject only generates an Unstructured inventory from the initial
	// object with the stored spec & status merged into it.
	// No API calls are made.
	invObj, err := storage.GetObject()
	if err != nil {
		return nil, fmt.Errorf("getting inventory from storage: %w", err)
	}
	return invObj, nil
}
