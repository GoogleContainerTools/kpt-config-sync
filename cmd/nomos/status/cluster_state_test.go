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

package status

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	git = &v1beta1.Git{
		Repo:     "git@github.com:tester/sample",
		Revision: "v1",
		Dir:      "admin",
	}

	oci = &v1beta1.Oci{
		Image: "us-docker.pkg.dev/test-project/test-ar-repo/sample",
		Dir:   "test",
	}

	helm = &v1beta1.HelmBase{
		Repo:    "oci://us-central1-docker.pkg.dev/your-dev-project/sample",
		Chart:   "test",
		Version: "0.1.0",
	}

	gitUpdated = &v1beta1.Git{
		Repo:     "git@github.com:tester/sample-updated",
		Revision: "v2",
		Dir:      "admin",
	}

	errorSummayWithOneError = &v1beta1.ErrorSummary{
		TotalCount:                1,
		Truncated:                 false,
		ErrorCountAfterTruncation: 1,
	}

	errorSummayWithTwoErrors = &v1beta1.ErrorSummary{
		TotalCount:                2,
		Truncated:                 false,
		ErrorCountAfterTruncation: 2,
	}

	lastSyncTimestamp = metav1.Now()
)

func TestRepoState_PrintRows(t *testing.T) {
	testCases := []struct {
		name string
		repo *RepoState
		want string
	}{
		{
			"optional git fields missing",
			&RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git: &v1beta1.Git{
					Repo: "https://github.com/tester/sample/",
				},
				status:    "SYNCED",
				commit:    "abc123",
				resources: exampleResources("abc123"),
			},
			"  <root>:root-sync\thttps://github.com/tester/sample@master\t\n  SYNCED @ 0001-01-01 00:00:00 +0000 UTC\tabc123\t\n  Managed resources:\n  \tNAMESPACE\tNAME\tSTATUS\tSOURCEHASH\n  \tbookstore\tdeployment.apps/test\tCurrent\tabc123\n  \tbookstore\tservice/test\tFailed\tabc123\n        A detailed message explaining the current condition.\n  \tbookstore\tservice/test2\tConflict\tabc123\n        A detailed message explaining why it is in the status ownership overlap.\n",
		},
		{
			"optional git subdirectory specified",
			&RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git: &v1beta1.Git{
					Repo: "https://github.com/tester/sample/",
					Dir:  "quickstart//multirepo//root/",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  <root>:root-sync\thttps://github.com/tester/sample/quickstart/multirepo/root@master\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional git subdirectory is '/'",
			&RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git: &v1beta1.Git{
					Repo: "https://github.com/tester/sample/",
					Dir:  "/",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  <root>:root-sync\thttps://github.com/tester/sample@master\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional git subdirectory is '.'",
			&RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git: &v1beta1.Git{
					Repo: "https://github.com/tester/sample/",
					Dir:  ".",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  <root>:root-sync\thttps://github.com/tester/sample@master\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional git subdirectory starts with '/'",
			&RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git: &v1beta1.Git{
					Repo: "https://github.com/tester/sample/",
					Dir:  "/admin",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  <root>:root-sync\thttps://github.com/tester/sample/admin@master\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional git branch specified",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:   "https://github.com/tester/sample",
					Branch: "feature",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  bookstore:repo-sync\thttps://github.com/tester/sample@feature\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional git revision specified",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "https://github.com/tester/sample",
					Revision: "v1",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  bookstore:repo-sync\thttps://github.com/tester/sample@v1\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional default git revision HEAD specified",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "https://github.com/tester/sample",
					Revision: "HEAD",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  bookstore:repo-sync\thttps://github.com/tester/sample@master\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"optional default git revision HEAD and branch specified",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "git@github.com:tester/sample",
					Revision: "HEAD",
					Branch:   "feature",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  bookstore:repo-sync\tgit@github.com:tester/sample@feature\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"all optional git fields specified",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "git@github.com:tester/sample",
					Dir:      "books",
					Branch:   "feature",
					Revision: "v1",
				},
				status:            "SYNCED",
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
			fmt.Sprintf("  bookstore:repo-sync\tgit@github.com:tester/sample/books@v1\t\n  SYNCED @ %v\tabc123\t\n", lastSyncTimestamp),
		},
		{
			"repo with errors",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "git@github.com:tester/sample",
					Dir:      "books",
					Revision: "v1",
				},
				status:       "ERROR",
				commit:       "abc123",
				errors:       []string{"error1", "error2"},
				errorSummary: errorSummayWithTwoErrors,
			},
			"  bookstore:repo-sync\tgit@github.com:tester/sample/books@v1\t\n  ERROR\tabc123\t\n  TotalErrorCount: 2\n  Error:\terror1\t\n  Error:\terror2\t\n",
		},
		{
			"repo with errors (truncated)",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "git@github.com:tester/sample",
					Dir:      "books",
					Revision: "v1",
				},
				status: "ERROR",
				commit: "abc123",
				errors: []string{"error1", "error2"},
				errorSummary: &v1beta1.ErrorSummary{
					TotalCount:                20,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			"  bookstore:repo-sync\tgit@github.com:tester/sample/books@v1\t\n  ERROR\tabc123\t\n  TotalErrorCount: 20, ErrorTruncated: true, ErrorCountAfterTruncation: 2\n  Error:\terror1\t\n  Error:\terror2\t\n",
		},
		{
			"unsynced repo",
			&RepoState{
				scope:    "bookstore",
				syncName: "repo-sync",
				git: &v1beta1.Git{
					Repo:     "git@github.com:tester/sample",
					Revision: "v1",
				},
				status: "PENDING",
			},
			"  bookstore:repo-sync\tgit@github.com:tester/sample@v1\t\n  PENDING\t\t\n",
		},
		{
			"OCI repo with source error",
			&RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.OciSource,
				oci: &v1beta1.Oci{
					Image: "us-docker.pkg.dev/test-project/test-ar-repo/sample",
					Dir:   "test",
				},
				status:       "ERROR",
				commit:       "abc123",
				errors:       []string{"error1", "error2"},
				errorSummary: errorSummayWithTwoErrors,
			},
			"  bookstore:repo-sync\tus-docker.pkg.dev/test-project/test-ar-repo/sample/test\t\n  ERROR\tabc123\t\n  TotalErrorCount: 2\n  Error:\terror1\t\n  Error:\terror2\t\n",
		},
		{
			"Helm repo with source error",
			&RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.HelmSource,
				helm:         helm,
				status:       "ERROR",
				commit:       "abc123",
				errors:       []string{"error1", "error2"},
				errorSummary: errorSummayWithTwoErrors,
			},
			"  bookstore:repo-sync\toci://us-central1-docker.pkg.dev/your-dev-project/sample/test:0.1.0\t\n  ERROR\tabc123\t\n  TotalErrorCount: 2\n  Error:\terror1\t\n  Error:\terror2\t\n",
		},
		{
			"Git field is missing when sourceType is git",
			&RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				status:       "ERROR",
				errors:       []string{"missing Git config"},
				errorSummary: errorSummayWithOneError,
			},
			"  bookstore:repo-sync\tN/A\t\n  ERROR\t\t\n  TotalErrorCount: 1\n  Error:\tmissing Git config\t\n",
		},
		{
			"OCI field is missing when sourceType is oci",
			&RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.OciSource,
				status:       "ERROR",
				errors:       []string{"missing OCI config"},
				errorSummary: errorSummayWithOneError,
			},
			"  bookstore:repo-sync\tN/A\t\n  ERROR\t\t\n  TotalErrorCount: 1\n  Error:\tmissing OCI config\t\n",
		},
		{
			"Helm field is missing when sourceType is helm",
			&RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.HelmSource,
				status:       "ERROR",
				errors:       []string{"missing Helm config"},
				errorSummary: errorSummayWithOneError,
			},
			"  bookstore:repo-sync\tN/A\t\n  ERROR\t\t\n  TotalErrorCount: 1\n  Error:\tmissing Helm config\t\n",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			tc.repo.printRows(&buffer)
			got := buffer.String()
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func toGitStatus(git *v1beta1.Git) *v1beta1.GitStatus {
	return &v1beta1.GitStatus{
		Repo:     git.Repo,
		Revision: git.Revision,
		Branch:   git.Branch,
		Dir:      git.Dir,
	}
}

