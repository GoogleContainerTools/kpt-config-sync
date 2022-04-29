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
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RepoHasStatusSyncLatestToken ensures ACM has reported all objects were
// successfully synced to the repository.
func RepoHasStatusSyncLatestToken(sha1 string) Predicate {
	return func(o client.Object) error {
		repo, ok := o.(*v1.Repo)
		if !ok {
			return WrongTypeErr(o, &v1.Repo{})
		}

		if len(repo.Status.Source.Errors) > 0 {
			return fmt.Errorf("status.source.errors contains errors: %+v", repo.Status.Source.Errors)
		}
		if len(repo.Status.Import.Errors) > 0 {
			return fmt.Errorf("status.source.errors contains errors: %+v", repo.Status.Import.Errors)
		}

		// Ensure there aren't any pending changes to sync.
		if len(repo.Status.Sync.InProgress) > 0 {
			return fmt.Errorf("status.sync.inProgress contains changes that haven't been synced: %+v",
				repo.Status.Sync.InProgress)
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
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// On error, display the full state of the RootSync to aid in debugging.
		jsn, err := json.MarshalIndent(rs, "", "  ")
		if err != nil {
			return err
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
		}

		if rs.Status.Source.ErrorSummary != nil && rs.Status.Source.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.source contains %d errors:\n%s", rs.Status.Source.ErrorSummary.TotalCount, string(jsn))
		}
		if gitDir := rs.Status.Source.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.source.gitStatus.dir %q does not match the provided directory %q:\n%s", gitDir, dir, string(jsn))
		}
		if rs.Status.Sync.ErrorSummary != nil && rs.Status.Sync.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.sync contains %d errors:\n%s", rs.Status.Sync.ErrorSummary.TotalCount, string(jsn))
		}
		if gitDir := rs.Status.Sync.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.sync.gitStatus.dir %q does not match the provided directory %q:\n%s", gitDir, dir, string(jsn))
		}
		if rs.Status.Rendering.ErrorSummary != nil && rs.Status.Rendering.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.rendering contains %d errors:\n%s", rs.Status.Rendering.ErrorSummary.TotalCount, string(jsn))
		}
		if gitDir := rs.Status.Rendering.Git.Dir; gitDir != dir {
			return fmt.Errorf("status.rendering.gitStatus.dir %q does not match the provided directory %q:\n%s", gitDir, dir, string(jsn))
		}
		if message := rs.Status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
			return fmt.Errorf("status.rendering.message %q does not indicate a successful state:\n%s", message, string(jsn))
		}
		return nil
	}
}

// RootSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RootSync matches sha1.
func RootSyncHasStatusSyncCommit(sha1 string) Predicate {
	return func(o client.Object) error {
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// On error, display the full state of the RootSync to aid in debugging.
		jsn, err := json.MarshalIndent(rs, "", "  ")
		if err != nil {
			return err
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
		}

		if rs.Status.Source.ErrorSummary != nil && rs.Status.Source.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.source contains %d errors:\n%s", rs.Status.Source.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Source.Commit; commit != sha1 {
			return fmt.Errorf("status.source.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if rs.Status.Sync.ErrorSummary != nil && rs.Status.Sync.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.sync contains %d errors:\n%s", rs.Status.Sync.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Sync.Commit; commit != sha1 {
			return fmt.Errorf("status.sync.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if rs.Status.Rendering.ErrorSummary != nil && rs.Status.Rendering.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.rendering contains %d errors:\n%s", rs.Status.Rendering.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Rendering.Commit; commit != sha1 {
			return fmt.Errorf("status.rendering.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if message := rs.Status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
			return fmt.Errorf("status.rendering.message %q does not indicate a successful state:\n%s", message, string(jsn))
		}
		if commit := rs.Status.LastSyncedCommit; commit != sha1 {
			return fmt.Errorf("status.lastSyncedCommit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("status.conditions['Syncing'].commit %q does not match git revision %q:\n%s", syncingCondition.Commit, sha1, string(jsn))
		}
		return nil
	}
}

// RepoSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RepoSync matches sha1.
func RepoSyncHasStatusSyncCommit(sha1 string) Predicate {
	return func(o client.Object) error {
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return WrongTypeErr(o, &v1beta1.RepoSync{})
		}

		jsn, err := json.MarshalIndent(rs, "", "  ")
		if err != nil {
			return err
		}

		// Ensure the reconciler is ready (no true condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("status.conditions[%d](%s) contains status: %s, reason: %s, message: %s, commit: %s, errorsSourceRefs: %v, errorSummary: %v\n%s",
					i, condition.Type, condition.Status, condition.Reason, condition.Message, condition.Commit, condition.ErrorSourceRefs, condition.ErrorSummary, string(jsn))
			}
		}

		if rs.Status.Source.ErrorSummary != nil && rs.Status.Source.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.source contains %d errors:\n%s", rs.Status.Source.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Source.Commit; commit != sha1 {
			return fmt.Errorf("status.source.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if rs.Status.Sync.ErrorSummary != nil && rs.Status.Sync.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.sync contains %d errors:\n%s", rs.Status.Sync.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Sync.Commit; commit != sha1 {
			return fmt.Errorf("status.sync.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if rs.Status.Rendering.ErrorSummary != nil && rs.Status.Rendering.ErrorSummary.TotalCount > 0 {
			return fmt.Errorf("status.rendering contains %d errors:\n%s", rs.Status.Rendering.ErrorSummary.TotalCount, string(jsn))
		}
		if commit := rs.Status.Rendering.Commit; commit != sha1 {
			return fmt.Errorf("status.rendering.commit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		if message := rs.Status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
			return fmt.Errorf("status.rendering.message %q does not indicate a successful state:\n%s", message, string(jsn))
		}
		if commit := rs.Status.LastSyncedCommit; commit != sha1 {
			return fmt.Errorf("status.lastSyncedCommit %q does not match git revision %q:\n%s", commit, sha1, string(jsn))
		}
		syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("status.conditions['Syncing'].commit %q does not match git revision %q:\n%s", syncingCondition.Commit, sha1, string(jsn))
		}
		return nil
	}
}
