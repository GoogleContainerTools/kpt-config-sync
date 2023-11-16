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
	"sync"
	"time"

	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Options holds configuration and core functionality required by all parsers.
type Options struct {
	// Parser defines the minimum interface required for Reconciler to use a
	// Parser to read configs from a filesystem.
	Parser filesystem.ConfigParser

	// ClusterName is the name of the cluster we're syncing configuration to.
	ClusterName string

	// Client knows how to read objects from a Kubernetes cluster and update
	// status.
	Client client.Client

	// ReconcilerName is the name of the reconciler resources, such as service
	// account, service, deployment and etc.
	ReconcilerName string

	// SyncName is the name of the RootSync or RepoSync object.
	SyncName string

	// PollingPeriod is the period of time between checking the filesystem for
	// source updates to sync.
	PollingPeriod time.Duration

	// ResyncPeriod is the period of time between forced re-sync from source
	// (even without a new commit).
	ResyncPeriod time.Duration

	// RetryPeriod is how long the Parser waits between retries, after an error.
	RetryPeriod time.Duration

	// StatusUpdatePeriod is how long the Parser waits between updates of the
	// sync status, to account for management conflict errors from the Remediator.
	StatusUpdatePeriod time.Duration

	// DiscoveryInterface is how the Parser learns what types are currently
	// available on the cluster.
	DiscoveryInterface discovery.ServerResourcer

	// Converter uses the DiscoveryInterface to encode the declared fields of
	// objects in Git.
	Converter *declared.ValueConverter

	// mux prevents status update conflicts.
	mux *sync.Mutex

	// RenderingEnabled indicates whether the hydration-controller is currently
	// running for this reconciler.
	RenderingEnabled bool

	// Files lists Files in the source of truth.
	Files
	// Updater mutates the most-recently-seen versions of objects stored in memory.
	Updater
}

// Parser represents a parser that can be pointed at and continuously parse a source.
type Parser interface {
	parseSource(ctx context.Context, state sourceState) ([]ast.FileObject, status.MultiError)
	setSourceStatus(ctx context.Context, newStatus sourceStatus) error
	setRenderingStatus(ctx context.Context, oldStatus, newStatus renderingStatus) error
	SetSyncStatus(ctx context.Context, newStatus syncStatus) error
	options() *Options
	// SyncErrors returns all the sync errors, including remediator errors,
	// validation errors, applier errors, and watch update errors.
	SyncErrors() status.MultiError
	// Syncing returns true if the updater is running.
	Syncing() bool
	// K8sClient returns the Kubernetes client that talks to the API server.
	K8sClient() client.Client
	// setRequiresRendering sets the requires-rendering annotation on the RSync
	setRequiresRendering(ctx context.Context, renderingRequired bool) error
}

func (o *Options) k8sClient() client.Client {
	return o.Client
}

func (o *Options) discoveryClient() discovery.ServerResourcer {
	return o.DiscoveryInterface
}
