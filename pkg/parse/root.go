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

	"github.com/elliotchance/orderedmap/v2"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/compare"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewRootRunner creates a new runnable parser for parsing a Root repository.
func NewRootRunner(opts *Options, rootOpts *RootOptions) Parser {
	return &root{
		Options:     opts,
		RootOptions: rootOpts,
	}
}

// RootOptions includes options specific to RootSync objects.
type RootOptions struct {
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

type root struct {
	*Options
	*RootOptions
}

var _ Parser = &root{}

func (p *root) options() *Options {
	return p.Options
}

// parseSource implements the Parser interface
func (p *root) parseSource(ctx context.Context, state *sourceState) ([]ast.FileObject, status.MultiError) {
	wantFiles := state.files
	if p.SourceFormat == configsync.SourceFormatHierarchy {
		// We're using hierarchical mode for the root repository, so ignore files
		// outside of the allowed directories.
		wantFiles = filesystem.FilterHierarchyFiles(state.syncDir, wantFiles)
	}

	filePaths := reader.FilePaths{
		RootDir:   state.syncDir,
		PolicyDir: p.SyncDir,
		Files:     wantFiles,
	}

	crds, err := p.declaredCRDs()
	if err != nil {
		return nil, err
	}
	builder := discovery.ScoperBuilder(p.DiscoveryInterface)

	klog.Infof("Parsing files from source dir: %s", state.syncDir.OSPath())
	objs, err := p.Parser.Parse(filePaths)
	if err != nil {
		return nil, err
	}

	options := validate.Options{
		ClusterName:  p.ClusterName,
		SyncName:     p.SyncName,
		PolicyDir:    p.SyncDir,
		PreviousCRDs: crds,
		BuildScoper:  builder,
		Converter:    p.Converter,
		// Enable API call so NamespaceSelector can talk to k8s-api-server.
		AllowAPICall:             true,
		DynamicNSSelectorEnabled: p.DynamicNSSelectorEnabled,
		NSControllerState:        p.NSControllerState,
		WebhookEnabled:           p.WebhookEnabled,
		FieldManager:             configsync.FieldManager,
	}
	options = OptionsForScope(options, p.Scope)

	if p.SourceFormat == configsync.SourceFormatUnstructured {
		if p.NamespaceStrategy == configsync.NamespaceStrategyImplicit {
			options.Visitors = append(options.Visitors, p.addImplicitNamespaces)
		}
		objs, err = validate.Unstructured(ctx, p.Client, objs, options)
	} else {
		objs, err = validate.Hierarchical(objs, options)
	}

	if status.HasBlockingErrors(err) {
		return nil, err
	}

	// Duplicated with namespace.go.
	e := addAnnotationsAndLabels(objs, declared.RootScope, p.SyncName, p.sourceContext(), state.commit)
	if e != nil {
		err = status.Append(err, status.InternalErrorf("unable to add annotations and labels: %v", e))
		return nil, err
	}
	return objs, err
}

// setSourceStatus implements the Parser interface
func (p *root) setSourceStatus(ctx context.Context, newStatus *SourceStatus) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSourceStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *root) setSourceStatusWithRetries(ctx context.Context, newStatus *SourceStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), &rs); err != nil {
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

	if err := p.Client.Status().Update(ctx, &rs, client.FieldOwner(configsync.FieldManager)); err != nil {
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

func (p *root) setRequiresRendering(ctx context.Context, renderingRequired bool) error {
	rs := &v1beta1.RootSync{}
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}
	newVal := strconv.FormatBool(renderingRequired)
	if core.GetAnnotation(rs, metadata.RequiresRenderingAnnotationKey) == newVal {
		// avoid unnecessary updates
		return nil
	}
	existing := rs.DeepCopy()
	core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, newVal)
	return p.Client.Patch(ctx, rs, client.MergeFrom(existing), client.FieldOwner(configsync.FieldManager))
}

// setRenderingStatus implements the Parser interface
func (p *root) setRenderingStatus(ctx context.Context, oldStatus, newStatus *RenderingStatus) error {
	if oldStatus.Equals(newStatus) {
		return nil
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setRenderingStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *root) setRenderingStatusWithRetries(ctx context.Context, newStatus *RenderingStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), &rs); err != nil {
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

	if err := p.Client.Status().Update(ctx, &rs, client.FieldOwner(configsync.FieldManager)); err != nil {
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

// ReconcilerStatusFromCluster gets the RootSync sync status from the cluster.
func (p *root) ReconcilerStatusFromCluster(ctx context.Context) (*ReconcilerStatus, error) {
	rs := &v1beta1.RootSync{}
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), rs); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, status.APIServerError(err, "failed to get RootSync")
	}

	syncing := false
	var syncingConditionLastUpdate metav1.Time
	for _, condition := range rs.Status.Conditions {
		if condition.Type == v1beta1.RootSyncSyncing {
			if condition.Status == metav1.ConditionTrue {
				syncing = true
			}
			syncingConditionLastUpdate = condition.LastUpdateTime
			break
		}
	}

	return reconcilerStatusFromRSyncStatus(rs.Status.Status, p.options().SourceType, syncing, syncingConditionLastUpdate), nil
}

