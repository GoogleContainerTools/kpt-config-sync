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
	"strings"

	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
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
func GetNomosAnnotationKeys(multiRepo bool) []string {
	if multiRepo {
		return append(CommonAnnotationKeys, MultiRepoOnlyAnnotationKeys...)
	}
	return CommonAnnotationKeys
}

// sourceAnnotations is a map of annotations that are valid to exist on objects
// in the source repository.
// These annotations are set by Config Sync users.
var sourceAnnotations = map[string]bool{
	NamespaceSelectorAnnotationKey:     true,
	LegacyClusterSelectorAnnotationKey: true,
	ClusterNameSelectorAnnotationKey:   true,
	ResourceManagementKey:              true,
	LifecycleMutationAnnotation:        true,
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
		k == OwningInventoryKey
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
