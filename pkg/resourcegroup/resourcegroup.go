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

package resourcegroup

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"sigs.k8s.io/cli-utils/pkg/common"
)

const (
	// SourceHashAnnotationKey is the name of the annotation which contains the source hash
	SourceHashAnnotationKey = "configmanagement.gke.io/token"
	// DisableStatusKey is the annotation key used for disabling ResourceGroup status
	DisableStatusKey = "configsync.gke.io/status"
	// DisableStatusValue is the annotation value used for disabling ResourceGroup status
	DisableStatusValue = "disabled"
)

// Unstructured creates a ResourceGroup object
func Unstructured(name, namespace, id string) *unstructured.Unstructured {
	groupVersion := fmt.Sprintf("%s/%s", v1alpha1.SchemeGroupVersionKind().Group, v1alpha1.SchemeGroupVersionKind().Version)
	inventoryObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": groupVersion,
			"kind":       v1alpha1.SchemeGroupVersionKind().Kind,
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					common.InventoryLabel: id,
				},
			},
			"spec": map[string]interface{}{
				"resources": []interface{}{},
			},
		},
	}
	return inventoryObj
}

// GetSourceHash returns the source hash that is defined in the
// source hash annotation.
func GetSourceHash(annotations map[string]string) string {
	if len(annotations) == 0 {
		return ""
	}
	return TruncateSourceHash(annotations[SourceHashAnnotationKey])
}

// TruncateSourceHash truncates the provided source hash after the first 7 characters.
func TruncateSourceHash(sourceHash string) string {
	if len(sourceHash) > 7 {
		return sourceHash[0:7]
	}
	return sourceHash
}

// IsStatusDisabled returns whether the ResourceGroup has disabled status updates.
func IsStatusDisabled(resgroup *v1alpha1.ResourceGroup) bool {
	annotations := resgroup.GetAnnotations()
	if annotations == nil {
		return false
	}
	val, found := annotations[DisableStatusKey]
	return found && val == DisableStatusValue
}
