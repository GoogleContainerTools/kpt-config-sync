// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package jsonpath

import (
	"encoding/json"
	"fmt"

	// Using gopkg.in/yaml.v3 instead of sigs.k8s.io/yaml on purpose.
	// yaml.v3 correctly parses ints:
	// https://github.com/kubernetes-sigs/yaml/issues/45
	// yaml.v3 Node is also used as input to yqlib.
	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"

	"github.com/spyzhov/ajson"
)

// Get evaluates the JSONPath expression to extract values from the input map.
// Returns the node values that were found (zero or more), or an error.
// For details about the JSONPath expression language, see:
// https://goessner.net/articles/JsonPath/
func Get(obj map[string]interface{}, expression string) ([]interface{}, error) {
	// format input object as json for input into jsonpath library
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input to json: %w", err)
	}

	klog.V(7).Info("jsonpath.Get input as json:\n%s", jsonBytes)

	// parse json into an ajson node
	root, err := ajson.Unmarshal(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal input json: %w", err)
	}

	// find nodes that match the expression
	nodes, err := root.JSONPath(expression)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate jsonpath expression (%s): %w", expression, err)
	}

	result := make([]interface{}, len(nodes))

	// get value of all matching nodes
	for i, node := range nodes {
		// format node value as json
		jsonBytes, err = ajson.Marshal(node)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal jsonpath result to json: %w", err)
		}

		klog.V(7).Info("jsonpath.Get output as json:\n%s", jsonBytes)

		// parse json back into a Go primitive
		var value interface{}
		err = yaml.Unmarshal(jsonBytes, &value)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal jsonpath result: %w", err)
		}
		result[i] = value
	}

	return result, nil
}

// Set evaluates the JSONPath expression to set a value in the input map.
// Returns the number of matching nodes that were updated, or an error.
// For details about the JSONPath expression language, see:
// https://goessner.net/articles/JsonPath/
func Set(obj map[string]interface{}, expression string, value interface{}) (int, error) {
	// format input object as json for input into jsonpath library
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal input to json: %w", err)
	}

	klog.V(7).Info("jsonpath.Set input as json:\n%s", jsonBytes)

	// parse json into an ajson node
	root, err := ajson.Unmarshal(jsonBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal input json: %w", err)
	}

	// retrieve nodes that match the expression
	nodes, err := root.JSONPath(expression)
	if err != nil {
		return 0, fmt.Errorf("failed to evaluate jsonpath expression (%s): %w", expression, err)
	}
	if len(nodes) == 0 {
		// zero nodes found, none updated
		return 0, nil
	}

	// set value of all matching nodes
	for _, node := range nodes {
		switch typedValue := value.(type) {
		case bool:
			err = node.SetBool(typedValue)
		case string:
			err = node.SetString(typedValue)
		case int:
			err = node.SetNumeric(float64(typedValue))
		case float64:
			err = node.SetNumeric(typedValue)
		case []interface{}:
			var arrayValue []*ajson.Node
			arrayValue, err = toArrayOfNodes(typedValue)
			if err != nil {
				break
			}
			err = node.SetArray(arrayValue)
		case map[string]interface{}:
			var mapValue map[string]*ajson.Node
			mapValue, err = toMapOfNodes(typedValue)
			if err != nil {
				break
			}
			err = node.SetObject(mapValue)
		default:
			if value == nil {
				err = node.SetNull()
			} else {
				err = fmt.Errorf("unsupported value type: %T", value)
			}
		}
		if err != nil {
			return 0, err
		}
	}

	// format into an ajson node
	jsonBytes, err = ajson.Marshal(root)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal jsonpath result to json: %w", err)
	}

	klog.V(7).Info("jsonpath.Set output as json:\n%s", jsonBytes)

	// parse json back into the input map
	err = yaml.Unmarshal(jsonBytes, &obj)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal jsonpath result: %w", err)
	}

	return len(nodes), nil
}

func toArrayOfNodes(obj []interface{}) ([]*ajson.Node, error) {
	out := make([]*ajson.Node, len(obj))
	for index, value := range obj {
		// format input object as json for input into jsonpath library
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal array element to json: %w", err)
		}

		// parse json into an ajson node
		node, err := ajson.Unmarshal(jsonBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal array element: %w", err)
		}
		out[index] = node
	}
	return out, nil
}

func toMapOfNodes(obj map[string]interface{}) (map[string]*ajson.Node, error) {
	out := make(map[string]*ajson.Node, len(obj))
	for key, value := range obj {
		// format input object as json for input into jsonpath library
		jsonBytes, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal map value to json: %w", err)
		}

		// parse json into an ajson node
		node, err := ajson.Unmarshal(jsonBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal map value: %w", err)
		}
		out[key] = node
	}
	return out, nil
}
