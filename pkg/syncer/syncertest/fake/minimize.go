// Copyright 2023 Google LLC
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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MinimizeUnstructured removes empty values, outward-in.
// Empty lists, empty maps, and nil values are considered empty, and removed.
// The object is modified in-place.
// Maps are modified in-place, but lists may or may not be replaced.
func MinimizeUnstructured(uObj *unstructured.Unstructured) {
	if len(uObj.Object) == 0 {
		return
	}
	uObj.Object = minimizeMap(uObj.Object)
}

// minimizeMap removes empty values, outward-in.
// Empty lists, empty maps, and nil values are considered empty, and removed.
// If the map itself is empty, before or after minimizing its values, it will
// be replaced with nil.
func minimizeMap(objMap map[string]interface{}) map[string]interface{} {
	if len(objMap) == 0 {
		return nil
	}
	// Yes, it's safe to remove entries from a map while iterating its entries.
	// Entries will not be skipped.
	for k, v := range objMap {
		switch typedV := v.(type) {
		case nil:
			// This case only catches non-typed nils.
			// https://glucn.medium.com/golang-an-interface-holding-a-nil-value-is-not-nil-bb151f472cc7
			delete(objMap, k)
		case map[string]interface{}:
			typedV = minimizeMap(typedV)
			if typedV != nil {
				objMap[k] = typedV
			} else {
				delete(objMap, k)
			}
		case []interface{}:
			typedV = minimizeList(typedV)
			if typedV != nil {
				objMap[k] = typedV
			} else {
				delete(objMap, k)
			}
		}
	}
	if len(objMap) == 0 {
		return nil
	}
	return objMap
}

// minimizeList removes empty entries, outward-in.
// Empty lists, empty maps, and nil values are considered empty, and removed.
// If the list itself is empty, before or after minimizing its entries, it will
// be replaced with nil.
func minimizeList(objList []interface{}) []interface{} {
	if len(objList) == 0 {
		return nil
	}
	// It's not safe to remove entries from a slice while iterating its entries.
	// So iterate with a manually managed index instead.
	// Reduce the index after deleting an entry to avoid skipping entries.
	for i := 0; i < len(objList); i++ {
		v := objList[i]
		switch typedV := v.(type) {
		case nil:
			// This case only catches non-typed nils.
			// https://glucn.medium.com/golang-an-interface-holding-a-nil-value-is-not-nil-bb151f472cc7
			objList = append(objList[:i], objList[i+1:]...)
			i--
		case map[string]interface{}:
			typedV = minimizeMap(typedV)
			if typedV != nil {
				objList[i] = typedV
			} else {
				objList = append(objList[:i], objList[i+1:]...)
				i--
			}
		case []interface{}:
			typedV = minimizeList(typedV)
			if typedV != nil {
				objList[i] = typedV
			} else {
				objList = append(objList[:i], objList[i+1:]...)
				i--
			}
		}
	}
	if len(objList) == 0 {
		return nil
	}
	return objList
}
