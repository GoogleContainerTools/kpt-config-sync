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
	"fmt"

	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/pkg/api/configsync"
)

// SyncSource describes the common methods available on all sources of truth.
type SyncSource interface {
	// Type returns the SourceType of this source.
	Type() configsync.SourceType
	// Commit returns the current/latest "commit" for this source.
	// The commit value is used to validate RSync status and metrics.
	Commit() (string, error)
	// TODO: Add SourceFormat
}

// GitSyncSource is the "git" source, backed by a Git repository.
type GitSyncSource struct {
	// Repository is a local clone of the remote Git repository that contains
	// the current/latest commit.
	Repository *gitproviders.Repository
	// TODO: Add SyncPath and Branch/Revision to uniquely identify part of a repo
}

// Type returns the SourceType of this source.
func (s *GitSyncSource) Type() configsync.SourceType {
	return configsync.GitSource
}

// Commit returns the current commit hash targeted by the local git repository.
func (s *GitSyncSource) Commit() (string, error) {
	return s.Repository.Hash()
}

// HelmSyncSource is the "helm" source, backed by a Helm chart.
type HelmSyncSource struct {
	ChartID registryproviders.HelmChartID
	// TODO: Add HelmRegistryProvider to allow for chart modifications
}

// Type returns the SourceType of this source.
func (s *HelmSyncSource) Type() configsync.SourceType {
	return configsync.HelmSource
}

// Commit returns the version of the current chart.
func (s *HelmSyncSource) Commit() (string, error) {
	return s.ChartID.Version, nil
}

// OCISyncSource is the "oci" source, backed by an OCI image.
type OCISyncSource struct {
	// TODO: add an OCI-specific image ID & migrate OCI RSyncs to use OCISyncSource
	// TODO: Add OCIRegistryProvider to allow for chart modifications
}

// Type returns the SourceType of this source.
func (s *OCISyncSource) Type() configsync.SourceType {
	return configsync.OciSource
}

// Commit is not yet implemented.
func (s *OCISyncSource) Commit() (string, error) {
	return "", fmt.Errorf("not yet implemented: OCISyncSource.Commit")
}
