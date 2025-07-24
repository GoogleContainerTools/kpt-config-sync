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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFixArgoRolloutObservedGeneration(t *testing.T) {
	tests := []struct {
		name          string
		obj           *unstructured.Unstructured
		expectedValue interface{}
		shouldBeFixed bool
	}{
		{
			name: "ArgoCD Rollout with string observedGeneration should be fixed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":      "test-rollout",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"observedGeneration": "2",
					},
				},
			},
			expectedValue: int64(2),
			shouldBeFixed: true,
		},
		{
			name: "ArgoCD Rollout with int64 observedGeneration should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":      "test-rollout",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"observedGeneration": int64(3),
					},
				},
			},
			expectedValue: int64(3),
			shouldBeFixed: false,
		},
		{
			name: "ArgoCD Rollout with invalid string observedGeneration should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":      "test-rollout",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"observedGeneration": "invalid",
					},
				},
			},
			expectedValue: "invalid",
			shouldBeFixed: false,
		},
		{
			name: "Non-ArgoCD Rollout should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":      "test-deployment",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"observedGeneration": "1",
					},
				},
			},
			expectedValue: "1",
			shouldBeFixed: false,
		},
		{
			name: "ArgoCD resource that is not a Rollout should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       "Application",
					"metadata": map[string]interface{}{
						"name":      "test-app",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"observedGeneration": "1",
					},
				},
			},
			expectedValue: "1",
			shouldBeFixed: false,
		},
		{
			name: "ArgoCD Rollout without observedGeneration should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":      "test-rollout",
						"namespace": "test-ns",
					},
					"status": map[string]interface{}{
						"phase": "Progressing",
					},
				},
			},
			expectedValue: nil,
			shouldBeFixed: false,
		},
		{
			name: "ArgoCD Rollout without status should not be changed",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":      "test-rollout",
						"namespace": "test-ns",
					},
				},
			},
			expectedValue: nil,
			shouldBeFixed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original value for comparison
			var originalValue interface{}
			if status, found := tt.obj.Object[StatusField]; found {
				if statusMap, ok := status.(map[string]interface{}); ok {
					originalValue = statusMap[ObservedGenerationField]
				}
			}

			// Apply the fix
			fixArgoRolloutObservedGeneration(tt.obj)

			// Check the result
			var actualValue interface{}
			if status, found := tt.obj.Object[StatusField]; found {
				if statusMap, ok := status.(map[string]interface{}); ok {
					actualValue = statusMap[ObservedGenerationField]
				}
			}

			if tt.shouldBeFixed {
				assert.Equal(t, tt.expectedValue, actualValue, "observedGeneration should be converted to int64")
				assert.NotEqual(t, originalValue, actualValue, "value should have been changed")
			} else {
				assert.Equal(t, tt.expectedValue, actualValue, "observedGeneration should remain unchanged")
			}
		})
	}
}

func TestComputeStatusWithArgoRollout(t *testing.T) {
	tests := []struct {
		name          string
		obj           *unstructured.Unstructured
		expectSuccess bool
	}{
		{
			name: "ArgoCD Rollout with string observedGeneration should compute status successfully",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":       "test-rollout",
						"namespace":  "test-ns",
						"generation": int64(2),
					},
					"status": map[string]interface{}{
						"observedGeneration": "2",
						"phase":              "Progressing",
					},
				},
			},
			expectSuccess: true,
		},
		{
			name: "ArgoCD Rollout with int64 observedGeneration should compute status successfully",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": ArgoRolloutAPIVersion,
					"kind":       ArgoRolloutKind,
					"metadata": map[string]interface{}{
						"name":       "test-rollout",
						"namespace":  "test-ns",
						"generation": int64(3),
					},
					"status": map[string]interface{}{
						"observedGeneration": int64(3),
						"phase":              "Progressing",
					},
				},
			},
			expectSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute status
			result := ComputeStatus(tt.obj)

			// Verify the result
			require.NotNil(t, result, "ComputeStatus should return a result")

			if tt.expectSuccess {
				// The status should not be Unknown if the fix worked
				// Note: The actual status depends on the kstatus library behavior
				// but it should not fail due to type conversion issues
				assert.NotEqual(t, "Unknown", string(result.Status), "Status should not be Unknown if fix worked")
			}
		})
	}
}

func TestConstants(t *testing.T) {
	// Test that constants are properly defined
	assert.Equal(t, "argoproj.io/v1alpha1", ArgoRolloutAPIVersion)
	assert.Equal(t, "Rollout", ArgoRolloutKind)
	assert.Equal(t, "status", StatusField)
	assert.Equal(t, "observedGeneration", ObservedGenerationField)
}
