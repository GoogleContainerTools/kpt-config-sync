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
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
)

// SetExpectedSyncPath updates the SyncSource for the specified RootSync or
// RepoSync with the provided Git dir, OCI dir.
func SetExpectedSyncPath(nt *NT, syncID core.ID, syncPath string) {
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Fatalf("Failed to update expectation for %s %s: nt.SyncSources not registered", syncID.Kind, syncID.ObjectKey)
	}
	switch tSource := source.(type) {
	case *syncsource.GitSyncSource:
		tSource.Directory = syncPath
	case *syncsource.OCISyncSource:
		tSource.Directory = syncPath
	case *syncsource.HelmSyncSource:
		nt.T.Fatalf("Failed to update expectation for %s %s: Use SetExpectedHelmChart instead", syncID.Kind, syncID.ObjectKey)
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}

// SetExpectedGitSource creates, updates, or replaces the SyncSource for the
// specified RootSync or RepoSync with the provided Git repo.
func SetExpectedGitSource(nt *NT, syncID core.ID, repo *gitproviders.Repository, syncPath string, sourceFormat configsync.SourceFormat) {
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Logf("Creating expectation for %s %s to sync with Git repo (repository: %q, directory: %q, sourceFormat: %q)",
			syncID.Kind, syncID.ObjectKey, repo.Name, syncPath, sourceFormat)
		nt.SyncSources[syncID] = &syncsource.GitSyncSource{
			Repository:   repo,
			SourceFormat: sourceFormat,
			Directory:    syncPath,
		}
		return
	}
	switch tSource := source.(type) {
	case *syncsource.GitSyncSource:
		nt.T.Logf("Updating expectation for %s %s to sync with Git repo (repository: %q, directory: %q, sourceFormat: %q)",
			syncID.Kind, syncID.ObjectKey, repo.Name, syncPath, sourceFormat)
		tSource.Repository = repo
		tSource.SourceFormat = sourceFormat
		tSource.Directory = syncPath
	case *syncsource.OCISyncSource, *syncsource.HelmSyncSource:
		nt.T.Logf("Replacing expectation for %s %s to sync with Git repo (repository: %q, directory: %q, sourceFormat: %q), instead of %T",
			syncID.Kind, syncID.ObjectKey, repo.Name, syncPath, sourceFormat, source)
		nt.SyncSources[syncID] = &syncsource.GitSyncSource{
			Repository:   repo,
			SourceFormat: sourceFormat,
			Directory:    syncPath,
		}
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}

// SetExpectedOCISource creates, updates, or replaces the SyncSource for the
// specified RootSync or RepoSync with the provided OCI image.
// ImageID is used for pulling. ImageDigest is used for validating the result.
func SetExpectedOCISource(nt *NT, syncID core.ID, imageID registryproviders.OCIImageID, imageDigest, syncPath string) {
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Logf("Creating expectation for %s %s to sync with OCI image (image: %q, directory: %q)",
			syncID.Kind, syncID.ObjectKey, imageID, syncPath)
		nt.SyncSources[syncID] = &syncsource.OCISyncSource{
			ImageID:   imageID,
			Digest:    imageDigest,
			Directory: syncPath,
		}
		return
	}
	switch tSource := source.(type) {
	case *syncsource.OCISyncSource:
		nt.T.Logf("Updating expectation for %s %s to sync with OCI image (image: %q, directory: %q)",
			syncID.Kind, syncID.ObjectKey, imageID, syncPath)
		tSource.ImageID = imageID
		tSource.Digest = imageDigest
		tSource.Directory = syncPath
	case *syncsource.GitSyncSource, *syncsource.HelmSyncSource:
		nt.T.Logf("Replacing expectation for %s %s to sync with OCI image (image: %q, directory: %q), instead of %T",
			syncID.Kind, syncID.ObjectKey, imageID, syncPath, source)
		nt.SyncSources[syncID] = &syncsource.OCISyncSource{
			ImageID:   imageID,
			Digest:    imageDigest,
			Directory: syncPath,
		}
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}

// SetExpectedHelmSource creates, updates, or replaces the SyncSource for the
// specified RootSync or RepoSync with the provided Helm chart ID.
func SetExpectedHelmSource(nt *NT, syncID core.ID, chartID registryproviders.HelmChartID) {
	source, exists := nt.SyncSources[syncID]
	if !exists {
		nt.T.Logf("Creating expectation for %s %s to sync with helm chart (id: %q)",
			syncID.Kind, syncID.ObjectKey, chartID)
		nt.SyncSources[syncID] = &syncsource.HelmSyncSource{
			ChartID: chartID,
		}
		return
	}
	switch tSource := source.(type) {
	case *syncsource.HelmSyncSource:
		nt.T.Logf("Updating expectation for %s %s to sync with helm chart (id: %q)",
			syncID.Kind, syncID.ObjectKey, chartID)
		tSource.ChartID = chartID
	case *syncsource.GitSyncSource, *syncsource.OCISyncSource:
		nt.T.Logf("Replacing expectation for %s %s to sync with helm chart (id: %q), instead of %T",
			syncID.Kind, syncID.ObjectKey, chartID, source)
		nt.SyncSources[syncID] = &syncsource.HelmSyncSource{
			ChartID: chartID,
		}
	default:
		nt.T.Fatalf("Invalid %s source %T: %s", syncID.Kind, source, syncID.Name)
	}
}
