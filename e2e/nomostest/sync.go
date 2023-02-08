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

package nomostest

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SyncInProgressError wraps a lists of changes with sync still in progress.
type SyncInProgressError struct {
	Repo *v1.Repo
}

// NewSyncInProgressError constructs a new SyncInProgressError
func NewSyncInProgressError(repo *v1.Repo) *SyncInProgressError {
	return &SyncInProgressError{
		Repo: repo,
	}
}

// Error constructs an error string
func (sipe *SyncInProgressError) Error() string {
	return fmt.Sprintf("status.sync.inProgress contains changes that haven't been synced: %s\nRepo: %s",
		log.AsJSON(sipe.Repo.Status.Sync.InProgress), log.AsJSON(sipe.Repo))
}

// MonoRepoSyncNotInProgress ensures the Repo does not have a sync in-progress.
func MonoRepoSyncNotInProgress(o client.Object) error {
	if o == nil {
		return ErrObjectNotFound
	}
	repo, ok := o.(*v1.Repo)
	if !ok {
		return WrongTypeErr(o, &v1.Repo{})
	}
	// Ensure there aren't any pending changes to sync.
	if len(repo.Status.Sync.InProgress) > 0 {
		return NewSyncInProgressError(repo)
	}
	return nil
}

// RepoHasStatusSyncLatestToken ensures ACM has reported all objects were
// successfully synced to the repository.
func RepoHasStatusSyncLatestToken(sha1 string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		repo, ok := o.(*v1.Repo)
		if !ok {
			return WrongTypeErr(o, &v1.Repo{})
		}

		if len(repo.Status.Source.Errors) > 0 {
			return fmt.Errorf("status.source.errors contains errors: %s\nRepo: %s",
				log.AsJSON(repo.Status.Source.Errors), log.AsJSON(repo))
		}
		if len(repo.Status.Import.Errors) > 0 {
			return fmt.Errorf("status.import.errors contains errors: %s\nRepo: %s",
				log.AsJSON(repo.Status.Import.Errors), log.AsJSON(repo))
		}

		// Ensure there aren't any pending changes to sync.
		if len(repo.Status.Sync.InProgress) > 0 {
			return NewSyncInProgressError(repo)
		}

		// Check the Sync.LatestToken as:
		// 1) Source.LatestToken is the most-recently-cloned hash of the git repository.
		//      It just means we've seen the update to the repository, but haven't
		//      updated the state of any objects on the cluster.
		// 2) Import.LatestToken is updated once we've successfully written the
		//      declared objects to ClusterConfigs/NamespaceConfigs, but haven't
		//      necessarily applied them to the cluster successfully.
		// 3) Sync.LatestToken is updated once we've updated the state of all
		//      objects on the cluster to match their declared states, so this is
		//      the one we want.
		if token := repo.Status.Sync.LatestToken; token != sha1 {
			return fmt.Errorf("status.sync.latestToken %q does not match git revision %q",
				token, sha1)
		}
		return nil
	}
}

// ClusterConfigHasToken created a Predicate that ensures .spec.token and
// .status.token on the passed ClusterConfig matches sha1.
//
// This means ACM has successfully synced all cluster-scoped objects from the
// latest repo commit to the cluster.
func ClusterConfigHasToken(sha1 string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		cc, ok := o.(*v1.ClusterConfig)
		if !ok {
			return WrongTypeErr(o, &v1.ClusterConfig{})
		}

		if token := cc.Spec.Token; token != sha1 {
			return fmt.Errorf("spec.token %q does not match git revision %q",
				token, sha1)
		}
		if token := cc.Status.Token; token != sha1 {
			return fmt.Errorf("status.token %q does not match git revision %q",
				token, sha1)
		}
		return nil
	}
}

// RootSyncHasStatusSyncDirectory creates a Predicate that ensures that the
// .status.sync.gitStatus.dir field on the passed RootSync matches the provided dir.
func RootSyncHasStatusSyncDirectory(dir string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d] is True: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RootSyncKind, log.AsJSON(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d] contains errors: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RootSyncKind, log.AsJSON(rs))
			}
		}
		return statusHasSyncDirAndNoErrors(rs.Status.Status, v1beta1.SourceType(rs.Spec.SourceType), dir, configsync.RootSyncKind, rs)
	}
}

// RepoSyncHasStatusSyncDirectory creates a Predicate that ensures that the
// .status.sync.gitStatus.dir field on the passed RepoSync matches the provided dir.
func RepoSyncHasStatusSyncDirectory(dir string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d] is True: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RepoSyncKind, log.AsJSON(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d] contains errors: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RepoSyncKind, log.AsJSON(rs))
			}
		}
		return statusHasSyncDirAndNoErrors(rs.Status.Status, v1beta1.SourceType(rs.Spec.SourceType), dir, configsync.RepoSyncKind, rs)
	}
}

// RootSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RootSync matches sha1.
func RootSyncHasStatusSyncCommit(sha1 string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d] is True: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RootSyncKind, log.AsJSON(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d] contains errors: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RootSyncKind, log.AsJSON(rs))
			}
		}

		if err := statusHasSyncCommitAndNoErrors(rs.Status.Status, sha1, configsync.RootSyncKind, rs); err != nil {
			return err
		}
		syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("status.conditions['Syncing'].commit %q does not match git revision %q:\n%s: %s",
				syncingCondition.Commit, sha1, configsync.RootSyncKind, log.AsJSON(rs))
		}
		return nil
	}
}

// RepoSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RepoSync matches sha1.
func RepoSyncHasStatusSyncCommit(sha1 string) Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}

		// Ensure the reconciler is ready (no true condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d] is True: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RepoSyncKind, log.AsJSON(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d] contains errors: %s\n%s: %s",
					i, log.AsJSON(condition), configsync.RepoSyncKind, log.AsJSON(rs))
			}
		}
		if err := statusHasSyncCommitAndNoErrors(rs.Status.Status, sha1, configsync.RepoSyncKind, rs); err != nil {
			return err
		}
		syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("status.conditions['Syncing'].commit %q does not match git revision %q:\n%s: %s",
				syncingCondition.Commit, sha1, configsync.RepoSyncKind, log.AsJSON(rs))
		}
		return nil
	}
}

func statusHasSyncCommitAndNoErrors(status v1beta1.Status, sha1, kind string, rs client.Object) error {
	if status.Source.ErrorSummary != nil && status.Source.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.source contains %d errors:\n%s: %s", status.Source.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if commit := status.Source.Commit; commit != sha1 {
		return fmt.Errorf("status.source.commit %q does not match git revision %q:\n%s: %s", commit, sha1, kind, log.AsJSON(rs))
	}
	if status.Sync.ErrorSummary != nil && status.Sync.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.sync contains %d errors:\n%s: %s", status.Sync.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if commit := status.Sync.Commit; commit != sha1 {
		return fmt.Errorf("status.sync.commit %q does not match git revision %q:\n%s: %s", commit, sha1, kind, log.AsJSON(rs))
	}
	if status.Rendering.ErrorSummary != nil && status.Rendering.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.rendering contains %d errors:\n%s: %s", status.Rendering.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if commit := status.Rendering.Commit; commit != sha1 {
		return fmt.Errorf("status.rendering.commit %q does not match git revision %q:\n%s: %s", commit, sha1, kind, log.AsJSON(rs))
	}
	if message := status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
		return fmt.Errorf("status.rendering.message %q does not indicate a successful state:\n%s: %s", message, kind, log.AsJSON(rs))
	}
	if commit := status.LastSyncedCommit; commit != sha1 {
		return fmt.Errorf("status.lastSyncedCommit %q does not match commit hash %q:\n%s: %s", commit, sha1, kind, log.AsJSON(rs))
	}
	return nil
}

func statusHasSyncDirAndNoErrors(status v1beta1.Status, sourceType v1beta1.SourceType, dir, kind string, rs client.Object) error {
	if status.Source.ErrorSummary != nil && status.Source.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.source contains %d errors:\n%s: %s", status.Source.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if status.Sync.ErrorSummary != nil && status.Sync.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.sync contains %d errors:\n%s: %s", status.Sync.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if status.Rendering.ErrorSummary != nil && status.Rendering.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.rendering contains %d errors:\n%s: %s", status.Rendering.ErrorSummary.TotalCount, kind, log.AsJSON(rs))
	}
	if message := status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
		return fmt.Errorf("status.rendering.message %q does not indicate a successful state:\n%s: %s", message, kind, log.AsJSON(rs))
	}
	switch sourceType {
	case v1beta1.OciSource:
		if ociDir := status.Source.Oci.Dir; ociDir != dir {
			return fmt.Errorf("status.source.ociStatus.dir %q does not match the provided directory %q:\n%s: %s", ociDir, dir, kind, log.AsJSON(rs))
		}
		if ociDir := status.Sync.Oci.Dir; ociDir != dir {
			return fmt.Errorf("status.sync.ociStatus.dir %q does not match the provided directory %q:\n%s: %s", ociDir, dir, kind, log.AsJSON(rs))
		}
		if ociDir := status.Rendering.Oci.Dir; ociDir != dir {
			return fmt.Errorf("status.rendering.ociStatus.dir %q does not match the provided directory %q:\n%s: %s", ociDir, dir, kind, log.AsJSON(rs))
		}
	case v1beta1.GitSource:
		if gitDir := status.Source.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.source.gitStatus.dir %q does not match the provided directory %q:\n%s: %s", gitDir, dir, kind, log.AsJSON(rs))
		}
		if gitDir := status.Sync.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.sync.gitStatus.dir %q does not match the provided directory %q:\n%s: %s", gitDir, dir, kind, log.AsJSON(rs))
		}
		if gitDir := status.Rendering.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.rendering.gitStatus.dir %q does not match the provided directory %q:\n%s: %s", gitDir, dir, kind, log.AsJSON(rs))
		}
	case v1beta1.HelmSource:
		if helmChart := status.Source.Helm.Chart; helmChart != dir {
			return fmt.Errorf("status.source.helmStatus.chart %q does not match the provided chart %q:\n%s: %s", helmChart, dir, kind, log.AsJSON(rs))
		}
		if helmChart := status.Sync.Helm.Chart; helmChart != dir {
			return fmt.Errorf("status.sync.helmStatus.chart %q does not match the provided chart %q:\n%s: %s", helmChart, dir, kind, log.AsJSON(rs))
		}
		if helmChart := status.Rendering.Helm.Chart; helmChart != dir {
			return fmt.Errorf("status.rendering.helmStatus.chart %q does not match the provided chart %q:\n%s: %s", helmChart, dir, kind, log.AsJSON(rs))
		}
	}
	return nil
}
