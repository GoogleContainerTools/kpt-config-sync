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

package metadata

import (
	"reflect"
	"strings"

	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CommonAnnotationKeys include the annotation keys used in both the mono-repo and multi-repo mode.
var CommonAnnotationKeys = []string{
	ClusterNameAnnotationKey,
	ResourceManagementKey,
	SourcePathAnnotationKey,
	SyncTokenAnnotationKey,
	DeclaredFieldsKey,
	ResourceIDKey,
}

// MultiRepoOnlyAnnotationKeys include the annotation keys used only in the multi-repo mode.
var MultiRepoOnlyAnnotationKeys = []string{
	GitContextKey,
	ResourceManagerKey,
	OwningInventoryKey,
}

// GetNomosAnnotationKeys returns the set of Nomos annotations that Config Sync should manage.
func GetNomosAnnotationKeys() []string {
	return append(CommonAnnotationKeys, MultiRepoOnlyAnnotationKeys...)
}

// sourceAnnotations is a map of annotations that are valid to exist on objects
// in the source repository.
// These annotations are set by Config Sync users.
var sourceAnnotations = map[string]bool{
	NamespaceSelectorAnnotationKey:         true,
	LegacyClusterSelectorAnnotationKey:     true,
	ClusterNameSelectorAnnotationKey:       true,
	ResourceManagementKey:                  true,
	LifecycleMutationAnnotation:            true,
	DeletionPropagationPolicyAnnotationKey: true,
}

// IsSourceAnnotation returns true if the annotation is a ConfigSync source
// annotation.
func IsSourceAnnotation(k string) bool {
	return sourceAnnotations[k]
}

// HasConfigSyncPrefix returns true if the string begins with a ConfigSync
// annotation prefix.
func HasConfigSyncPrefix(s string) bool {
	return strings.HasPrefix(s, ConfigManagementPrefix) || strings.HasPrefix(s, configsync.ConfigSyncPrefix)
}

// IsConfigSyncAnnotationKey returns whether an annotation key is a Config Sync annotation key.
func IsConfigSyncAnnotationKey(k string) bool {
	return HasConfigSyncPrefix(k) ||
		strings.HasPrefix(k, LifecycleMutationAnnotation) ||
		k == OwningInventoryKey ||
		k == DeletionPropagationPolicyAnnotationKey
}

// isConfigSyncAnnotation returns whether an annotation is a Config Sync annotation.
func isConfigSyncAnnotation(k, v string) bool {
	return IsConfigSyncAnnotationKey(k) || (k == HNCManagedBy && v == configmanagement.GroupName)
}

// IsConfigSyncLabelKey returns whether a label key is a Config Sync label key.
func IsConfigSyncLabelKey(k string) bool {
	return HasConfigSyncPrefix(k) || k == ManagedByKey
}

// isConfigSyncLabel returns whether a label is a Config Sync label.
func isConfigSyncLabel(k, v string) bool {
	return HasConfigSyncPrefix(k) || (k == ManagedByKey && v == ManagedByValue)
}

// HasConfigSyncMetadata returns true if the given obj has at least one Config Sync annotation or label.
func HasConfigSyncMetadata(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	for k, v := range annotations {
		if isConfigSyncAnnotation(k, v) {
			return true
		}
	}

	labels := obj.GetLabels()
	for k, v := range labels {
		if isConfigSyncLabel(k, v) {
			return true
		}
	}
	return false
}

// RemoveConfigSyncMetadata removes the Config Sync metadata, including both Config Sync
// annotations and labels, from the given resource.
// The only Config Sync metadata which will not be removed is `LifecycleMutationAnnotation`.
// The resource is modified in place. Returns true if the object was modified.
func RemoveConfigSyncMetadata(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	labels := obj.GetLabels()
	before := len(annotations) + len(labels)

	// Remove Config Sync annotations
	for k, v := range annotations {
		if isConfigSyncAnnotation(k, v) && k != LifecycleMutationAnnotation {
			delete(annotations, k)
		}
	}
	obj.SetAnnotations(annotations)

	// Remove Config Sync labels
	for k, v := range labels {
		if isConfigSyncLabel(k, v) {
			delete(labels, k)
		}
	}
	obj.SetLabels(labels)

	after := len(obj.GetAnnotations()) + len(obj.GetLabels())
	return before != after
}

// UpdateConfigSyncMetadata applies the Config Sync metadata of fromObj
// to toObj where toObj is modified in place.
func UpdateConfigSyncMetadata(fromObj client.Object, toObj client.Object) {
	csAnnotations, csLabels := getConfigSyncMetadata(fromObj)

	// toObj object has the lifecycle annotation but not the fromObj object
	// This is necessary as otherwise, the annotation won't be removed when using SSA
	if fromObj.GetAnnotations()[LifecycleMutationAnnotation] == "" &&
		toObj.GetAnnotations()[LifecycleMutationAnnotation] == IgnoreMutation {
		csAnnotations[LifecycleMutationAnnotation] = ""
	}

	core.AddAnnotations(toObj, csAnnotations)
	core.AddLabels(toObj, csLabels)
}

func HasSameCSMetadata(obj1, obj2 client.Object) bool {
	// Compare CSmetadata
	csAnnotations1, csLabels1 := getConfigSyncMetadata(obj1)
	csAnnotations2, csLabels2 := getConfigSyncMetadata(obj2)

	return reflect.DeepEqual(csAnnotations1, csAnnotations2) && reflect.DeepEqual(csLabels1, csLabels2)
}

// RemoveApplySetPartOfLabel removes the ApplySet part-of label IFF the value
// matches the specified applySetID.
// The resource is modified in place. Returns true if the object was modified.
func RemoveApplySetPartOfLabel(obj client.Object, applySetID string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	v, found := labels[ApplySetPartOfLabel]
	if !found || v != applySetID {
		return false
	}
	delete(labels, ApplySetPartOfLabel)
	obj.SetLabels(labels)
	return true
}

// GetConfigSyncMetadata gets all Config Sync annotations and labels from the given resource
func getConfigSyncMetadata(obj client.Object) (map[string]string, map[string]string) {
	configSyncAnnotations := map[string]string{}
	configSyncLabels := map[string]string{}

	annotations := obj.GetAnnotations()

	for k, v := range annotations {
		if isConfigSyncAnnotation(k, v) {
			configSyncAnnotations[k] = v
		}
	}

	labels := obj.GetLabels()
	for k, v := range labels {
		if isConfigSyncLabel(k, v) {
			configSyncLabels[k] = v
		}
	}
	return configSyncAnnotations, configSyncLabels
}
