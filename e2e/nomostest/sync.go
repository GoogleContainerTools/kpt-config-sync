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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/resourcegroup"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/util/log"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RootSyncHasStatusSyncPath creates a Predicate that ensures that the
// .status.sync.gitStatus.dir field on the passed RootSync matches the provided dir.
func RootSyncHasStatusSyncPath(syncPath string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("%s status.conditions[%d] is True: %s:\n%s",
					configsync.RootSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("%s status.conditions[%d] contains errors: %s:\n%s",
					configsync.RootSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
		}
		err := statusHasSyncDirAndNoErrors(rs.Status.Status, rs.Spec.SourceType, syncPath)
		if err != nil {
			return fmt.Errorf("%s %w:\n%s", configsync.RootSyncKind, err, log.AsYAML(rs))
		}
		return nil
	}
}

// RepoSyncHasStatusSyncPath creates a Predicate that ensures that the
// .status.sync.gitStatus.dir field on the passed RepoSync matches the provided dir.
func RepoSyncHasStatusSyncPath(syncPath string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RepoSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("%s status.conditions[%d] is True: %s:\n%s",
					configsync.RepoSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("%s status.conditions[%d] contains errors: %s:\n%s",
					configsync.RepoSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
		}
		err := statusHasSyncDirAndNoErrors(rs.Status.Status, rs.Spec.SourceType, syncPath)
		if err != nil {
			return fmt.Errorf("%s %w:\n%s", configsync.RepoSyncKind, err, log.AsYAML(rs))
		}
		return nil
	}
}

// RootSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RootSync matches sha1.
func RootSyncHasStatusSyncCommit(sha1 string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RootSync{})
		}

		// Ensure the reconciler is ready (no true or error condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("%s status.conditions[%d] is True: %s:\n%s",
					configsync.RootSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("%s status.conditions[%d] contains errors: %s:\n%s",
					configsync.RootSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
		}

		if err := statusHasSyncCommitAndNoErrors(rs.Status.Status, sha1); err != nil {
			return fmt.Errorf("%s %w:\n%s", configsync.RootSyncKind, err, log.AsYAML(rs))
		}
		syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("%s status.conditions['Syncing'].commit %q does not match git revision %q:\n%s",
				configsync.RootSyncKind, syncingCondition.Commit, sha1, log.AsYAML(rs))
		}
		return nil
	}
}

// RepoSyncHasStatusSyncCommit creates a Predicate that ensures that the
// .status.sync.commit field on the passed RepoSync matches sha1.
func RepoSyncHasStatusSyncCommit(sha1 string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RepoSync{})
		}

		// Ensure the reconciler is ready (no true condition).
		for i, condition := range rs.Status.Conditions {
			if condition.Status == metav1.ConditionTrue {
				return fmt.Errorf("%s status.conditions[%d] is True: %s:\n%s",
					configsync.RepoSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
			if condition.ErrorSummary != nil && condition.ErrorSummary.TotalCount > 0 {
				return fmt.Errorf("%s status.conditions[%d] contains errors: %s:\n%s",
					configsync.RepoSyncKind, i, log.AsJSON(condition), log.AsYAML(rs))
			}
		}
		if err := statusHasSyncCommitAndNoErrors(rs.Status.Status, sha1); err != nil {
			return fmt.Errorf("%s %w:\n%s", configsync.RepoSyncKind, err, log.AsYAML(rs))
		}
		syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
		if syncingCondition != nil && syncingCondition.Commit != sha1 {
			return fmt.Errorf("%s status.conditions['Syncing'].commit %q does not match git revision %q:\n%s",
				configsync.RepoSyncKind, syncingCondition.Commit, sha1, log.AsYAML(rs))
		}
		return nil
	}
}

func statusHasSyncCommitAndNoErrors(status v1beta1.Status, commit string) error {
	if status.Source.ErrorSummary != nil && status.Source.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.source contains %d errors", status.Source.ErrorSummary.TotalCount)
	}
	if srcCommit := status.Source.Commit; srcCommit != commit {
		return fmt.Errorf("status.source.commit %q does not match git revision %q", srcCommit, commit)
	}
	if status.Sync.ErrorSummary != nil && status.Sync.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.sync contains %d errors", status.Sync.ErrorSummary.TotalCount)
	}
	if syncCommit := status.Sync.Commit; syncCommit != commit {
		return fmt.Errorf("status.sync.commit %q does not match git revision %q", syncCommit, commit)
	}
	if status.Rendering.ErrorSummary != nil && status.Rendering.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.rendering contains %d errors", status.Rendering.ErrorSummary.TotalCount)
	}
	if renderCommit := status.Rendering.Commit; renderCommit != commit {
		return fmt.Errorf("status.rendering.commit %q does not match git revision %q", renderCommit, commit)
	}
	if message := status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
		return fmt.Errorf("status.rendering.message %q does not indicate a successful state", message)
	}
	if lastSyncedCommit := status.LastSyncedCommit; lastSyncedCommit != commit {
		return fmt.Errorf("status.lastSyncedCommit %q does not match commit hash %q", lastSyncedCommit, commit)
	}
	return nil
}

