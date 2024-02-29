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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	opts.mux = &sync.Mutex{}
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
	SourceFormat filesystem.SourceFormat

	// NamespaceStrategy indicates the NamespaceStrategy to be used by this
	// reconciler.
	NamespaceStrategy configsync.NamespaceStrategy

	// DynamicNSSelectorEnabled represents whether the NamespaceSelector's dynamic
	// mode is enabled. If it is enabled, NamespaceSelector will also select
	// resources matching the on-cluster Namespaces.
	DynamicNSSelectorEnabled bool

	// NSControllerState stores whether the Namespace Controller schedules a sync
	// event for the reconciler thread, along with the cached NamespaceSelector
	// and selected namespaces.
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
func (p *root) parseSource(ctx context.Context, state sourceState) ([]ast.FileObject, status.MultiError) {
	wantFiles := state.files
	if p.SourceFormat == filesystem.SourceFormatHierarchy {
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
	}
	options = OptionsForScope(options, p.Scope)

	if p.SourceFormat == filesystem.SourceFormatUnstructured {
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
	e := addAnnotationsAndLabels(objs, declared.RootReconciler, p.SyncName, p.sourceContext(), state.commit)
	if e != nil {
		err = status.Append(err, status.InternalErrorf("unable to add annotations and labels: %v", e))
		return nil, err
	}
	return objs, err
}

// setSourceStatus implements the Parser interface
func (p *root) setSourceStatus(ctx context.Context, newStatus sourceStatus) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSourceStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *root) setSourceStatusWithRetries(ctx context.Context, newStatus sourceStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	currentRS := rs.DeepCopy()

	setSourceStatusFields(&rs.Status.Source, p, newStatus, denominator)

	continueSyncing := (rs.Status.Source.ErrorSummary.TotalCount == 0)
	var errorSource []v1beta1.ErrorSource
	if len(rs.Status.Source.Errors) > 0 {
		errorSource = []v1beta1.ErrorSource{v1beta1.SourceError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Source", "Source", newStatus.commit, errorSource, rs.Status.Source.ErrorSummary, newStatus.lastUpdate)

	// Avoid unnecessary status updates.
	if !currentRS.Status.Source.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping source status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.errs)
	metrics.RecordReconcilerErrors(ctx, "source", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "source", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New source errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}

	if klog.V(5).Enabled() {
		klog.Infof("Updating source status for RootSync %s/%s:\nDiff (- Expected, + Actual):\n%s",
			rs.Namespace, rs.Name, cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := p.Client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync source status (total error count: %d, denominator: %d): %s.", rs.Status.Source.ErrorSummary.TotalCount, denominator, err)
			return p.setSourceStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync source status from parser")
	}
	return nil
}

func setSourceStatusFields(source *v1beta1.SourceStatus, p Parser, newStatus sourceStatus, denominator int) {
	cse := status.ToCSE(newStatus.errs)
	source.Commit = newStatus.commit
	switch p.options().SourceType {
	case v1beta1.GitSource:
		source.Git = &v1beta1.GitStatus{
			Repo:     p.options().SourceRepo,
			Revision: p.options().SourceRev,
			Branch:   p.options().SourceBranch,
			Dir:      p.options().SyncDir.SlashPath(),
		}
		source.Oci = nil
		source.Helm = nil
	case v1beta1.OciSource:
		source.Oci = &v1beta1.OciStatus{
			Image: p.options().SourceRepo,
			Dir:   p.options().SyncDir.SlashPath(),
		}
		source.Git = nil
		source.Helm = nil
	case v1beta1.HelmSource:
		source.Helm = &v1beta1.HelmStatus{
			Repo:    p.options().SourceRepo,
			Chart:   p.options().SyncDir.SlashPath(),
			Version: getChartVersionFromCommit(p.options().SourceRev, source.Commit),
		}
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
	source.LastUpdate = newStatus.lastUpdate
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
	return p.Client.Patch(ctx, rs, client.MergeFrom(existing))
}

// setRenderingStatus implements the Parser interface
func (p *root) setRenderingStatus(ctx context.Context, oldStatus, newStatus renderingStatus) error {
	if oldStatus.equal(newStatus) {
		return nil
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setRenderingStatusWithRetires(ctx, newStatus, defaultDenominator)
}

func (p *root) setRenderingStatusWithRetires(ctx context.Context, newStatus renderingStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	currentRS := rs.DeepCopy()

	setRenderingStatusFields(&rs.Status.Rendering, p, newStatus, denominator)

	continueSyncing := (rs.Status.Rendering.ErrorSummary.TotalCount == 0)
	var errorSource []v1beta1.ErrorSource
	if len(rs.Status.Rendering.Errors) > 0 {
		errorSource = []v1beta1.ErrorSource{v1beta1.RenderingError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Rendering", newStatus.message, newStatus.commit, errorSource, rs.Status.Rendering.ErrorSummary, newStatus.lastUpdate)

	// Avoid unnecessary status updates.
	if !currentRS.Status.Rendering.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping rendering status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.errs)
	metrics.RecordReconcilerErrors(ctx, "rendering", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "rendering", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New rendering errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}

	if klog.V(5).Enabled() {
		klog.Infof("Updating rendering status for RootSync %s/%s:\nDiff (- Expected, + Actual):\n%s",
			rs.Namespace, rs.Name, cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := p.Client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync rendering status (total error count: %d, denominator: %d): %s.", rs.Status.Rendering.ErrorSummary.TotalCount, denominator, err)
			return p.setRenderingStatusWithRetires(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync rendering status from parser")
	}
	return nil
}

func setRenderingStatusFields(rendering *v1beta1.RenderingStatus, p Parser, newStatus renderingStatus, denominator int) {
	cse := status.ToCSE(newStatus.errs)
	rendering.Commit = newStatus.commit
	switch p.options().SourceType {
	case v1beta1.GitSource:
		rendering.Git = &v1beta1.GitStatus{
			Repo:     p.options().SourceRepo,
			Revision: p.options().SourceRev,
			Branch:   p.options().SourceBranch,
			Dir:      p.options().SyncDir.SlashPath(),
		}
		rendering.Oci = nil
		rendering.Helm = nil
	case v1beta1.OciSource:
		rendering.Oci = &v1beta1.OciStatus{
			Image: p.options().SourceRepo,
			Dir:   p.options().SyncDir.SlashPath(),
		}
		rendering.Git = nil
		rendering.Helm = nil
	case v1beta1.HelmSource:
		rendering.Helm = &v1beta1.HelmStatus{
			Repo:    p.options().SourceRepo,
			Chart:   p.options().SyncDir.SlashPath(),
			Version: getChartVersionFromCommit(p.options().SourceRev, rendering.Commit),
		}
		rendering.Git = nil
		rendering.Oci = nil
	}
	rendering.Message = newStatus.message
	errorSummary := &v1beta1.ErrorSummary{
		TotalCount: len(cse),
		Truncated:  denominator != 1,
	}
	rendering.Errors = cse[0 : len(cse)/denominator]
	rendering.ErrorSummary = errorSummary
	rendering.LastUpdate = newStatus.lastUpdate
}

// SetSyncStatus implements the Parser interface
// SetSyncStatus sets the RootSync sync status.
// `errs` includes the errors encountered during the apply step;
func (p *root) SetSyncStatus(ctx context.Context, newStatus syncStatus) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSyncStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *root) setSyncStatusWithRetries(ctx context.Context, newStatus syncStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	rs := &v1beta1.RootSync{}
	if err := p.Client.Get(ctx, rootsync.ObjectKey(p.SyncName), rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync")
	}

	currentRS := rs.DeepCopy()

	setSyncStatusFields(&rs.Status.Status, newStatus, denominator)

	errorSources, errorSummary := summarizeErrors(rs.Status.Source, rs.Status.Sync)
	if newStatus.syncing {
		rootsync.SetSyncing(rs, true, "Sync", "Syncing", rs.Status.Sync.Commit, errorSources, errorSummary, rs.Status.Sync.LastUpdate)
	} else {
		if errorSummary.TotalCount == 0 {
			rs.Status.LastSyncedCommit = rs.Status.Sync.Commit
		}
		rootsync.SetSyncing(rs, false, "Sync", "Sync Completed", rs.Status.Sync.Commit, errorSources, errorSummary, rs.Status.Sync.LastUpdate)
	}

	// Avoid unnecessary status updates.
	if !currentRS.Status.Sync.LastUpdate.IsZero() && cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping sync status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return nil
	}

	csErrs := status.ToCSE(newStatus.errs)
	metrics.RecordReconcilerErrors(ctx, "sync", csErrs)
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "sync", len(csErrs))
	if len(csErrs) > 0 {
		klog.Infof("New sync errors for RootSync %s/%s: %+v",
			rs.Namespace, rs.Name, csErrs)
	}
	if !newStatus.syncing && rs.Status.Sync.Commit != "" {
		metrics.RecordLastSync(ctx, metrics.StatusTagValueFromSummary(errorSummary), rs.Status.Sync.Commit, rs.Status.Sync.LastUpdate.Time)
	}

	if klog.V(5).Enabled() {
		klog.Infof("Updating sync status for RootSync %s/%s:\nDiff (- Expected, + Actual):\n%s",
			rs.Namespace, rs.Name, cmp.Diff(currentRS.Status, rs.Status))
	}

	if err := p.Client.Status().Update(ctx, rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return p.setSyncStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync sync status")
	}
	return nil
}

func setSyncStatusFields(syncStatus *v1beta1.Status, newStatus syncStatus, denominator int) {
	cse := status.ToCSE(newStatus.errs)
	syncStatus.Sync.Commit = newStatus.commit
	syncStatus.Sync.Git = syncStatus.Source.Git
	syncStatus.Sync.Oci = syncStatus.Source.Oci
	syncStatus.Sync.Helm = syncStatus.Source.Helm
	setSyncStatusErrors(syncStatus, cse, denominator)
	syncStatus.Sync.LastUpdate = newStatus.lastUpdate
}

func setSyncStatusErrors(syncStatus *v1beta1.Status, cse []v1beta1.ConfigSyncError, denominator int) {
	syncStatus.Sync.ErrorSummary = &v1beta1.ErrorSummary{
		TotalCount: len(cse),
		Truncated:  denominator != 1,
	}
	syncStatus.Sync.Errors = cse[0 : len(cse)/denominator]
}

// summarizeErrors summarizes the errors from `sourceStatus` and `syncStatus`, and returns an ErrorSource slice and an ErrorSummary.
func summarizeErrors(sourceStatus v1beta1.SourceStatus, syncStatus v1beta1.SyncStatus) ([]v1beta1.ErrorSource, *v1beta1.ErrorSummary) {
	var errorSources []v1beta1.ErrorSource
	if len(sourceStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SourceError)
	}
	if len(syncStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SyncError)
	}

	errorSummary := &v1beta1.ErrorSummary{}
	for _, summary := range []*v1beta1.ErrorSummary{sourceStatus.ErrorSummary, syncStatus.ErrorSummary} {
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
	namespaces := make(map[string]bool)

	for _, o := range objs {
		if o.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind() {
			namespaces[o.GetName()] = true
		} else if o.GetNamespace() != "" && !namespaces[o.GetNamespace()] {
			// If unset, this ensures the key exists and is false.
			// Otherwise it has no impact.
			namespaces[o.GetNamespace()] = false
		}
	}

	for ns, isDeclared := range namespaces {
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
	return p.Errors()
}

// Syncing returns true if the updater is running.
// SyncErrors implements the Parser interface
func (p *root) Syncing() bool {
	return p.Updating()
}

// K8sClient implements the Parser interface
func (p *root) K8sClient() client.Client {
	return p.Client
}

// prependRootSyncRemediatorStatus adds the conflict error detected by the remediator to the front of the sync errors.
func prependRootSyncRemediatorStatus(ctx context.Context, client client.Client, syncName string, conflictErrs []status.ManagementConflictError, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := client.Get(ctx, rootsync.ObjectKey(syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync: "+syncName)
	}

	var errs []v1beta1.ConfigSyncError
	for _, conflictErr := range conflictErrs {
		conflictCSEError := conflictErr.ToCSE()
		conflictPairCSEError := conflictErr.CurrentManagerError().ToCSE()
		errorFound := false
		for _, e := range rs.Status.Sync.Errors {
			// Dedup the same remediator conflict error.
			if e.Code == status.ManagementConflictErrorCode && (e.ErrorMessage == conflictCSEError.ErrorMessage || e.ErrorMessage == conflictPairCSEError.ErrorMessage) {
				errorFound = true
				break
			}
		}
		if !errorFound {
			errs = append(errs, conflictCSEError)
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

	if err := client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return prependRootSyncRemediatorStatus(ctx, client, syncName, conflictErrs, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync sync status")
	}
	return nil
}

// sourceRev will display the source version,
// but that could potentially be provided to use as a range of
// versions from which we pick the latest. We should display the
// version that was actually pulled down if we can.
// commit is expected to be of the format `chart:version`,
// so we parse it to grab the version.
func getChartVersionFromCommit(sourceRev, commit string) string {
	split := strings.Split(commit, ":")
	if len(split) == 2 {
		return split[1]
	}
	return sourceRev
}
