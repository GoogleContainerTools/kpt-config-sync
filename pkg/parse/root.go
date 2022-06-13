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
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewRootRunner creates a new runnable parser for parsing a Root repository.
func NewRootRunner(clusterName, syncName, reconcilerName string, format filesystem.SourceFormat, fileReader reader.Reader, c client.Client, pollingFrequency time.Duration, resyncPeriod time.Duration, fs FileSource, dc discovery.DiscoveryInterface, resources *declared.Resources, app applier.Interface, rem remediator.Interface) (Parser, error) {
	converter, err := declared.NewValueConverter(dc)
	if err != nil {
		return nil, err
	}

	opts := opts{
		clusterName:      clusterName,
		syncName:         syncName,
		reconcilerName:   reconcilerName,
		client:           c,
		pollingFrequency: pollingFrequency,
		resyncPeriod:     resyncPeriod,
		files:            files{FileSource: fs},
		parser:           filesystem.NewParser(fileReader),
		updater: updater{
			scope:      declared.RootReconciler,
			resources:  resources,
			applier:    app,
			remediator: rem,
		},
		discoveryInterface: dc,
		converter:          converter,
		mux:                &sync.Mutex{},
	}
	return &root{opts: opts, sourceFormat: format}, nil
}

type root struct {
	opts

	// sourceFormat defines the structure of the Root repository. Only the Root
	// repository may be SourceFormatHierarchy; all others are implicitly
	// SourceFormatUnstructured.
	sourceFormat filesystem.SourceFormat
}

var _ Parser = &root{}

func (p *root) options() *opts {
	return &(p.opts)
}

