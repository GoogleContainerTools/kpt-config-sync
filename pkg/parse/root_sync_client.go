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

package parse

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/compare"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RootOptions includes options specific to RootSync objects.
type RootOptions struct {
	// Extend parse.Options
	*Options

	// SourceFormat defines the structure of the Root repository. Only the Root
	// repository may be SourceFormatHierarchy; all others are implicitly
	// SourceFormatUnstructured.
	SourceFormat configsync.SourceFormat

	// NamespaceStrategy indicates the NamespaceStrategy to be used by this
	// reconciler.
	NamespaceStrategy configsync.NamespaceStrategy

	// DynamicNSSelectorEnabled represents whether the NamespaceSelector's dynamic
	// mode is enabled. If it is enabled, NamespaceSelector will also select
	// resources matching the on-cluster Namespaces.
	// Only Root reconciler may have dynamic NamespaceSelector enabled because
	// RepoSync can't manage NamespaceSelectors.
	DynamicNSSelectorEnabled bool

	// NSControllerState stores whether the Namespace Controller schedules a sync
	// event for the reconciler thread, along with the cached NamespaceSelector
	// and selected namespaces.
	// Only Root reconciler may have Namespace Controller state because
	// RepoSync can't manage NamespaceSelectors.
	NSControllerState *namespacecontroller.State
}

type rootSyncStatusClient struct {
	options *Options
	// mux prevents status update conflicts.
	mux sync.Mutex
}

