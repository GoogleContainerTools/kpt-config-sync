// Copyright 2024 Google LLC
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

package nomostest

import (
	"reflect"

	"kpt.dev/configsync/e2e/nomostest/syncsource"
	"kpt.dev/configsync/pkg/core"
)

// SetExpectedSyncSource creates, updates, or replaces the Server for the
// specified RootSync or RepoSync.
func SetExpectedSyncSource(nt *NT, syncID core.ID, source syncsource.SyncSource) {
	oldSource, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Logf("Creating expectation for %s %s to sync with %T (%s)",
			syncID.Kind, syncID.ObjectKey, source, source)
	} else if reflect.TypeOf(oldSource) == reflect.TypeOf(source) {
		nt.T.Logf("Updating expectation for %s %s to sync with %T (%s)",
			syncID.Kind, syncID.ObjectKey, source, source)
	} else {
		nt.T.Logf("Replacing expectation for %s %s to sync with %T (%s), instead of %T",
			syncID.Kind, syncID.ObjectKey, source, source, oldSource)
	}
	nt.SyncSources[syncID] = source
}

// UpdateExpectedSyncSource executes the provided function on an existing
// Server in the SyncSources, allowing updating multiple fields.
func UpdateExpectedSyncSource[T syncsource.SyncSource](nt *NT, syncID core.ID, mutateFn func(T)) {
	nt.T.Helper()
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Fatalf("Failed to update expectation for %s %s: not registered in nt.SyncSources", syncID.Kind, syncID.ObjectKey)
	}
	switch tSource := source.(type) {
	case T:
		mutateFn(tSource)
		nt.T.Logf("Updating expectation for %s %s to sync with %T (%s)",
			syncID.Kind, syncID.ObjectKey, tSource, tSource)
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}

// SetExpectedSyncPath updates the Server for the specified RootSync or
// RepoSync with the provided Git dir, OCI dir.
// Updates both the Directory & ExpectedDirectory.
func SetExpectedSyncPath(nt *NT, syncID core.ID, syncPath string) {
	nt.T.Helper()
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Fatalf("Failed to update expectation for %s %s: not registered in nt.SyncSources", syncID.Kind, syncID.ObjectKey)
	}
	switch tSource := source.(type) {
	case *syncsource.GitSyncSource:
		tSource.Directory = syncPath
		tSource.ExpectedDirectory = ""
	case *syncsource.OCISyncSource:
		tSource.Directory = syncPath
		tSource.ExpectedDirectory = ""
	case *syncsource.HelmSyncSource:
		nt.T.Fatalf("Failed to update expectation for %s %s: Use SetExpectedSyncSource instead", syncID.Kind, syncID.ObjectKey)
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}

// SetExpectedGitCommit updates the Server for the specified RootSync or
// RepoSync with the provided Git commit, without changing the git reference of
// the source being pulled.
func SetExpectedGitCommit(nt *NT, syncID core.ID, expectedCommit string) {
	nt.T.Helper()
	UpdateExpectedSyncSource(nt, syncID, func(source *syncsource.GitSyncSource) {
		source.ExpectedCommit = expectedCommit
	})
}

// SetExpectedOCIImageDigest updates the Server for the specified RootSync
// or RepoSync with the provided OCI image digest, without changing the ID of
// the image being pulled.
func SetExpectedOCIImageDigest(nt *NT, syncID core.ID, expectedImageDigest string) {
	nt.T.Helper()
	UpdateExpectedSyncSource(nt, syncID, func(source *syncsource.OCISyncSource) {
		source.ExpectedImageDigest = expectedImageDigest
	})
}