// parseSource implements the Parser interface
func (p *root) parseSource(ctx context.Context, state sourceState) ([]ast.FileObject, status.MultiError) {
	wantFiles := state.files
	if p.sourceFormat == filesystem.SourceFormatHierarchy {
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
	builder := utildiscovery.ScoperBuilder(p.discoveryInterface)

	klog.Infof("Parsing files from source dir: %s", state.syncDir.OSPath())
	objs, err := p.parser.Parse(filePaths)
	if err != nil {
		return nil, err
	}

	options := validate.Options{
		ClusterName:  p.clusterName,
		PolicyDir:    p.SyncDir,
		PreviousCRDs: crds,
		BuildScoper:  builder,
		Converter:    p.converter,
	}
	options = OptionsForScope(options, p.scope)

	if p.sourceFormat == filesystem.SourceFormatUnstructured {
		options.Visitors = append(options.Visitors, p.addImplicitNamespaces)
		objs, err = validate.Unstructured(objs, options)
	} else {
		objs, err = validate.Hierarchical(objs, options)
	}

	metrics.RecordReconcilerErrors(ctx, "parsing", status.NonBlockingErrors(err))

	if status.HasBlockingErrors(err) {
		return nil, err
	}

	// Duplicated with namespace.go.
	e := addAnnotationsAndLabels(objs, declared.RootReconciler, p.syncName, p.sourceContext(), state.commit)
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
	if err := p.client.Get(ctx, rootsync.ObjectKey(p.syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	setSourceStatus(&rs.Status.Source, p, newStatus, denominator)

	continueSyncing := true
	if rs.Status.Source.ErrorSummary.TotalCount > 0 {
		continueSyncing = false
	}
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "source", rs.Status.Source.ErrorSummary.TotalCount)
	var errorSource []v1beta1.ErrorSource
	if rs.Status.Source.Errors != nil {
		errorSource = []v1beta1.ErrorSource{v1beta1.SourceError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Source", "Source", newStatus.commit, errorSource, rs.Status.Source.ErrorSummary, newStatus.lastUpdate)

	metrics.RecordReconcilerErrors(ctx, "source", status.ToCSE(newStatus.errs))

	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync source status (total error count: %d, denominator: %d): %s.", rs.Status.Source.ErrorSummary.TotalCount, denominator, err)
			return p.setSourceStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync source status from parser")
	}
	return nil
}

func setSourceStatus(source *v1beta1.SourceStatus, p Parser, newStatus sourceStatus, denominator int) {
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
	case v1beta1.OciSource:
		source.Oci = &v1beta1.OciStatus{
			Image: p.options().SourceRepo,
			Dir:   p.options().SyncDir.SlashPath(),
		}
		source.Git = nil
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
	if err := p.client.Get(ctx, rootsync.ObjectKey(p.syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync for parser")
	}

	if rs.Status.Rendering.Commit != newStatus.commit {
		if newStatus.message == RenderingSkipped {
			metrics.RecordSkipRenderingCount(ctx)
		} else {
			metrics.RecordRenderingCount(ctx)
		}
	}

	setRenderingStatus(&rs.Status.Rendering, p, newStatus, denominator)

	continueSyncing := true
	if rs.Status.Rendering.ErrorSummary.TotalCount > 0 {
		metrics.RecordReconcilerErrors(ctx, "rendering", status.ToCSE(newStatus.errs))
		continueSyncing = false
	}
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "rendering", rs.Status.Rendering.ErrorSummary.TotalCount)
	var errorSource []v1beta1.ErrorSource
	if rs.Status.Rendering.Errors != nil {
		errorSource = []v1beta1.ErrorSource{v1beta1.RenderingError}
	}
	rootsync.SetSyncing(&rs, continueSyncing, "Rendering", newStatus.message, newStatus.commit, errorSource, rs.Status.Rendering.ErrorSummary, newStatus.lastUpdate)

	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync rendering status (total error count: %d, denominator: %d): %s.", rs.Status.Rendering.ErrorSummary.TotalCount, denominator, err)
			return p.setRenderingStatusWithRetires(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync rendering status from parser")
	}
	return nil
}

func setRenderingStatus(rendering *v1beta1.RenderingStatus, p Parser, newStatus renderingStatus, denominator int) {
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
	case v1beta1.OciSource:
		rendering.Oci = &v1beta1.OciStatus{
			Image: p.options().SourceRepo,
			Dir:   p.options().SyncDir.SlashPath(),
		}
		rendering.Git = nil
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
func (p *root) SetSyncStatus(ctx context.Context, errs status.MultiError) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	var allErrs status.MultiError
	remediatorErrs := p.remediator.ConflictErrors()
	for _, e := range remediatorErrs {
		allErrs = status.Append(allErrs, e)
	}
	// Add conflicting errors before other apply errors.
	allErrs = status.Append(allErrs, errs)
	return p.setSyncStatusWithRetries(ctx, allErrs, defaultDenominator)
}

func (p *root) setSyncStatusWithRetries(ctx context.Context, errs status.MultiError, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RootSync
	if err := p.client.Get(ctx, rootsync.ObjectKey(p.syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RootSync")
	}

	// syncing indicates whether the applier is syncing.
	syncing := p.applier.Syncing()

	setSyncStatus(&rs.Status.Status, status.ToCSE(errs), denominator)

	metrics.RecordReconcilerErrors(ctx, "sync", status.ToCSE(errs))
	metrics.RecordPipelineError(ctx, configsync.RootSyncName, "sync", rs.Status.Sync.ErrorSummary.TotalCount)
	if !syncing {
		metrics.RecordLastSync(ctx, rs.Status.Sync.Commit, rs.Status.Sync.LastUpdate.Time)
	}

	errorSources, errorSummary := summarizeErrors(rs.Status.Source, rs.Status.Sync)
	if syncing {
		rootsync.SetSyncing(&rs, true, "Sync", "Syncing", rs.Status.Sync.Commit, errorSources, &errorSummary, rs.Status.Sync.LastUpdate)
	} else {
		if errorSummary.TotalCount == 0 {
			rs.Status.LastSyncedCommit = rs.Status.Sync.Commit
		}
		rootsync.SetSyncing(&rs, false, "Sync", "Sync Completed", rs.Status.Sync.Commit, errorSources, &errorSummary, rs.Status.Sync.LastUpdate)
	}

	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RootSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return p.setSyncStatusWithRetries(ctx, errs, denominator*2)
		}
		return status.APIServerError(err, "failed to update RootSync sync status")
	}
	return nil
}

func setSyncStatus(syncStatus *v1beta1.Status, syncErrs []v1beta1.ConfigSyncError, denominator int) {
	syncStatus.Sync.Commit = syncStatus.Source.Commit
	syncStatus.Sync.Git = syncStatus.Source.Git
	syncStatus.Sync.Oci = syncStatus.Source.Oci
	syncStatus.Sync.ErrorSummary = &v1beta1.ErrorSummary{
		TotalCount: len(syncErrs),
		Truncated:  denominator != 1,
	}
	syncStatus.Sync.Errors = syncErrs[0 : len(syncErrs)/denominator]
	syncStatus.Sync.LastUpdate = metav1.Now()
}

// summarizeErrors summarizes the errors from `sourceStatus` and `syncStatus`, and returns an ErrorSource slice and an ErrorSummary.
func summarizeErrors(sourceStatus v1beta1.SourceStatus, syncStatus v1beta1.SyncStatus) ([]v1beta1.ErrorSource, v1beta1.ErrorSummary) {
	errorSources := []v1beta1.ErrorSource{}
	if len(sourceStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SourceError)
	}
	if len(syncStatus.Errors) > 0 {
		errorSources = append(errorSources, v1beta1.SyncError)
	}

	errorSummary := v1beta1.ErrorSummary{}
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
		err := p.client.Get(context.Background(), types.NamespacedName{Name: ns}, existingNs)
		if err != nil && !apierrors.IsNotFound(err) {
			errs = status.Append(errs, errors.Wrapf(err, "unable to check the existence of the implicit namespace %q", ns))
			continue
		}

		existingNs.SetGroupVersionKind(kinds.Namespace())
		// If the namespace already exists and not self-managed, do not add it as an implicit namespace.
		// This is to avoid conflicts caused by multiple Root reconcilers managing the same implicit namespace.
		if err == nil && !diff.IsManager(p.scope, p.syncName, existingNs) {
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

// ApplierErrors implements the Parser interface
func (p *root) ApplierErrors() status.MultiError {
	return p.applier.Errors()
}

// RemediatorConflictErrors implements the Parser interface
func (p *root) RemediatorConflictErrors() []status.ManagementConflictError {
	return p.remediator.ConflictErrors()
}

// K8sClient implements the Parser interface
func (p *root) K8sClient() client.Client {
	return p.client
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
	setSyncStatus(&rs.Status.Status, errs, denominator)

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