func toOciStatus(oci *v1beta1.Oci) *v1beta1.OciStatus {
	return &v1beta1.OciStatus{
		Image: oci.Image,
		Dir:   oci.Dir,
	}
}

func toHelmStatus(h *v1beta1.HelmBase) *v1beta1.HelmStatus {
	return &v1beta1.HelmStatus{
		Repo:    h.Repo,
		Chart:   h.Chart,
		Version: h.Version,
	}
}

func TestRepoState_NamespaceRepoStatus(t *testing.T) {
	stalledCondition := v1beta1.RepoSyncCondition{
		Type:    v1beta1.RepoSyncStalled,
		Status:  metav1.ConditionTrue,
		Reason:  "Deployment",
		Message: "deployment failure",
	}

	reconcilingCondition := v1beta1.RepoSyncCondition{
		Type:    v1beta1.RepoSyncReconciling,
		Status:  metav1.ConditionTrue,
		Reason:  "Deployment",
		Message: "deployment in progress",
	}

	reconciledCondition := v1beta1.RepoSyncCondition{
		Type:   v1beta1.RepoSyncReconciling,
		Status: metav1.ConditionFalse,
	}

	syncingTrueCondition := func(commit, msg string) v1beta1.RepoSyncCondition {
		return v1beta1.RepoSyncCondition{
			Type:    v1beta1.RepoSyncSyncing,
			Status:  metav1.ConditionTrue,
			Commit:  commit,
			Message: msg,
		}
	}

	syncingFalseCondition := func(commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary) v1beta1.RepoSyncCondition {
		return v1beta1.RepoSyncCondition{
			Type:            v1beta1.RepoSyncSyncing,
			Status:          metav1.ConditionFalse,
			Commit:          commit,
			ErrorSourceRefs: errorSources,
			ErrorSummary:    errorSummary,
		}
	}

	testCases := []struct {
		name                      string
		syncingConditionSupported bool
		gitSpec                   *v1beta1.Git
		ociSpec                   *v1beta1.Oci
		helmSpec                  *v1beta1.HelmBase
		sourceType                configsync.SourceType
		conditions                []v1beta1.RepoSyncCondition
		sourceStatus              v1beta1.SourceStatus
		renderingStatus           v1beta1.RenderingStatus
		syncStatus                v1beta1.SyncStatus
		resourceGroup             *unstructured.Unstructured
		want                      *RepoState
	}{
		{
			name:       "fresh installation, namespace reconciler is stalled",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{stalledCondition},
			want: &RepoState{
				scope:        "bookstore",
				sourceType:   configsync.GitSource,
				syncName:     "repo-sync",
				git:          git,
				status:       stalledMsg,
				commit:       emptyCommit,
				errors:       []string{"deployment failure"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "switch repo, namespace reconciler is stalled",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{stalledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       stalledMsg,
				commit:       emptyCommit,
				errors:       []string{"deployment failure"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "fresh installation, namespace reconciler is reconciling",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconcilingCondition},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     reconcilingMsg,
				commit:     emptyCommit,
			},
		},
		{
			name:       "switch repo, namespace reconciler is reconciling",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconcilingCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     reconcilingMsg,
				commit:     emptyCommit,
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] fresh installation, namespace reconciler is importing source configs",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				commit:     emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is importing source configs",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions:                []v1beta1.RepoSyncCondition{reconciledCondition},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				commit:     emptyCommit,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is importing source configs",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				// This mistakenly reports an error because `nomos status` checks all errors first.
				// The following test case shows how the status is reported correctly with the syncing condition.
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is importing source configs",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions:                []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     pendingMsg,
				commit:     emptyCommit,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has import error",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     util.ErrorMsg,
				// This mistakenly reports the commit as empty because "nomos status" used
				// to read the commit from .status.sync.commit, which is not available at this point.
				// The test case below shows how the commit is reported successfully via the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has import error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has import error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     util.ErrorMsg,
				// This mistakenly shows the commit because `nomos status` sets commit to `.status.sync.commit`.
				// The test case below shows how it is fixed.
				commit: "abc123",
				// The errors are also wrong because it included a sync error from a previous commit.
				errors:       []string{"KNV2004: import error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has import error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is rendering new commit",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering in progress",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				// This mistakenly reports an empty commit because `nomos status` sets commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with the syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is rendering new commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "rendering in progress"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering in progress",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				commit:     "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is rendering new commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering in progress",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				// This mistakenly reports an error status because `nomos status` checks all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it still reports an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an old error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is rendering new commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "rendering in progress"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering in progress",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     pendingMsg,
				commit:     "def456",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has rendering error",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     util.ErrorMsg,
				// This mistakenly reports an empty commit because `nomos status` sets the commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has rendering error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has rendering error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     util.ErrorMsg,
				// This mistakenly reports the commit and errors because `nomos status` checks all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				commit: "abc123",
				// The errors are wrong because it includes an apply error from a previous commit.
				errors:       []string{"KNV2015: rendering error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has rendering error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is parsing and validating rendered commit",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				// This mistakenly reports the commit to be empty because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with they syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is parsing and validating rendered commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "rendering succeeded"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				commit:     "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is parsing and validating rendered commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering succeeded",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				// This mistakenly reports an error status because `nomos status` used to check all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it reports to an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an apply error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is parsing and validating rendered commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "rendering succeeded"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering succeeded",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     pendingMsg,
				commit:     "def456",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has parsing or validation error",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     util.ErrorMsg,
				// This mistakenly reports the commit as empty because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has parsing or validation error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has parsing or validation error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     util.ErrorMsg,
				// This mistakenly reports the commit because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit: "abc123",
				// The errors are wrong because it includes an apply error from a previous commit.
				errors:       []string{"KNV2004: parsing error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has parsing or validation error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is syncing validated WET commit",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				// The commit should be available because it is included in the rendering and source statuses,
				// but `nomos status` used to set the commit to be .status.sync.commit, which is `N/A`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is syncing validated WET commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "Rendering skipped"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "Rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        git,
				status:     pendingMsg,
				commit:     "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is syncing validated WET commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				// This mistakenly reports an error status because `nomos status` checks all errors first.
				// The test case below shows how the status is correctly reported with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it reports an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an apply error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is syncing validated WET commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "Rendering skipped"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "Rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:      "bookstore",
				syncName:   "repo-sync",
				sourceType: configsync.GitSource,
				git:        gitUpdated,
				status:     pendingMsg,
				commit:     "def456",
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] fresh installation, repo has non-blocking source error and syncing error",
			gitSpec:    git,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has non-blocking source error and syncing error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError}, errorSummayWithTwoErrors),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] switch repo, repo has non-blocking source error and syncing error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has non-blocking source error and syncing error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError}, errorSummayWithTwoErrors),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				sourceType:   configsync.GitSource,
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:          "[accurate status before syncing condition is supported] repo is synced",
			gitSpec:       git,
			resourceGroup: k8sobjects.ResourceGroupObject(core.Namespace("bookstore"), core.Name("repo-sync"), withResources()),
			conditions:    []v1beta1.RepoSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:        toGitStatus(git),
				Commit:     "abc123",
				LastUpdate: lastSyncTimestamp,
			},
			want: &RepoState{
				scope:             "bookstore",
				syncName:          "repo-sync",
				sourceType:        configsync.GitSource,
				git:               git,
				status:            syncedMsg,
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
				resources:         exampleResources(""),
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] repo is synced",
			gitSpec:                   git,
			resourceGroup:             k8sobjects.ResourceGroupObject(core.Namespace("bookstore"), core.Name("repo-sync"), withResourcesAndCommit("abc123")),
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", nil, nil),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:        toGitStatus(git),
				Commit:     "abc123",
				LastUpdate: lastSyncTimestamp,
			},
			want: &RepoState{
				scope:             "bookstore",
				syncName:          "repo-sync",
				git:               git,
				sourceType:        configsync.GitSource,
				status:            syncedMsg,
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
				resources:         exampleResources("abc123"),
			},
		},
		{
			name:                      "OCI repo has import error",
			ociSpec:                   oci,
			sourceType:                configsync.OciSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Oci:    toOciStatus(oci),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				oci:          oci,
				sourceType:   configsync.OciSource,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "OCI repo has import error",
			ociSpec:                   oci,
			sourceType:                configsync.OciSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Oci:    toOciStatus(oci),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				oci:          oci,
				sourceType:   configsync.OciSource,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "OCI repo has rendering error",
			ociSpec:                   oci,
			sourceType:                configsync.OciSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Oci:     toOciStatus(oci),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			sourceStatus: v1beta1.SourceStatus{
				Oci:    toOciStatus(oci),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Oci:    toOciStatus(oci),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				oci:          oci,
				sourceType:   configsync.OciSource,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "OCI repo has syncing error",
			ociSpec:                   oci,
			sourceType:                configsync.OciSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SyncError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Oci:     toOciStatus(oci),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			sourceStatus: v1beta1.SourceStatus{
				Oci:    toOciStatus(oci),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Oci:    toOciStatus(oci),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				oci:          oci,
				sourceType:   configsync.OciSource,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "Helm repo has import error",
			helmSpec:                  helm,
			sourceType:                configsync.HelmSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Helm:   toHelmStatus(helm),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				helm:         helm,
				sourceType:   configsync.HelmSource,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "Helm repo has rendering error",
			helmSpec:                  helm,
			sourceType:                configsync.HelmSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Helm:   toHelmStatus(helm),
				Commit: "def456",
			},
			renderingStatus: v1beta1.RenderingStatus{
				Helm:    toHelmStatus(helm),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			syncStatus: v1beta1.SyncStatus{
				Helm:   toHelmStatus(helm),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				helm:         helm,
				sourceType:   configsync.HelmSource,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "Helm repo has syncing error",
			helmSpec:                  helm,
			sourceType:                configsync.HelmSource,
			syncingConditionSupported: true,
			conditions: []v1beta1.RepoSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SyncError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Helm:   toHelmStatus(helm),
				Commit: "abc123",
			},
			renderingStatus: v1beta1.RenderingStatus{
				Helm:    toHelmStatus(helm),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			syncStatus: v1beta1.SyncStatus{
				Helm:   toHelmStatus(helm),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "bookstore",
				syncName:     "repo-sync",
				helm:         helm,
				sourceType:   configsync.HelmSource,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			repoSync := k8sobjects.RepoSyncObjectV1Beta1("bookstore", configsync.RepoSyncName)
			repoSync.Spec.Git = tc.gitSpec
			repoSync.Spec.Oci = tc.ociSpec
			if tc.helmSpec != nil {
				repoSync.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: *tc.helmSpec}
			}
			repoSync.Spec.SourceType = tc.sourceType
			if repoSync.Spec.SourceType == "" {
				repoSync.Spec.SourceType = configsync.GitSource
			}
			repoSync.Status.Conditions = tc.conditions
			repoSync.Status.Source = tc.sourceStatus
			repoSync.Status.Rendering = tc.renderingStatus
			repoSync.Status.Sync = tc.syncStatus
			got := namespaceRepoStatus(repoSync, tc.resourceGroup, tc.syncingConditionSupported)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(*tc.want)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestRepoState_RootRepoStatus(t *testing.T) {
	stalledCondition := v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncStalled,
		Status:  metav1.ConditionTrue,
		Reason:  "Deployment",
		Message: "deployment failure",
	}

	reconcilingCondition := v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncReconciling,
		Status:  metav1.ConditionTrue,
		Reason:  "Deployment",
		Message: "deployment in progress",
	}

	reconciledCondition := v1beta1.RootSyncCondition{
		Type:   v1beta1.RootSyncReconciling,
		Status: metav1.ConditionFalse,
	}

	syncingTrueCondition := func(commit, msg string) v1beta1.RootSyncCondition {
		return v1beta1.RootSyncCondition{
			Type:    v1beta1.RootSyncSyncing,
			Status:  metav1.ConditionTrue,
			Commit:  commit,
			Message: msg,
		}
	}

	syncingFalseCondition := func(commit string, errorSources []v1beta1.ErrorSource, errorSummary *v1beta1.ErrorSummary) v1beta1.RootSyncCondition {
		return v1beta1.RootSyncCondition{
			Type:            v1beta1.RootSyncSyncing,
			Status:          metav1.ConditionFalse,
			Commit:          commit,
			ErrorSourceRefs: errorSources,
			ErrorSummary:    errorSummary,
		}
	}

	testCases := []struct {
		name                      string
		syncingConditionSupported bool
		gitSpec                   *v1beta1.Git
		conditions                []v1beta1.RootSyncCondition
		sourceStatus              v1beta1.SourceStatus
		renderingStatus           v1beta1.RenderingStatus
		syncStatus                v1beta1.SyncStatus
		want                      *RepoState
	}{
		{
			name:       "fresh installation, namespace reconciler is stalled",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{stalledCondition},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       stalledMsg,
				commit:       emptyCommit,
				errors:       []string{"deployment failure"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "switch repo, namespace reconciler is stalled",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{stalledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       stalledMsg,
				commit:       emptyCommit,
				errors:       []string{"deployment failure"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "fresh installation, namespace reconciler is reconciling",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconcilingCondition},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   reconcilingMsg,
				commit:   emptyCommit,
			},
		},
		{
			name:       "switch repo, namespace reconciler is reconciling",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconcilingCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   reconcilingMsg,
				commit:   emptyCommit,
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] fresh installation, namespace reconciler is importing source configs",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				commit:   emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is importing source configs",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions:                []v1beta1.RootSyncCondition{reconciledCondition},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				commit:   emptyCommit,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is importing source configs",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				// This mistakenly reports an error because `nomos status` checks all errors first.
				// The following test case shows how the status is reported correctly with the syncing condition.
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is importing source configs",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions:                []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   pendingMsg,
				commit:   emptyCommit,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has import error",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   util.ErrorMsg,
				// This mistakenly reports the commit as empty because "nomos status" used
				// to read the commit from .status.sync.commit, which is not available at this point.
				// The test case below shows how the commit is reported successfully via the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has import error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has import error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   util.ErrorMsg,
				// This mistakenly shows the commit because `nomos status` sets commit to `.status.sync.commit`.
				// The test case below shows how it is fixed.
				commit: "abc123",
				// The errors are also wrong because it included a sync error from a previous commit.
				errors:       []string{"KNV2004: import error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has import error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: import error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2004: import error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is rendering new commit",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering in progress",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				// This mistakenly reports an empty commit because `nomos status` sets commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with the syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is rendering new commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "rendering in progress"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering in progress",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				commit:   "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is rendering new commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering in progress",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				// This mistakenly reports an error status because `nomos status` checks all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it still reports an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an old error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is rendering new commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "rendering in progress"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering in progress",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   pendingMsg,
				commit:   "def456",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has rendering error",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   util.ErrorMsg,
				// This mistakenly reports an empty commit because `nomos status` sets the commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has rendering error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has rendering error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   util.ErrorMsg,
				// This mistakenly reports the commit and errors because `nomos status` checks all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				commit: "abc123",
				// The errors are wrong because it includes an apply error from a previous commit.
				errors:       []string{"KNV2015: rendering error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has rendering error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.RenderingError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Errors:  []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2015: rendering error"}},
				Message: "rendering failed",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2015: rendering error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is parsing and validating rendered commit",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				// This mistakenly reports the commit to be empty because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how it is fixed with they syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is parsing and validating rendered commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "rendering succeeded"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering succeeded",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				commit:   "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is parsing and validating rendered commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering succeeded",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				// This mistakenly reports an error status because `nomos status` used to check all errors first.
				// The test case below shows how it is fixed with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it reports to an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an apply error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is parsing and validating rendered commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "rendering succeeded"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering succeeded",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   pendingMsg,
				commit:   "def456",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, repo has parsing or validation error",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   util.ErrorMsg,
				// This mistakenly reports the commit as empty because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit:       emptyCommit,
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has parsing or validation error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, repo has parsing or validation error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   util.ErrorMsg,
				// This mistakenly reports the commit because `nomos status` used to set the commit to `.status.sync.commit`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit: "abc123",
				// The errors are wrong because it includes an apply error from a previous commit.
				errors:       []string{"KNV2004: parsing error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has parsing or validation error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError}, errorSummayWithOneError),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2004: parsing error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV2004: parsing error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] fresh installation, namespace reconciler is syncing validated WET commit",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				// The commit should be available because it is included in the rendering and source statuses,
				// but `nomos status` used to set the commit to be .status.sync.commit, which is `N/A`.
				// The test case below shows how the commit is correctly reported with the syncing condition.
				commit: emptyCommit,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, namespace reconciler is syncing validated WET commit",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("abc123", "Rendering skipped"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "Rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      git,
				status:   pendingMsg,
				commit:   "abc123",
			},
		},
		{
			name:       "[incorrect status expected before syncing condition is supported] switch repo, namespace reconciler is syncing validated WET commit",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				// This mistakenly reports an error status because `nomos status` checks all errors first.
				// The test case below shows how the status is correctly reported with the syncing condition.
				status: util.ErrorMsg,
				// The commit is wrong because it reports an old commit.
				commit: "abc123",
				// The errors are wrong because it reports an apply error from a previous commit.
				errors:       []string{"KNV2009: apply error"},
				errorSummary: errorSummayWithOneError,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, namespace reconciler is syncing validated WET commit",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingTrueCondition("def456", "Rendering skipped"),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "Rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:    "<root>",
				syncName: "root-sync",
				git:      gitUpdated,
				status:   pendingMsg,
				commit:   "def456",
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] fresh installation, repo has non-blocking source error and syncing error",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] fresh installation, repo has non-blocking source error and syncing error",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError}, errorSummayWithTwoErrors),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          git,
				status:       util.ErrorMsg,
				commit:       "abc123",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] switch repo, repo has non-blocking source error and syncing error",
			gitSpec:    gitUpdated,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] switch repo, repo has non-blocking source error and syncing error",
			gitSpec:                   gitUpdated,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("def456", []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError}, errorSummayWithTwoErrors),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(gitUpdated),
				Commit:  "def456",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV1021: non-blocking parse error"}},
			},
			syncStatus: v1beta1.SyncStatus{
				Git:    toGitStatus(gitUpdated),
				Commit: "def456",
				Errors: []v1beta1.ConfigSyncError{{ErrorMessage: "KNV2009: apply error"}},
			},
			want: &RepoState{
				scope:        "<root>",
				syncName:     "root-sync",
				git:          gitUpdated,
				status:       util.ErrorMsg,
				commit:       "def456",
				errors:       []string{"KNV1021: non-blocking parse error", "KNV2009: apply error"},
				errorSummary: errorSummayWithTwoErrors,
			},
		},
		{
			name:       "[accurate status before syncing condition is supported] repo is synced",
			gitSpec:    git,
			conditions: []v1beta1.RootSyncCondition{reconciledCondition},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:        toGitStatus(git),
				Commit:     "abc123",
				LastUpdate: lastSyncTimestamp,
			},
			want: &RepoState{
				scope:             "<root>",
				syncName:          "root-sync",
				git:               git,
				status:            syncedMsg,
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
		},
		{
			name:                      "[accurate status with syncing condition supported] repo is synced",
			gitSpec:                   git,
			syncingConditionSupported: true,
			conditions: []v1beta1.RootSyncCondition{
				reconciledCondition,
				syncingFalseCondition("abc123", nil, nil),
			},
			renderingStatus: v1beta1.RenderingStatus{
				Git:     toGitStatus(git),
				Commit:  "abc123",
				Message: "rendering skipped",
			},
			sourceStatus: v1beta1.SourceStatus{
				Git:    toGitStatus(git),
				Commit: "abc123",
			},
			syncStatus: v1beta1.SyncStatus{
				Git:        toGitStatus(git),
				Commit:     "abc123",
				LastUpdate: lastSyncTimestamp,
			},
			want: &RepoState{
				scope:             "<root>",
				syncName:          "root-sync",
				git:               git,
				status:            syncedMsg,
				lastSyncTimestamp: lastSyncTimestamp,
				commit:            "abc123",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
			rootSync.Spec.Git = tc.gitSpec
			rootSync.Status.Conditions = tc.conditions
			rootSync.Status.Source = tc.sourceStatus
			rootSync.Status.Rendering = tc.renderingStatus
			rootSync.Status.Sync = tc.syncStatus
			got := RootRepoStatus(rootSync, nil, tc.syncingConditionSupported)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(*tc.want)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestClusterState_PrintRows(t *testing.T) {
	testCases := []struct {
		name    string
		cluster *ClusterState
		want    string
	}{
		{
			"cluster without config sync",
			&ClusterState{
				Ref:    "gke_sample-project_europe-west1-b_cluster-1",
				status: "UNINSTALLED",
			},
			`
gke_sample-project_europe-west1-b_cluster-1
  --------------------
  UNINSTALLED	
`,
		},
		{
			"cluster without repos",
			&ClusterState{
				Ref:    "gke_sample-project_europe-west1-b_cluster-1",
				status: "UNCONFIGURED",
				Error:  "Missing git-creds secret",
			},
			`
gke_sample-project_europe-west1-b_cluster-1
  --------------------
  UNCONFIGURED	Missing git-creds secret
`,
		},
		{
			"cluster with Git repos",
			&ClusterState{
				Ref: "gke_sample-project_europe-west1-b_cluster-2",
				repos: []*RepoState{
					{
						scope:      "<root>",
						syncName:   "root-sync",
						sourceType: configsync.GitSource,
						git: &v1beta1.Git{
							Repo: "git@github.com:tester/sample",
						},
						status: "SYNCED",
						commit: "abc123",
					},
					{
						scope:      "bookstore",
						syncName:   "repos-sync",
						sourceType: configsync.GitSource,
						git: &v1beta1.Git{
							Repo:   "git@github.com:tester/sample",
							Branch: "feature",
						},
						status: "SYNCED",
						commit: "abc123",
					},
				},
			},
			`
gke_sample-project_europe-west1-b_cluster-2
  --------------------
  <root>:root-sync	git@github.com:tester/sample@master	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
  --------------------
  bookstore:repos-sync	git@github.com:tester/sample@feature	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
`,
		},
		{
			"cluster with Oci image",
			&ClusterState{
				Ref: "gke_sample-project_europe-west1-b_cluster-2",
				repos: []*RepoState{
					{
						scope:      "<root>",
						syncName:   "root-sync",
						sourceType: configsync.OciSource,
						oci: &v1beta1.Oci{
							Image: "us-docker.pkg.dev/test-project/test-ar-repo/sample",
						},
						status: "SYNCED",
						commit: "abc123",
					},
					{
						scope:      "bookstore",
						syncName:   "repos-sync",
						sourceType: configsync.OciSource,
						oci: &v1beta1.Oci{
							Image: "us-docker.pkg.dev/test-project/test-ar-repo/sample-repo",
							Dir:   "test",
						},
						status: "SYNCED",
						commit: "abc123",
					},
				},
			},
			`
gke_sample-project_europe-west1-b_cluster-2
  --------------------
  <root>:root-sync	us-docker.pkg.dev/test-project/test-ar-repo/sample	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
  --------------------
  bookstore:repos-sync	us-docker.pkg.dev/test-project/test-ar-repo/sample-repo/test	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
`,
		},
		{
			"cluster with Helm chart",
			&ClusterState{
				Ref: "gke_sample-project_europe-west1-b_cluster-2",
				repos: []*RepoState{
					{
						scope:      "<root>",
						syncName:   "root-sync",
						sourceType: configsync.HelmSource,
						helm: &v1beta1.HelmBase{
							Repo:  "oci://us-central1-docker.pkg.dev/your-dev-project/sample",
							Chart: "test",
						},
						status: "SYNCED",
						commit: "abc123",
					},
					{
						scope:      "bookstore",
						syncName:   "repos-sync",
						sourceType: configsync.HelmSource,
						helm: &v1beta1.HelmBase{
							Repo:    "oci://us-central1-docker.pkg.dev/your-dev-project/sample",
							Chart:   "test",
							Version: "0.2.0",
						},
						status: "SYNCED",
						commit: "abc123",
					},
				},
			},
			`
gke_sample-project_europe-west1-b_cluster-2
  --------------------
  <root>:root-sync	oci://us-central1-docker.pkg.dev/your-dev-project/sample/test:latest	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
  --------------------
  bookstore:repos-sync	oci://us-central1-docker.pkg.dev/your-dev-project/sample/test:0.2.0	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			tc.cluster.printRows(&buffer)
			got := buffer.String()
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestClusterState_PrintRowsWithNameFilter(t *testing.T) {
	testCases := []struct {
		name    string
		cluster *ClusterState
		want    string
	}{
		{
			"cluster with multiple root sync",
			&ClusterState{
				Ref: "gke_sample-project_europe-west1-b_cluster-2",
				repos: []*RepoState{
					{
						scope:      "<root>",
						syncName:   "root-sync",
						sourceType: configsync.GitSource,
						git: &v1beta1.Git{
							Repo: "git@github.com:tester/sample",
						},
						status: "SYNCED",
						commit: "abc123",
					},
					{
						scope:      "<root>",
						syncName:   "root-sync-2",
						sourceType: configsync.GitSource,
						git: &v1beta1.Git{
							Repo:   "git@github.com:tester/sample",
							Branch: "feature",
						},
						status: "SYNCED",
						commit: "abc123",
					},
					{
						scope:      "<root>",
						syncName:   "root-sync-3",
						sourceType: configsync.GitSource,
						git: &v1beta1.Git{
							Repo:   "git@github.com:tester/sample",
							Branch: "dev",
						},
						status: "SYNCED",
						commit: "abc123",
					},
				},
			},
			`
gke_sample-project_europe-west1-b_cluster-2
  --------------------
  <root>:root-sync-2	git@github.com:tester/sample@feature	
  SYNCED @ 0001-01-01 00:00:00 +0000 UTC	abc123	
`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			name = "root-sync-2"
			tc.cluster.printRows(&buffer)
			got := buffer.String()
			if got != tc.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func withResources() core.MetaMutator {
	status := map[string]interface{}{
		"resourceStatuses": []interface{}{
			map[string]interface{}{
				"group":     "apps",
				"kind":      "Deployment",
				"namespace": "bookstore",
				"name":      "test",
				"status":    "Current",
			},
			map[string]interface{}{
				"kind":      "Service",
				"namespace": "bookstore",
				"name":      "test",
				"status":    "Failed",
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "Stalled",
						"status":  "True",
						"message": "A detailed message explaining the current condition.",
					},
				},
			},
			map[string]interface{}{
				"kind":      "Service",
				"namespace": "bookstore",
				"name":      "test2",
				"status":    "Current",
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "OwnershipOverlap",
						"status":  "True",
						"message": "A detailed message explaining why it is in the status ownership overlap.",
					},
				},
			},
		},
	}
	return func(o client.Object) {
		u := o.(*unstructured.Unstructured)
		unstructured.SetNestedField(u.Object, status, "status") //nolint
	}
}

func withResourcesAndCommit(commit string) core.MetaMutator {
	status := map[string]interface{}{
		"resourceStatuses": []interface{}{
			map[string]interface{}{
				"group":      "apps",
				"kind":       "Deployment",
				"namespace":  "bookstore",
				"name":       "test",
				"status":     "Current",
				"sourceHash": commit,
			},
			map[string]interface{}{
				"kind":       "Service",
				"namespace":  "bookstore",
				"name":       "test",
				"status":     "Failed",
				"sourceHash": commit,
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "Stalled",
						"status":  "True",
						"message": "A detailed message explaining the current condition.",
					},
				},
			},
			map[string]interface{}{
				"kind":       "Service",
				"namespace":  "bookstore",
				"name":       "test2",
				"status":     "Current",
				"sourceHash": commit,
				"conditions": []interface{}{
					map[string]interface{}{
						"type":    "OwnershipOverlap",
						"status":  "True",
						"message": "A detailed message explaining why it is in the status ownership overlap.",
					},
				},
			},
		},
	}
	return func(o client.Object) {
		u := o.(*unstructured.Unstructured)
		unstructured.SetNestedField(u.Object, status, "status") //nolint
	}
}

func exampleResources(commit string) []resourceState {
	return []resourceState{
		{
			Group:      "apps",
			Kind:       "Deployment",
			Namespace:  "bookstore",
			Name:       "test",
			Status:     "Current",
			SourceHash: commit,
		},
		{
			Group:      "",
			Kind:       "Service",
			Namespace:  "bookstore",
			Name:       "test",
			Status:     "Failed",
			SourceHash: commit,
			Conditions: []Condition{{
				Type:    "Stalled",
				Status:  "True",
				Message: "A detailed message explaining the current condition.",
			}},
		},
		{
			Group:      "",
			Kind:       "Service",
			Namespace:  "bookstore",
			Name:       "test2",
			Status:     "Conflict",
			SourceHash: commit,
			Conditions: []Condition{{
				Type:    "OwnershipOverlap",
				Status:  "True",
				Message: "A detailed message explaining why it is in the status ownership overlap.",
			}},
		},
	}
}
