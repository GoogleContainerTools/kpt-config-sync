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

package status

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type resourceState struct {
	Namespace  string      `json:"namespace"`
	Name       string      `json:"name"`
	Group      string      `json:"group,omitempty"`
	Kind       string      `json:"kind"`
	Status     string      `json:"status"`
	SourceHash string      `json:"sourceHash,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
}

// Condition is the for the resource status condition
type Condition struct {
	// type of the condition
	Type string `json:"type"`

	// status of the condition
	Status string `json:"status"`

	// one-word CamelCase reason for the condition’s last transition
	// +optional
	Reason string `json:"reason,omitempty"`

	// human-readable message indicating details about last transition
	// +optional
	Message string `json:"message,omitempty"`

	// last time the condition transit from one status to another
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

func (r resourceState) String() string {
	if r.Group == "" {
		return fmt.Sprintf("%s/%s", strings.ToLower(r.Kind), r.Name)
	}
	return fmt.Sprintf("%s.%s/%s", strings.ToLower(r.Kind), r.Group, r.Name)
}

// byNamespaceAndType implements sort.Interface:
// It first sort the resources by namespace, then sort them
// by type.
type byNamespaceAndType []resourceState

func (b byNamespaceAndType) Len() int {
	return len(b)
}

func (b byNamespaceAndType) Less(i, j int) bool {
	if b[i].Namespace < b[j].Namespace {
		return true
	}
	if b[i].Namespace > b[j].Namespace {
		return false
	}
	return b[i].String() < b[j].String()
}

func (b byNamespaceAndType) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func resourceLevelStatus(rg *unstructured.Unstructured) ([]resourceState, error) {
	if rg == nil {
		return nil, nil
	}
	rawStatus, found, err := unstructured.NestedSlice(rg.Object, "status", "resourceStatuses")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("ResourceGroup CR %s/%s doesn't contain resource status", rg.GetNamespace(), rg.GetName())
	}

	states := make([]resourceState, len(rawStatus))
	data, err := yaml.Marshal(rawStatus)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &states); err != nil {
		return nil, err
	}
	return checkConflict(states), nil
}

func checkConflict(states []resourceState) []resourceState {
	for i, s := range states {
		for _, c := range s.Conditions {
			if c.Type == "OwnershipOverlap" && c.Status == "True" {
				states[i].Status = "Conflict"
			}
		}
	}

	return states
}
