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

package helm

import (
	"fmt"
	"reflect"

	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// listConcatenate will combine yaml documents, with list elements
// concatenated. See unit tests for examples.
func listConcatenate(valuesToMerge [][]byte) ([]byte, error) {
	if len(valuesToMerge) == 0 {
		return nil, nil
	}

	result := valuesToMerge[0]
	for i := 1; i < len(valuesToMerge); i++ {
		var firstMap map[string]interface{}
		if err := yaml.Unmarshal(valuesToMerge[i], &firstMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal: %w", err)
		}
		var secondMap map[string]interface{}
		if err := yaml.Unmarshal(result, &secondMap); err != nil {
			return nil, fmt.Errorf("failed to unmarshal: %w", err)
		}
		r, err := listConcatenateTwo(firstMap, secondMap)
		if err != nil {
			return nil, fmt.Errorf("failed to merge: %w", err)
		}
		result, err = yaml.Marshal(r)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal: %w", err)
		}
	}
	return result, nil
}

// handles the nested nodes, ensuring that list elements are concatenated
// but maps are still overriden
func handleNested(first, second interface{}) (interface{}, error) {
	firstType := reflect.TypeOf(first).Kind()
	secondType := reflect.TypeOf(second).Kind()

	if firstType != secondType {
		return first, nil
	}

	switch firstType {
	case reflect.Slice:
		// slices get concatenated
		return concatenateSlices(first, second), nil
	case reflect.Map:
		firstMap := first.(map[string]interface{})
		secondMap := second.(map[string]interface{})
		for key, secondVal := range secondMap {
			firstVal, found := firstMap[key]
			if found {
				m, err := handleNested(firstVal, secondVal)
				if err != nil {
					return nil, err
				}
				// we are only concatenating lists, other types will just get
				// overriden
				mType := reflect.TypeOf(m).Kind()
				if mType == reflect.Slice {
					firstMap[key] = m
				}
			}
		}
	}
	return first, nil
}

// listConcatenate will combine just two yaml documents, with list elements
// concatenated. See unit tests for examples.
func listConcatenateTwo(first, second interface{}) (interface{}, error) {
	firstType := reflect.TypeOf(first).Kind()
	secondType := reflect.TypeOf(second).Kind()

	if firstType != secondType {
		return first, nil
	}

	switch firstType {
	case reflect.Slice:
		// slices get concatenated
		return concatenateSlices(first, second), nil
	case reflect.Map:
		firstMap := first.(map[string]interface{})
		secondMap := second.(map[string]interface{})
		for key, secondVal := range secondMap {
			firstVal, found := firstMap[key]
			if found {
				m, err := handleNested(firstVal, secondVal)
				if err != nil {
					return nil, err
				}
				firstMap[key] = m

			} else {
				firstMap[key] = secondVal
			}
		}
	}
	return first, nil
}

func interfaceToSlice(i interface{}) []interface{} {
	switch val := i.(type) {
	case []interface{}:
		return val
	default:
		return append([]interface{}{}, i)
	}
}

func concatenateSlices(first interface{}, second interface{}) []interface{} {
	firstSlice := interfaceToSlice(first)
	secondSlice := interfaceToSlice(second)
	return append(firstSlice, secondSlice...)
}
