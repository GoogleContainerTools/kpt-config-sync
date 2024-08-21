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

package syncsource

import (
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
)

// Set of RootSync & RepoSync IDs that map to SyncSources.
type Set map[core.ID]SyncSource

// Reset clears the Set.
func (s *Set) Reset() {
	clear(*s)
}

// RootSyncs returns a Set that only contains the RootSync IDs and their SyncSources.
func (s Set) RootSyncs() Set {
	set := make(map[core.ID]SyncSource)
	for id, source := range s {
		if id.Kind == configsync.RootSyncKind {
			set[id] = source
		}
	}
	return set
}

// RepoSyncs returns a Set that only contains the RepoSync IDs and their SyncSources.
func (s Set) RepoSyncs() Set {
	set := make(map[core.ID]SyncSource)
	for id, source := range s {
		if id.Kind == configsync.RepoSyncKind {
			set[id] = source
		}
	}
	return set
}

// GitSyncSources returns a Set that only contains RootSyncs & RepoSyncs using GitSyncSources.
func (s Set) GitSyncSources() Set {
	set := make(map[core.ID]SyncSource)
	for id, source := range s {
		if _, ok := source.(*GitSyncSource); ok {
			set[id] = source
		}
	}
	return set
}

// HelmSyncSources returns a Set that only contains RootSyncs & RepoSyncs using HelmSyncSources.
func (s Set) HelmSyncSources() Set {
	set := make(map[core.ID]SyncSource)
	for id, source := range s {
		if _, ok := source.(*HelmSyncSource); ok {
			set[id] = source
		}
	}
	return set
}

// OCISyncSources returns a Set that only contains RootSyncs & RepoSyncs using OCISyncSources.
func (s Set) OCISyncSources() Set {
	set := make(map[core.ID]SyncSource)
	for id, source := range s {
		if _, ok := source.(*OCISyncSource); ok {
			set[id] = source
		}
	}
	return set
}
