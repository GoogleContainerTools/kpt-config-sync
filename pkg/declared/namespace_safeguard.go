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

package declared

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// deletesAllNamespaces is a sanity check that ensures we don't accidentally
// delete all user data because we misread configs from the file system.
//
// Returns an error if:
// 1) previous contains more than 1 Namespace.
// 2) current contains zero Namespaces.
//
// Otherwise returns nil.
func deletesAllNamespaces(previous, current map[core.ID]*unstructured.Unstructured) status.Error {
	var previousNamespaces []string
	var currentNamespaces []string

	for p := range previous {
		if p.GroupKind == kinds.Namespace().GroupKind() {
			previousNamespaces = append(previousNamespaces, p.Name)
		}
	}
	for c := range current {
		if c.GroupKind == kinds.Namespace().GroupKind() {
			currentNamespaces = append(currentNamespaces, c.Name)
		}
	}

	if len(currentNamespaces) == 0 && len(previousNamespaces) >= 2 {
		return DeleteAllNamespacesError(previousNamespaces)
	}
	return nil
}

// DeleteAllNamespacesError represents a failsafe error we return to ensure we
// don't accidentally delete all of a user's data. We've had filesystem read
// errors in the past which delete all Namespaces at once.
//
// Thus, we require users to commit a change that deletes all but one Namespace
// before deleting the final one. This is the error we show to prevent us from
// taking such destructive action, and ensuring users know how to tell ACM that
// they really do want to delete all Namespaces.
func DeleteAllNamespacesError(previous []string) status.Error {
	return status.EmptySourceErrorBuilder.Sprintf(
		"New commit would delete all Namespaces %v. "+
			"If this is not a mistake, follow directions for safely deleting all Namespaces.",
		previous).Build()
}
