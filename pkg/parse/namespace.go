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

	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/status"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewNamespaceRunner creates a new runnable parser for parsing a Namespace repo.
func NewNamespaceRunner(clusterName, syncName, reconcilerName string, scope declared.Scope, fileReader reader.Reader, c client.Client, pollingFrequency time.Duration, resyncPeriod time.Duration, fs FileSource, dc discovery.DiscoveryInterface, resources *declared.Resources, app applier.Interface, rem remediator.Interface) (Parser, error) {
	converter, err := declared.NewValueConverter(dc)
	if err != nil {
		return nil, err
	}

	return &namespace{
		opts: opts{
			clusterName:      clusterName,
			client:           c,
			syncName:         syncName,
			reconcilerName:   reconcilerName,
			pollingFrequency: pollingFrequency,
			resyncPeriod:     resyncPeriod,
			files:            files{FileSource: fs},
			parser:           filesystem.NewParser(fileReader),
			updater: updater{
				scope:      scope,
				resources:  resources,
				applier:    app,
				remediator: rem,
			},
			discoveryInterface: dc,
			converter:          converter,
			mux:                &sync.Mutex{},
		},
		scope: scope,
	}, nil
}

type namespace struct {
	opts

	// scope is the name of the Namespace this parser is for.
	// It is an error for this parser's repository to contain resources outside of
	// this Namespace.
	scope declared.Scope
}

var _ Parser = &namespace{}

func (p *namespace) options() *opts {
	return &(p.opts)
}

// parseSource implements the Parser interface
func (p *namespace) parseSource(ctx context.Context, state sourceState) ([]ast.FileObject, status.MultiError) {
	p.mux.Lock()
	defer p.mux.Unlock()

	filePaths := reader.FilePaths{
		RootDir:   state.syncDir,
		PolicyDir: p.SyncDir,
		Files:     state.files,
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

	objs, err = validate.Unstructured(objs, options)

	metrics.RecordReconcilerErrors(ctx, "parsing", status.NonBlockingErrors(err))

	if status.HasBlockingErrors(err) {
		return nil, err
	}

	// Duplicated with root.go.
	e := addAnnotationsAndLabels(objs, p.scope, p.syncName, p.gitContext(), state.commit)
	if e != nil {
		err = status.Append(err, status.InternalErrorf("unable to add annotations and labels: %v", e))
		return nil, err
	}
	return objs, err
}

// setSourceStatus implements the Parser interface
//
// setSourceStatus sets the source status with a given source state and set of errors.  If errs is empty, all errors
// will be removed from the status.
func (p *namespace) setSourceStatus(ctx context.Context, newStatus sourceStatus) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSourceStatusWithRetries(ctx, newStatus, defaultDenominator)
}

func (p *namespace) setSourceStatusWithRetries(ctx context.Context, newStatus sourceStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}
	// The main idea here is an error-robust way of surfacing to the user that
	// we're having problems reading from our local clone of their source repository.
	// This can happen when Kubernetes does weird things with mounted filesystems,
	// or if an attacker tried to maliciously change the cluster's record of the
	// source of truth.
	var rs v1beta1.RepoSync
	if err := p.client.Get(ctx, reposync.ObjectKey(p.scope, p.syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RepoSync for parser")
	}

	setSourceStatus(&rs.Status.Source, p, newStatus, denominator)

	continueSyncing := true
	if rs.Status.Source.ErrorSummary.TotalCount > 0 {
		continueSyncing = false
	}
	metrics.RecordPipelineError(ctx, configsync.RepoSyncName, "source", rs.Status.Source.ErrorSummary.TotalCount)
	reposync.SetSyncing(&rs, continueSyncing, "Source", "Source", newStatus.commit, []v1beta1.ErrorSource{v1beta1.SourceError}, rs.Status.Source.ErrorSummary, newStatus.lastUpdate)

	metrics.RecordReconcilerErrors(ctx, "source", status.ToCSE(newStatus.errs))

	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RepoSync source status (total error count: %d, denominator: %d): %s.", rs.Status.Source.ErrorSummary.TotalCount, denominator, err)
			return p.setSourceStatusWithRetries(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RepoSync source status from parser")
	}
	return nil
}

// setRenderingStatus implements the Parser interface
func (p *namespace) setRenderingStatus(ctx context.Context, oldStatus, newStatus renderingStatus) error {
	if oldStatus.equal(newStatus) {
		return nil
	}

	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setRenderingStatusWithRetires(ctx, newStatus, defaultDenominator)
}