func statusHasSyncDirAndNoErrors(status v1beta1.Status, sourceType configsync.SourceType, syncPath string) error {
	if status.Source.ErrorSummary != nil && status.Source.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.source contains %d errors", status.Source.ErrorSummary.TotalCount)
	}
	if status.Sync.ErrorSummary != nil && status.Sync.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.sync contains %d errors", status.Sync.ErrorSummary.TotalCount)
	}
	if status.Rendering.ErrorSummary != nil && status.Rendering.ErrorSummary.TotalCount > 0 {
		return fmt.Errorf("status.rendering contains %d errors", status.Rendering.ErrorSummary.TotalCount)
	}
	if message := status.Rendering.Message; message != parse.RenderingSucceeded && message != parse.RenderingSkipped {
		return fmt.Errorf("status.rendering.message %q does not indicate a successful state", message)
	}
	switch sourceType {
	case configsync.OciSource:
		if status.Source.Oci == nil {
			return fmt.Errorf("status.source.oci is nil")
		}
		if ociDir := status.Source.Oci.Dir; ociDir != syncPath {
			return fmt.Errorf("status.source.oci.dir %q does not match the expected directory %q", ociDir, syncPath)
		}
		if status.Sync.Oci == nil {
			return fmt.Errorf("status.sync.oci is nil")
		}
		if ociDir := status.Sync.Oci.Dir; ociDir != syncPath {
			return fmt.Errorf("status.sync.oci.dir %q does not match the expected directory %q", ociDir, syncPath)
		}
		if status.Rendering.Oci == nil {
			return fmt.Errorf("status.rendering.oci is nil")
		}
		if ociDir := status.Rendering.Oci.Dir; ociDir != syncPath {
			return fmt.Errorf("status.rendering.oci.dir %q does not match the expected directory %q", ociDir, syncPath)
		}
	case configsync.GitSource:
		if status.Source.Git == nil {
			return fmt.Errorf("status.source.git is nil")
		}
		if gitDir := status.Source.Git.Dir; gitDir != syncPath {
			return fmt.Errorf("status.source.git.dir %q does not match the expected directory %q", gitDir, syncPath)
		}
		if status.Sync.Git == nil {
			return fmt.Errorf("status.sync.git is nil")
		}
		if gitDir := status.Sync.Git.Dir; gitDir != syncPath {
			return fmt.Errorf("status.sync.git.dir %q does not match the expected directory %q", gitDir, syncPath)
		}
		if status.Rendering.Git == nil {
			return fmt.Errorf("status.rendering.git is nil")
		}
		if gitDir := status.Rendering.Git.Dir; gitDir != syncPath {
			return fmt.Errorf("status.rendering.git.dir %q does not match the expected directory %q", gitDir, syncPath)
		}
	case configsync.HelmSource:
		if status.Source.Helm == nil {
			return fmt.Errorf("status.source.helm is nil")
		}
		if helmChart := status.Source.Helm.Chart; helmChart != syncPath {
			return fmt.Errorf("status.source.helm.chart %q does not match the expected chart %q", helmChart, syncPath)
		}
		if status.Sync.Helm == nil {
			return fmt.Errorf("status.sync.helm is nil")
		}
		if helmChart := status.Sync.Helm.Chart; helmChart != syncPath {
			return fmt.Errorf("status.sync.helm.chart %q does not match the expected chart %q", helmChart, syncPath)
		}
		if status.Rendering.Helm == nil {
			return fmt.Errorf("status.rendering.helm is nil")
		}
		if helmChart := status.Rendering.Helm.Chart; helmChart != syncPath {
			return fmt.Errorf("status.rendering.helm.chart %q does not match the expected chart %q", helmChart, syncPath)
		}
	}
	return nil
}

func resourceGroupHasReconciled(commit string, scheme *runtime.Scheme) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rg, ok := o.(*v1alpha1.ResourceGroup)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1alpha1.ResourceGroup{})
		}

		// If status is disabled for this ResourceGroup, skip all status checks
		if resourcegroup.IsStatusDisabled(rg) {
			return testpredicates.ResourceGroupHasNoStatus()(o)
		}

		// Test status.observedGeneration matches metadata.generation
		if err := testpredicates.HasObservedLatestGeneration(scheme)(o); err != nil {
			return err
		}

		// Test that metadata.deletionTimestamp is missing, and conditions do not include Reconciling=True or Stalled=True
		if err := testpredicates.StatusEquals(scheme, kstatus.CurrentStatus)(o); err != nil {
			return err
		}

		// Test that Stalled condition exists and is "False".
		// This ensures the condition wasn't removed by the applier.
		if err := testpredicates.HasConditionStatus(scheme, string(v1alpha1.Stalled), corev1.ConditionFalse)(o); err != nil {
			return err
		}

		// Test that all the spec.resources exist in the status.resourceStatuses,
		// and that all deleted objects were removed by the applier.
		shortCommit := resourcegroup.TruncateSourceHash(commit)
		found := rg.Status.ResourceStatuses
		expected := make([]v1alpha1.ResourceStatus, len(rg.Spec.Resources))
		for i, resource := range rg.Spec.Resources {
			expected[i] = v1alpha1.ResourceStatus{
				ObjMetadata: *resource.DeepCopy(),
				Status:      v1alpha1.Current,
				Strategy:    v1alpha1.Apply,
				Actuation:   v1alpha1.ActuationSucceeded,
				Reconcile:   v1alpha1.ReconcileSucceeded,
				SourceHash:  shortCommit,
				Conditions:  nil,
			}
		}
		if !equality.Semantic.DeepEqual(found, expected) {
			return fmt.Errorf("status.resourceStatuses does not match expected value in %s:\nDiff (- Expected, + Found)\n%s",
				kinds.ObjectSummary(o), log.AsYAMLDiff(expected, found))
		}

		// Note: status.subgroupStatuses is ignored
		return nil
	}
}
