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
	"strings"

	"kpt.dev/configsync/pkg/api/configsync"
)

const delimiter = "_"

// ResourceManager returns the manager to indicate a resource is managed by a particular reconciler.
// If the manager is the default RootSync or RepoSync, only return the scope for backward compatibility.
// Otherwise, return scope_name.
func ResourceManager(scope Scope, rsName string) string {
	if rsName == configsync.RootSyncName || rsName == configsync.RepoSyncName {
		return string(scope)
	}
	return string(scope) + delimiter + rsName
}

// IsRootManager returns whether the manager is running on a root reconciler.
func IsRootManager(manager string) bool {
	return strings.HasPrefix(manager, string(RootReconciler))
}

// ManagerScopeAndName returns the scope and name of the resource manager.
func ManagerScopeAndName(manager string) (Scope, string) {
	scopeAndName := strings.Split(manager, delimiter)
	if len(scopeAndName) == 0 || scopeAndName[0] == "" {
		return "", ""
	}
	scope := Scope(scopeAndName[0])
	switch scope {
	case RootReconciler:
		if len(scopeAndName) > 1 {
			return scope, scopeAndName[1]
		}
		return scope, configsync.RootSyncName
	default:
		if len(scopeAndName) > 1 {
			return scope, scopeAndName[1]
		}
		return scope, configsync.RepoSyncName
	}
}