// SetSourceStatus implements the Parser interface
func (p *rootSyncStatusClient) SetSourceStatus(ctx context.Context, newStatus *SourceStatus) status.Error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSourceStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *rootSyncStatusClient) setSourceStatusWithRetries(ctx context.Context, newStatus *SourceStatus, denominator int) status.Error {
	if denominator <= 0 {
		return status.InternalErrorf("denominator must be positive: %d", denominator)
	}
	opts := p.options

	var rs v1beta1.RootSync
	if err := opts.Client.Get(ctx, rootsync.ObjectKey(opts.SyncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	currentRS := rs.DeepCopy()

	setSourceStatusFields(&rs.Status.Source, newStatus, denominator)

	continueSyncing := (rs.Status.Source.ErrorSummary.TotalCount == 0)
	var errorSource []v1beta1.ErrorSource
	if len(rs.Status.Source.Errors) > 0 {
		errorSource = []v1beta1.ErrorSource{v1beta1.SourceError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Source", "Source", newStatus.Commit, errorSource, rs.Status.Source.ErrorSummary, newStatus.LastUpdate)

	// Avoid unnecessary status updates.
	if !currentRS.Status.Source.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping source status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.Errs)
	metrics.RecordReconcilerErrors(ctx, "source", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "source", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New source errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}

	if klog.V(5).Enabled() {
		klog.V(5).Infof("Updating source status:\nDiff (- Removed, + Added):\n%s",
			cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := opts.Client.Status().Update(ctx, &rs, client.FieldOwner(configsync.FieldManager)); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync source status (total error count: %d, denominator: %d): %s.", rs.Status.Source.ErrorSummary.TotalCount, denominator, err)
			return p.setSourceStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync source status from parser")
	}
	return nil
}

func setSourceStatusFields(source *v1beta1.SourceStatus, newStatus *SourceStatus, denominator int) {
	cse := status.ToCSE(newStatus.Errs)
	source.Commit = newStatus.Commit
	switch newSourceSpec := newStatus.Spec.(type) {
	case GitSourceSpec:
		source.Git = &v1beta1.GitStatus{
			Repo:     newSourceSpec.Repo,
			Revision: newSourceSpec.Revision,
			Branch:   newSourceSpec.Branch,
			Dir:      newSourceSpec.Dir,
		}
		source.Oci = nil
		source.Helm = nil
	case OCISourceSpec:
		source.Oci = &v1beta1.OciStatus{
			Image: newSourceSpec.Image,
			Dir:   newSourceSpec.Dir,
		}
		source.Git = nil
		source.Helm = nil
	case HelmSourceSpec:
		source.Helm = &v1beta1.HelmStatus{
			Repo:    newSourceSpec.Repo,
			Chart:   newSourceSpec.Chart,
			Version: newSourceSpec.Version,
		}
		source.Git = nil
		source.Oci = nil
	default:
		source.Helm = nil
		source.Git = nil
		source.Oci = nil
	}
	errorSummary := &v1beta1.ErrorSummary{
		TotalCount:                len(cse),
		Truncated:                 denominator != 1,
		ErrorCountAfterTruncation: len(cse) / denominator,
	}
	source.Errors = cse[0 : len(cse)/denominator]
	source.ErrorSummary = errorSummary
	source.LastUpdate = newStatus.LastUpdate
}

func (p *rootSyncStatusClient) SetImageToSyncAnnotation(ctx context.Context, commit string) status.Error {
	opts := p.options
	rs := &v1beta1.RootSync{}
	rs.Namespace = configsync.ControllerNamespace
	rs.Name = opts.SyncName

	var patch string
	if opts.SourceType == configsync.OciSource ||
		(opts.SourceType == configsync.HelmSource && strings.HasPrefix(opts.SourceRepo, "oci://")) {
		newVal := fmt.Sprintf("%s@sha256:%s", opts.SourceRepo, commit)
		patch = fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`,
			metadata.ImageToSyncAnnotationKey, newVal)
		klog.V(3).Infof("Updating annotation: %s: %s", metadata.ImageToSyncAnnotationKey, newVal)
	} else {
		patch = fmt.Sprintf(`{"metadata":{"annotations":{"%s":null}}}`,
			metadata.ImageToSyncAnnotationKey)
		klog.V(3).Infof("Updating annotation: %s: %s", metadata.ImageToSyncAnnotationKey, "null")
	}

	err := opts.Client.Patch(ctx, rs,
		client.RawPatch(types.MergePatchType, []byte(patch)),
		client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return status.APIServerErrorf(err, "failed to patch RootSync annotation: %s", metadata.ImageToSyncAnnotationKey)
	}
	return nil
}

func (p *rootSyncStatusClient) SetRequiresRenderingAnnotation(ctx context.Context, renderingRequired bool) status.Error {
	opts := p.options
	rs := &v1beta1.RootSync{}
	if err := opts.Client.Get(ctx, rootsync.ObjectKey(opts.SyncName), rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}
	newVal := strconv.FormatBool(renderingRequired)
	if core.GetAnnotation(rs, metadata.RequiresRenderingAnnotationKey) == newVal {
		// avoid unnecessary updates
		return nil
	}
	klog.V(3).Infof("Updating annotation: %s: %s", metadata.RequiresRenderingAnnotationKey, newVal)
	existing := rs.DeepCopy()
	core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, newVal)
	err := opts.Client.Patch(ctx, rs,
		client.MergeFrom(existing),
		client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return status.APIServerErrorf(err, "failed to patch RootSync annotation: %s", metadata.RequiresRenderingAnnotationKey)
	}
	return nil
}

// SetRenderingStatus implements the Parser interface
func (p *rootSyncStatusClient) SetRenderingStatus(ctx context.Context, oldStatus, newStatus *RenderingStatus) status.Error {
	if oldStatus.Equals(newStatus) {
		return nil
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setRenderingStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *rootSyncStatusClient) setRenderingStatusWithRetries(ctx context.Context, newStatus *RenderingStatus, denominator int) status.Error {
	if denominator <= 0 {
		return status.InternalErrorf("denominator must be positive: %d", denominator)
	}
	opts := p.options

	var rs v1beta1.RootSync
	if err := opts.Client.Get(ctx, rootsync.ObjectKey(opts.SyncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	currentRS := rs.DeepCopy()

	setRenderingStatusFields(&rs.Status.Rendering, newStatus, denominator)

	continueSyncing := (rs.Status.Rendering.ErrorSummary.TotalCount == 0)
	var errorSource []v1beta1.ErrorSource
	if len(rs.Status.Rendering.Errors) > 0 {
		errorSource = []v1beta1.ErrorSource{v1beta1.RenderingError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Rendering", newStatus.Message, newStatus.Commit, errorSource, rs.Status.Rendering.ErrorSummary, newStatus.LastUpdate)

	// Avoid unnecessary status updates.
	if !currentRS.Status.Rendering.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping rendering status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.Errs)
	metrics.RecordReconcilerErrors(ctx, "rendering", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "rendering", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New rendering errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}

	if klog.V(5).Enabled() {
		klog.V(5).Infof("Updating rendering status:\nDiff (- Removed, + Added):\n%s",
			cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := opts.Client.Status().Update(ctx, &rs, client.FieldOwner(configsync.FieldManager)); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync rendering status (total error count: %d, denominator: %d): %s.", rs.Status.Rendering.ErrorSummary.TotalCount, denominator, err)
			return p.setRenderingStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync rendering status from parser")
	}
	return nil
}

func setRenderingStatusFields(rendering *v1beta1.RenderingStatus, newStatus *RenderingStatus, denominator int) {
	cse := status.ToCSE(newStatus.Errs)
	rendering.Commit = newStatus.Commit
	switch newSourceSpec := newStatus.Spec.(type) {
	case GitSourceSpec:
		rendering.Git = &v1beta1.GitStatus{
			Repo:     newSourceSpec.Repo,
			Revision: newSourceSpec.Revision,
			Branch:   newSourceSpec.Branch,
			Dir:      newSourceSpec.Dir,
		}
		rendering.Oci = nil
		rendering.Helm = nil
	case OCISourceSpec:
		rendering.Oci = &v1beta1.OciStatus{
			Image: newSourceSpec.Image,
			Dir:   newSourceSpec.Dir,
		}
		rendering.Git = nil
		rendering.Helm = nil
	case HelmSourceSpec:
		rendering.Helm = &v1beta1.HelmStatus{
			Repo:    newSourceSpec.Repo,
			Chart:   newSourceSpec.Chart,
			Version: newSourceSpec.Version,
		}
		rendering.Git = nil
		rendering.Oci = nil
	default:
		rendering.Helm = nil
		rendering.Git = nil
		rendering.Oci = nil
	}
	rendering.Message = newStatus.Message
	errorSummary := &v1beta1.ErrorSummary{
		TotalCount: len(cse),
		Truncated:  denominator != 1,
	}
	rendering.Errors = cse[0 : len(cse)/denominator]
	rendering.ErrorSummary = errorSummary
	rendering.LastUpdate = newStatus.LastUpdate
}

// GetReconcilerStatus gets the RootSync sync status from the cluster.
func (p *rootSyncStatusClient) GetReconcilerStatus(ctx context.Context) (*ReconcilerStatus, status.Error) {
	opts := p.options
	rs := &v1beta1.RootSync{}
	if err := opts.Client.Get(ctx, rootsync.ObjectKey(opts.SyncName), rs); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, status.APIServerError(err, "failed to get RootSync")
	}

	syncing := false
	for _, condition := range rs.Status.Conditions {
		if condition.Type == v1beta1.RootSyncSyncing {
			if condition.Status == metav1.ConditionTrue {
				syncing = true
			}
			break
		}
	}

	return reconcilerStatusFromRSyncStatus(rs.Status.Status, opts.SourceType, syncing), nil
}

func reconcilerStatusFromRSyncStatus(rsyncStatus v1beta1.Status, sourceType configsync.SourceType, syncing bool) *ReconcilerStatus {
	var sourceSpec, renderSpec, syncSpec SourceSpec
	switch sourceType {
	case configsync.GitSource:
		if rsyncStatus.Source.Git != nil {
			sourceSpec = GitSourceSpec{
				Repo:     rsyncStatus.Source.Git.Repo,
				Revision: rsyncStatus.Source.Git.Revision,
				Branch:   rsyncStatus.Source.Git.Branch,
				Dir:      rsyncStatus.Source.Git.Dir,
			}
		}
		if rsyncStatus.Rendering.Git != nil {
			renderSpec = GitSourceSpec{
				Repo:     rsyncStatus.Rendering.Git.Repo,
				Revision: rsyncStatus.Rendering.Git.Revision,
				Branch:   rsyncStatus.Rendering.Git.Branch,
				Dir:      rsyncStatus.Rendering.Git.Dir,
			}
		}
		if rsyncStatus.Sync.Git != nil {
			syncSpec = GitSourceSpec{
				Repo:     rsyncStatus.Sync.Git.Repo,
				Revision: rsyncStatus.Sync.Git.Revision,
				Branch:   rsyncStatus.Sync.Git.Branch,
				Dir:      rsyncStatus.Sync.Git.Dir,
			}
		}
	case configsync.OciSource:
		if rsyncStatus.Source.Oci != nil {
			sourceSpec = OCISourceSpec{
				Image: rsyncStatus.Source.Oci.Image,
				Dir:   rsyncStatus.Source.Oci.Dir,
			}
		}
		if rsyncStatus.Rendering.Oci != nil {
			renderSpec = OCISourceSpec{
				Image: rsyncStatus.Rendering.Oci.Image,
				Dir:   rsyncStatus.Rendering.Oci.Dir,
			}
		}
		if rsyncStatus.Sync.Oci != nil {
			syncSpec = OCISourceSpec{
				Image: rsyncStatus.Sync.Oci.Image,
				Dir:   rsyncStatus.Sync.Oci.Dir,
			}
		}
	case configsync.HelmSource:
		if rsyncStatus.Source.Helm != nil {
			sourceSpec = HelmSourceSpec{
				Repo:    rsyncStatus.Source.Helm.Repo,
				Chart:   rsyncStatus.Source.Helm.Chart,
				Version: rsyncStatus.Source.Helm.Version,
			}
		}
		if rsyncStatus.Rendering.Helm != nil {
			renderSpec = HelmSourceSpec{
				Repo:    rsyncStatus.Rendering.Helm.Repo,
				Chart:   rsyncStatus.Rendering.Helm.Chart,
				Version: rsyncStatus.Rendering.Helm.Version,
			}
		}
		if rsyncStatus.Sync.Helm != nil {
			syncSpec = HelmSourceSpec{
				Repo:    rsyncStatus.Sync.Helm.Repo,
				Chart:   rsyncStatus.Sync.Helm.Chart,
				Version: rsyncStatus.Sync.Helm.Version,
			}
		}
	}

	return &ReconcilerStatus{
		SourceStatus: &SourceStatus{
			Spec:   sourceSpec,
			Commit: rsyncStatus.Source.Commit,
			// Can't parse errors.
			// Errors will be reset the next time the reconciler updates the status.
			Errs:       nil,
			LastUpdate: rsyncStatus.Source.LastUpdate,
		},
		RenderingStatus: &RenderingStatus{
			Spec:    renderSpec,
			Commit:  rsyncStatus.Rendering.Commit,
			Message: rsyncStatus.Rendering.Message,
			// Can't parse errors.
			// Errors will be reset the next time the reconciler updates the status.
			Errs:       nil,
			LastUpdate: rsyncStatus.Rendering.LastUpdate,
			// Whether there exists a kustomize.yaml in the source is not persisted in the RSync status.
			// TODO: Move requiresRendering out of reconcilerStatus.
			RequiresRendering: false,
		},
		SyncStatus: &SyncStatus{
			Spec:    syncSpec,
			Syncing: syncing,
			Commit:  rsyncStatus.Sync.Commit,
			// Can't parse errors.
			// Errors will be reset the next time the reconciler updates the status.
			Errs:       nil,
			LastUpdate: rsyncStatus.Sync.LastUpdate,
		},
	}
}

// SetSyncStatus implements the Parser interface
// SetSyncStatus sets the RootSync sync status.
// `errs` includes the errors encountered during the apply step;
func (p *rootSyncStatusClient) SetSyncStatus(ctx context.Context, newStatus *SyncStatus) status.Error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSyncStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *rootSyncStatusClient) setSyncStatusWithRetries(ctx context.Context, newStatus *SyncStatus, denominator int) status.Error {
	if denominator <= 0 {
		return status.InternalErrorf("denominator must be positive: %d", denominator)
	}
	opts := p.options

	rs := &v1beta1.RootSync{}
	if err := opts.Client.Get(ctx, rootsync.ObjectKey(opts.SyncName), rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync")
	}

	currentRS := rs.DeepCopy()

	setSyncStatusFields(&rs.Status.Status, newStatus, denominator)

	// The Syncing condition should only represent the status and errors for the latest commit.
	// So only update the Syncing condition here if we haven't fetched a new commit.
	// Ideally, checking the source commit would be enough, but because fetching and parsing share the source status,
	// we also have to check the rendering commit, which may be updated first.
	var lastSyncStatus string
	if rs.Status.Source.Commit == rs.Status.Sync.Commit && rs.Status.Rendering.Commit == rs.Status.Sync.Commit {
		errorSources, errorSummary := summarizeErrorsForCommit(rs.Status.Source, rs.Status.Rendering, rs.Status.Sync, rs.Status.Sync.Commit)
		if newStatus.Syncing {
			rootsync.SetSyncing(rs, true, "Sync", "Syncing", rs.Status.Sync.Commit, errorSources, errorSummary, rs.Status.Sync.LastUpdate)
		} else {
			if errorSummary.TotalCount == 0 {
				rs.Status.LastSyncedCommit = rs.Status.Sync.Commit
			}
			rootsync.SetSyncing(rs, false, "Sync", "Sync Completed", rs.Status.Sync.Commit, errorSources, errorSummary, rs.Status.Sync.LastUpdate)
		}
		lastSyncStatus = metrics.StatusTagValueFromSummary(errorSummary)
	}

	// Avoid unnecessary status updates.
	if !currentRS.Status.Sync.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping sync status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.Errs)
	metrics.RecordReconcilerErrors(ctx, "sync", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "sync", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New sync errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}
	// Only update the LastSyncTimestamp metric immediately after a sync attempt
	if !newStatus.Syncing && rs.Status.Sync.Commit != "" && lastSyncStatus != "" {
		metrics.RecordLastSync(ctx, lastSyncStatus, rs.Status.Sync.Commit, rs.Status.Sync.LastUpdate.Time)
	}

	if klog.V(5).Enabled() {
		klog.V(5).Infof("Updating sync status:\nDiff (- Removed, + Added):\n%s",
			cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := opts.Client.Status().Update(ctx, rs, client.FieldOwner(configsync.FieldManager)); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return p.setSyncStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync sync status")
	}
	return nil
}

func setSyncStatusFields(syncStatus *v1beta1.Status, newStatus *SyncStatus, denominator int) {
	cse := status.ToCSE(newStatus.Errs)
	syncStatus.Sync.Commit = newStatus.Commit
	syncStatus.Sync.Git = syncStatus.Source.Git
	syncStatus.Sync.Oci = syncStatus.Source.Oci
	syncStatus.Sync.Helm = syncStatus.Source.Helm
	setSyncStatusErrors(syncStatus, cse, denominator)
	syncStatus.Sync.LastUpdate = newStatus.LastUpdate
}

func setSyncStatusErrors(syncStatus *v1beta1.Status, cse []v1beta1.ConfigSyncError, denominator int) {
	syncStatus.Sync.ErrorSummary = &v1beta1.ErrorSummary{
		TotalCount: len(cse),
		Truncated:  denominator != 1,
	}
	syncStatus.Sync.Errors = cse[0 : len(cse)/denominator]
}

// summarizeErrorsForCommit summarizes the source, rendering, and sync errors
// for a specific commit.
//
// Since we don't keep errors for old commits after that stage has started
// working on a new commit, this process is a little bit lossy.
func summarizeErrorsForCommit(sourceStatus v1beta1.SourceStatus, renderingStatus v1beta1.RenderingStatus, syncStatus v1beta1.SyncStatus, commit string) ([]v1beta1.ErrorSource, *v1beta1.ErrorSummary) {
	var errorSources []v1beta1.ErrorSource
	var summaries []*v1beta1.ErrorSummary

	if sourceStatus.Commit == commit && len(sourceStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SourceError)
		summaries = append(summaries, sourceStatus.ErrorSummary)
	}
	if renderingStatus.Commit == commit && len(renderingStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.RenderingError)
		summaries = append(summaries, renderingStatus.ErrorSummary)
	}
	if syncStatus.Commit == commit && len(syncStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SyncError)
		summaries = append(summaries, syncStatus.ErrorSummary)
	}

	errorSummary := &v1beta1.ErrorSummary{}
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		errorSummary.TotalCount += summary.TotalCount
		errorSummary.ErrorCountAfterTruncation += summary.ErrorCountAfterTruncation
		if summary.Truncated {
			errorSummary.Truncated = true
		}
	}
	return errorSources, errorSummary
}

// prependRootSyncRemediatorStatus adds the conflict error detected by the remediator to the front of the sync errors.
func prependRootSyncRemediatorStatus(ctx context.Context, c client.Client, syncName string, conflictErrs []status.ManagementConflictError, denominator int) status.Error {
	if denominator <= 0 {
		return status.InternalErrorf("denominator must be positive: %d", denominator)
	}

	var rs v1beta1.RootSync
	if err := c.Get(ctx, rootsync.ObjectKey(syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync: "+syncName)
	}

	// Skip updating if already reported by this reconciler or the remote reconciler (inverted)
	var errs []v1beta1.ConfigSyncError
	for _, conflictErr := range conflictErrs {
		cse := conflictErr.ToCSE()
		invertedCSE := conflictErr.Invert().ToCSE()
		errorFound := false
		for _, e := range rs.Status.Sync.Errors {
			if e.Code == status.ManagementConflictErrorCode && (e.ErrorMessage == cse.ErrorMessage || e.ErrorMessage == invertedCSE.ErrorMessage) {
				errorFound = true
				break
			}
		}
		if !errorFound {
			errs = append(errs, cse)
		}
	}

	// No new errors, so no update
	if len(errs) == 0 {
		return nil
	}

	// Add the remeditor conflict errors before other sync errors for more visibility.
	errs = append(errs, rs.Status.Sync.Errors...)
	setSyncStatusErrors(&rs.Status.Status, errs, denominator)
	rs.Status.Sync.LastUpdate = metav1.Now()

	if err := c.Status().Update(ctx, &rs, client.FieldOwner(configsync.FieldManager)); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return prependRootSyncRemediatorStatus(ctx, c, syncName, conflictErrs, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync sync status")
	}
	return nil
}
