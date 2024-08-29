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
	"strings"

	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

// SyncSource describes the common methods available on all sources of truth.
type SyncSource interface {
	// Type returns the SourceType of this source.
	Type() configsync.SourceType
	// Commit returns the current/latest "commit" for this source.
	// The value is used to validate RSync status and metrics.
	Commit() (string, error)
	// Path within the source to sync to the cluster.
	// For Helm charts, the value represents the chart name instead.
	// The value is used to validate RSync status.
	Path() string
	// TODO: Add SourceFormat?
}

// GitSyncSource is the "git" source, backed by a Git repository.
type GitSyncSource struct {
	// Repository is a reference to the repository to fetch & sync from.
	Repository gitproviders.Repository
	// Branch is the Git branch to fetch & sync.
	Branch string
	// Revision is the GitRef (branch, tag, ref, or commit) to fetch & sync.
	Revision string
	// SourceFormat of the repository
	SourceFormat configsync.SourceFormat
	// Directory is the path within the repository to sync to the cluster.
	Directory string
	// ExpectedDirectory is the path within the source to sync to the cluster.
	// Used to validate the RSync status.
	ExpectedDirectory string
	// ExpectedCommit is the Git commit expected to be pulled by the reconciler.
	// Used to validate the RSync status.
	ExpectedCommit string
}

func (s *GitSyncSource) String() string {
	return fmt.Sprintf("(syncURL: %q, branch: %q, revision: %q, directory: %q, sourceFormat: %q, expectedDirectory: %q, expectedCommit: %q)",
		s.Repository.SyncURL(), s.Branch, s.Revision, s.Directory, s.SourceFormat, s.ExpectedDirectory, s.ExpectedCommit)
}

// Type returns the SourceType of this source.
func (s *GitSyncSource) Type() configsync.SourceType {
	return configsync.GitSource
}

// Commit returns the current commit hash targeted by the local git repository.
func (s *GitSyncSource) Commit() (string, error) {
	if s.ExpectedCommit != "" {
		return s.ExpectedCommit, nil
	}
	// If ExpectedCommit is not specified, use the latest local commit.
	if localRepo, ok := s.Repository.(*gitproviders.ReadWriteRepository); ok {
		return localRepo.Hash()
	}
	// TODO: Get latest commit from ReadOnlyRepository
	// This would require more information than we have in the GitSyncSource,
	// like the auth and proxy config.
	// For now, if using ReadOnlyRepository, the ExpectedCommit is required.
	return "", nil
}

// Path within the git repository to sync to the cluster.
func (s *GitSyncSource) Path() string {
	var dir string
	if s.ExpectedDirectory != "" {
		dir = s.ExpectedDirectory
	} else {
		dir = s.Directory
	}
	if dir == "" {
		dir = controllers.DefaultSyncDir
	}
	return dir
}

// HelmSyncSource is the "helm" source, backed by a Helm chart.
type HelmSyncSource struct {
	// ChartID is the ID of the Helm chart.
	// Used to set the RSync spec.
	ChartID registryproviders.HelmChartID
	// ExpectedChartVersion is the version of the Helm chart.
	// Used to validate the RSync status.
	ExpectedChartVersion string
}

func (s *HelmSyncSource) String() string {
	return fmt.Sprintf("(chartID: %q, expectedChartVersion: %q)",
		s.ChartID, s.ExpectedChartVersion)
}

// Type returns the SourceType of this source.
func (s *HelmSyncSource) Type() configsync.SourceType {
	return configsync.HelmSource
}

// Commit returns the version of the current chart.
func (s *HelmSyncSource) Commit() (string, error) {
	if s.ExpectedChartVersion != "" {
		return s.ExpectedChartVersion, nil
	}
	return s.ChartID.Version, nil
}

// Path returns the name of the current chart.
func (s *HelmSyncSource) Path() string {
	return s.ChartID.Name
}

// OCISyncSource is the "oci" source, backed by an OCI image.
type OCISyncSource struct {
	// ImageID is the ID of the OCI image.
	// Used to set the RSync spec.
	ImageID registryproviders.OCIImageID
	// Directory is the path within the OCI image to sync to the cluster.
	// Used to set the RSync spec.
	Directory string
	// ExpectedDirectory is the path within the OCI image to sync to the cluster.
	// Used to validate the RSync status.
	ExpectedDirectory string
	// ImageDigest of the OCI image, including "sha256:" prefix.
	// Used to validate the RSync status.
	ExpectedImageDigest string
}

func (s *OCISyncSource) String() string {
	return fmt.Sprintf("(imageID: %q, directory: %q, expectedDirectory: %q, expectedImageDigest: %q)",
		s.ImageID, s.Directory, s.ExpectedDirectory, s.ExpectedImageDigest)
}

// Type returns the SourceType of this source.
func (s *OCISyncSource) Type() configsync.SourceType {
	return configsync.OciSource
}

// Commit is not yet implemented.
func (s *OCISyncSource) Commit() (string, error) {
	var digest string
	if s.ExpectedImageDigest != "" {
		digest = s.ExpectedImageDigest
	} else {
		digest = s.ImageID.Digest
	}
	return strings.TrimPrefix(digest, "sha256:"), nil
}

// Path within the OCI image to sync to the cluster.
func (s *OCISyncSource) Path() string {
	var dir string
	if s.ExpectedDirectory != "" {
		dir = s.ExpectedDirectory
	} else {
		dir = s.Directory
	}
	if dir == "" {
		dir = controllers.DefaultSyncDir
	}
	return dir
}
