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
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
)

// RootScope is the scope that includes both cluster-scoped and namespace-scoped
// resource objects. A "root reconciler" can manage resource objects in any
// namespace (given the appropriate RBAC permissions).
//
// Any Scope that is not the RootScope is assumed to be the name of a namespace.
const RootScope = Scope(":root")

// Scope defines a distinct (but not necessarily disjoint) area of responsibility
// for a Reconciler.
type Scope string

// ScopeFromSyncNamespace returns the scope associated with a specific
// namespace.
//
// This is possible because:
// - RepoSyncs are NOT allowed in the "config-management-system" namespace.
// - RootSyncs are ONLY allowed in the "config-management-system" namespace.
func ScopeFromSyncNamespace(syncNamespace string) Scope {
	if syncNamespace == configmanagement.ControllerNamespace {
		return RootScope
	}
	return Scope(syncNamespace)
}

// SyncNamespace returns the namespace associated with this Scope.
//
// This is possible because:
// - RepoSyncs are NOT allowed in the "config-management-system" namespace.
// - RootSyncs are ONLY allowed in the "config-management-system" namespace.
func (s Scope) SyncNamespace() string {
	if s == RootScope {
		return configmanagement.ControllerNamespace
	}
	return string(s)
}

// SyncKind returns the resource kind associated with this Scope.
func (s Scope) SyncKind() string {
	if s == RootScope {
		return configsync.RootSyncKind
	}
	return configsync.RepoSyncKind
}

// String returns the scope as a string.
func (s Scope) String() string {
	return string(s)
}

// Validate returns an error if the Scope is invalid.
// Non-root Scopes must be valid DNS subdomains (RFC 1123).
func (s Scope) Validate() error {
	if s == RootScope {
		return nil
	}
	errs := validation.IsDNS1123Subdomain(s.String())
	if len(errs) > 0 {
		return status.InternalErrorf("invalid scope %q: %v", s, errs)
	}
	return nil
}

// ReconcilerNameFromScope returns the reconciler name associated with a
// specific sync scope and sync name.
func ReconcilerNameFromScope(syncScope Scope, syncName string) string {
	if syncScope == RootScope {
		return core.RootReconcilerName(syncName)
	}
	return core.NsReconcilerName(syncScope.String(), syncName)
}
