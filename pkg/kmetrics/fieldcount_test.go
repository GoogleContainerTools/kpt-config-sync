/*
Copyright 2021 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kmetrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKustomizeFieldUsage(t *testing.T) {
	testCases := map[string]struct {
		inputDir string
		flags    []string
		expected *KustomizeFieldMetrics
	}{
		"simple": {
			inputDir: "./testdata/simple",
			expected: &KustomizeFieldMetrics{
				FieldCount:         map[string]int{"Resources": 1},
				TopTierCount:       map[string]int{"Resources": 1},
				PatchCount:         map[string]int{},
				BaseCount:          map[string]int{},
				HelmMetrics:        map[string]int{},
				K8sMetadata:        map[string]int{},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"missing kustomization": {
			inputDir: "./testdata/missingkustomization",
			expected: nil,
		},
		"complex": {
			inputDir: "./testdata/complex",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Resources":             3,
					"NamePrefix":            3,
					"Patches":               2,
					"PatchesStrategicMerge": 1,
					"Transformers":          1,
				},
				TopTierCount: map[string]int{
					"Resources":    4,
					"Transformers": 1,
				},
				PatchCount: map[string]int{
					"Patches":               2,
					"PatchesStrategicMerge": 3,
				},
				BaseCount: map[string]int{
					"Local":  2,
					"Remote": 1,
				},
				HelmMetrics: map[string]int{},
				K8sMetadata: map[string]int{
					"NamePrefix": 3,
				},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"component": {
			inputDir: "./testdata/component",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Resources":  1,
					"Components": 1,
					"NamePrefix": 1,
					"Generators": 2,
				},
				TopTierCount: map[string]int{
					"Resources":  1,
					"Generators": 3,
				},
				PatchCount:  map[string]int{},
				BaseCount:   map[string]int{},
				HelmMetrics: map[string]int{},
				K8sMetadata: map[string]int{
					"NamePrefix": 1,
				},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"with generator": {
			inputDir: "./testdata/withgenerator",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Resources":          1,
					"ConfigMapGenerator": 1,
				},
				TopTierCount: map[string]int{
					"Resources":           2,
					"ConfigMapGenerators": 1,
				},
				PatchCount:         map[string]int{},
				BaseCount:          map[string]int{},
				HelmMetrics:        map[string]int{},
				K8sMetadata:        map[string]int{},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"with helm charts": {
			inputDir: "./testdata/withhelmcharts",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Generators":                  1,
					"HelmCharts":                  1,
					"HelmChartInflationGenerator": 1,
				},
				TopTierCount: map[string]int{
					"Generators": 2,
				},
				PatchCount: map[string]int{},
				BaseCount:  map[string]int{},
				HelmMetrics: map[string]int{
					"render-helm-chart":           2,
					"HelmChartInflationGenerator": 1,
					"HelmCharts":                  2,
				},
				K8sMetadata:        map[string]int{},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"k8s metadata": {
			inputDir: "./testdata/k8smetadata",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Resources":    2,
					"Namespace":    1,
					"NameSuffix":   1,
					"CommonLabels": 1,
					"Labels":       2,
				},
				TopTierCount: map[string]int{
					"Resources": 2,
				},
				PatchCount: map[string]int{},
				BaseCount: map[string]int{
					"Local": 1,
				},
				HelmMetrics: map[string]int{},
				K8sMetadata: map[string]int{
					"Namespace":    1,
					"NameSuffix":   1,
					"CommonLabels": 3,
					"Labels":       3,
				},
				SimplMetrics:       map[string]int{},
				DeprecationMetrics: map[string]int{},
			},
		},
		"simplification usage": {
			inputDir: "./testdata/simplificationUsage",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Resources":    1,
					"Replicas":     1,
					"Images":       1,
					"Replacements": 1,
				},
				TopTierCount: map[string]int{
					"Resources": 3,
				},
				PatchCount:  map[string]int{},
				BaseCount:   map[string]int{},
				HelmMetrics: map[string]int{},
				K8sMetadata: map[string]int{},
				SimplMetrics: map[string]int{
					"Replicas":           1,
					"Images":             4,
					"ReplacementSources": 2,
					"ReplacementTargets": 4,
				},
				DeprecationMetrics: map[string]int{},
			},
		},
		"deprecating fields": {
			inputDir: "./testdata/deprecatingfields",
			expected: &KustomizeFieldMetrics{
				FieldCount: map[string]int{
					"Bases": 1,
					"Vars":  1,
					"Crds":  1,
				},
				TopTierCount: map[string]int{},
				PatchCount:   map[string]int{},
				BaseCount:    map[string]int{},
				HelmMetrics:  map[string]int{},
				K8sMetadata:  map[string]int{},
				SimplMetrics: map[string]int{},
				DeprecationMetrics: map[string]int{
					"Bases": 2,
					"Vars":  3,
					"Crds":  2,
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			kt, err := readKustomizeFile(tc.inputDir)
			assert.NoError(t, err)
			result, err := kustomizeFieldUsage(kt, tc.inputDir)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestKustomizeResourcesGenerated(t *testing.T) {
	testCases := map[string]struct {
		input    string
		flags    []string
		expected int
	}{
		"simple": {
			input: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment-1
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment-2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-configmap
`,
			expected: 3,
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			result, err := kustomizeResourcesGenerated(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