func reconcilerStatusFromRSyncStatus(rsyncStatus v1beta1.Status, sourceType configsync.SourceType, syncing bool, syncingConditionLastUpdate metav1.Time) *ReconcilerStatus {
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
		SyncingConditionLastUpdate: syncingConditionLastUpdate,
	}
}

// SetSyncStatus implements the Parser interface
// SetSyncStatus sets the RootSync sync status.
// `errs` includes the errors encountered during the apply step;
func (p *root) SetSyncStatus(ctx context.Context, newStatus *SyncStatus) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSyncStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *root) setSyncStatusWithRetries(ctx context.Context, newStatus *SyncStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	rs := &v1beta1.RootSync{}
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), rs); err != nil {
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

	if err := p.Client.Status().Update(ctx, rs, client.FieldOwner(configsync.FieldManager)); err != nil {
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

// addImplicitNamespaces hydrates the given FileObjects by injecting implicit
// namespaces into the list before returning it. Implicit namespaces are those
// that are declared by an object's metadata namespace field but are not present
// in the list. The implicit namespace is only added if it doesn't exist.
func (p *root) addImplicitNamespaces(objs []ast.FileObject) ([]ast.FileObject, status.MultiError) {
	var errs status.MultiError
	// namespaces will track the set of Namespaces we expect to exist, and those
	// which actually do.
	namespaces := orderedmap.NewOrderedMap[string, bool]()

	for _, o := range objs {
		if o.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind() {
			namespaces.Set(o.GetName(), true)
		} else if o.GetNamespace() != "" {
			if _, found := namespaces.Get(o.GetNamespace()); !found {
				// If unset, this ensures the key exists and is false.
				// Otherwise it has no impact.
				namespaces.Set(o.GetNamespace(), false)
			}
		}
	}

	for e := namespaces.Front(); e != nil; e = e.Next() {
		ns, isDeclared := e.Key, e.Value
		// Do not treat config-management-system as an implicit namespace for multi-sync support.
		// Otherwise, the namespace will become a managed resource, and will cause conflict among multiple RootSyncs.
		if isDeclared || ns == configsync.ControllerNamespace {
			continue
		}
		existingNs := &corev1.Namespace{}
		err := p.Client.Get(context.Background(), types.NamespacedName{Name: ns}, existingNs)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = status.Append(errs, fmt.Errorf("unable to check the existence of the implicit namespace %q: %w", ns, err))
			continue
		}

		existingNs.SetGroupVersionKind(kinds.Namespace())
		// If the namespace already exists and not self-managed, do not add it as an implicit namespace.
		// This is to avoid conflicts caused by multiple Root reconcilers managing the same implicit namespace.
		if err == nil && !diff.IsManager(p.Scope, p.SyncName, existingNs) {
			continue
		}

		// Add the implicit namespace if it doesn't exist, or if it is managed by itself.
		// If it is a self-managed namespace, still add it to the object list. Otherwise,
		// it will be pruned because it is no longer in the inventory list.
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(kinds.Namespace())
		u.SetName(ns)
		// We do NOT want to delete theses implicit Namespaces when the resources
		// inside them are removed from the repo. We don't know when it is safe to remove
		// the implicit namespaces. An implicit namespace may already exist in the
		// cluster. Deleting it will cause other unmanaged resources in that namespace
		// being deleted.
		//
		// Adding the LifecycleDeleteAnnotation is to prevent the applier from deleting
		// the implicit namespace when the namespaced config is removed from the repo.
		// Note that if the user later declares the
		// Namespace without this annotation, the annotation is removed as expected.
		u.SetAnnotations(map[string]string{common.LifecycleDeleteAnnotation: common.PreventDeletion})
		objs = append(objs, ast.NewFileObject(u, cmpath.RelativeOS("")))
	}

	return objs, errs
}

// SyncErrors returns all the sync errors, including remediator errors,
// validation errors, applier errors, and watch update errors.
// SyncErrors implements the Parser interface
func (p *root) SyncErrors() status.MultiError {
	return p.SyncErrorCache.Errors()
}

// K8sClient implements the Parser interface
func (p *root) K8sClient() client.Client {
	return p.Client
}

// prependRootSyncRemediatorStatus adds the conflict error detected by the remediator to the front of the sync errors.
func prependRootSyncRemediatorStatus(ctx context.Context, c client.Client, syncName string, conflictErrs []status.ManagementConflictError, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
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