func (p *namespace) setRenderingStatusWithRetires(ctx context.Context, newStatus renderingStatus, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RepoSync
	if err := p.client.Get(ctx, reposync.ObjectKey(p.scope, p.syncName), &rs); err != nil {
		return status.APIServerError(err, "failed to get RepoSync for parser")
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
		// If rendering errors exist, it should only have one error, so use cse[0] to get the error code.
		metrics.RecordReconcilerErrors(ctx, "rendering", status.ToCSE(newStatus.errs))
		continueSyncing = false
	}
	metrics.RecordPipelineError(ctx, configsync.RepoSyncName, "rendering", rs.Status.Rendering.ErrorSummary.TotalCount)

	reposync.SetSyncing(&rs, continueSyncing, "Rendering", newStatus.message, newStatus.commit, []v1beta1.ErrorSource{v1beta1.RenderingError}, rs.Status.Rendering.ErrorSummary, newStatus.lastUpdate)
	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RepoSync rendering status (total error count: %d, denominator: %d): %s.", rs.Status.Rendering.ErrorSummary.TotalCount, denominator, err)
			return p.setRenderingStatusWithRetires(ctx, newStatus, denominator*2)
		}
		return status.APIServerError(err, "failed to update RepoSync rendering status from parser")
	}
	return nil
}

// SetSyncStatus implements the Parser interface
// SetSyncStatus sets the RepoSync sync status.
// `errs` includes the errors encountered during the apply step;
func (p *namespace) SetSyncStatus(ctx context.Context, errs status.MultiError) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	return p.setSyncStatusWithRetries(ctx, errs, defaultDenominator)
}

func (p *namespace) setSyncStatusWithRetries(ctx context.Context, errs status.MultiError, denominator int) error {
	if denominator <= 0 {
		return fmt.Errorf("The denominator must be a positive number")
	}

	var rs v1beta1.RepoSync
	if err := p.client.Get(ctx, reposync.ObjectKey(p.scope, p.syncName), &rs); err != nil {
		return status.APIServerError(err, fmt.Sprintf("failed to get the RepoSync object for the %v namespace", p.scope))
	}

	// syncing indicates whether the applier is syncing.
	syncing := p.applier.Syncing()

	setSyncStatus(&rs.Status.Status, status.ToCSE(errs), denominator)

	metrics.RecordReconcilerErrors(ctx, "sync", status.ToCSE(errs))
	metrics.RecordPipelineError(ctx, configsync.RepoSyncName, "sync", rs.Status.Sync.ErrorSummary.TotalCount)
	if !syncing {
		metrics.RecordLastSync(ctx, rs.Status.Sync.Commit, rs.Status.Sync.LastUpdate.Time)
	}

	errorSources, errorSummary := summarizeErrors(rs.Status.Source, rs.Status.Sync)
	if syncing {
		reposync.SetSyncing(&rs, true, "Sync", "Syncing", rs.Status.Sync.Commit, errorSources, &errorSummary, rs.Status.Sync.LastUpdate)
	} else {
		if errorSummary.TotalCount == 0 {
			rs.Status.LastSyncedCommit = rs.Status.Sync.Commit
		}
		reposync.SetSyncing(&rs, false, "Sync", "Sync Completed", rs.Status.Sync.Commit, errorSources, &errorSummary, rs.Status.Sync.LastUpdate)
	}

	if err := p.client.Status().Update(ctx, &rs); err != nil {
		// If the update failure was caused by the size of the RootSync object, we would truncate the errors and retry.
		if isRequestTooLargeError(err) {
			klog.Infof("Failed to update RepoSync sync status (total error count: %d, denominator: %d): %s.", rs.Status.Sync.ErrorSummary.TotalCount, denominator, err)
			return p.setSyncStatusWithRetries(ctx, errs, denominator*2)
		}
		return status.APIServerError(err, fmt.Sprintf("failed to update the RepoSync sync status for the %v namespace", p.scope))
	}
	return nil
}

// ApplierErrors implements the Parser interface
func (p *namespace) ApplierErrors() status.MultiError {
	return p.applier.Errors()
}

// RemediatorConflictErrors implements the Parser interface
func (p *namespace) RemediatorConflictErrors() []status.ManagementConflictError {
	return p.remediator.ConflictErrors()
}

// K8sClient implements the Parser interface
func (p *namespace) K8sClient() client.Client {
	return p.client
}
