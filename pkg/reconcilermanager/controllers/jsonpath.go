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

package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/spyzhov/ajson"
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"
)

// deleteFields evaluates the JSONPath expression to delete fields from the input map.
func deleteFields(obj map[string]interface{}, expression string) error {
	// format input object as json for input into jsonpath library
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal input to json: %w", err)
	}
	// parse json into an ajson node
	root, err := ajson.Unmarshal(jsonBytes)
	if err != nil {
		return fmt.Errorf("failed to unmarshal input json: %w", err)
	}

	// find nodes that match the expression
	nodes, err := root.JSONPath(expression)
	if err != nil {
		return fmt.Errorf("failed to evaluate jsonpath expression (%s): %w", expression, err)
	}

	if len(nodes) == 0 {
		// zero nodes found, none updated
		return nil
	}
	// get value of all matching nodes
	for _, node := range nodes {
		if err = node.Delete(); err != nil {
			klog.Warningf("failed to delete a node: %v", err)
		}
	}

	jsonBytes, err = ajson.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal jsonpath result to json: %w", err)
	}
	// parse json back into the input map
	err = yaml.Unmarshal(jsonBytes, &obj)
	if err != nil {
		return fmt.Errorf("failed to unmarshal jsonpath delete result: %w", err)
	}
	return nil
}
