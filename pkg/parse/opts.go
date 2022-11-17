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

// opts holds configuration and core functionality required by all parsers.
type opts struct {
	parser filesystem.ConfigParser

	// clusterName is the name of the cluster we're syncing configuration to.
	clusterName string

	// client knows how to read objects from a Kubernetes cluster and update
	// status.
	client client.Client

	// reconcilerName is the name of the reconciler resources, such as service
	// account, service, deployment and etc.
	reconcilerName string

	// syncName is the name of the RootSync or RepoSync object.
	syncName string

	// pollingPeriod is the period of time between checking the filesystem for
	// source updates to sync.
	pollingPeriod time.Duration

	// ResyncPeriod is the period of time between forced re-sync from source
	// (even without a new commit).
	resyncPeriod time.Duration

	// retryPeriod is how long the parser waits between retries, after an error.
	retryPeriod time.Duration

	// statusUpdatePeriod is how long the parser waits between updates of the
	// sync status, to account for management conflict errors from the remediator.
	statusUpdatePeriod time.Duration

	// discoveryInterface is how the parser learns what types are currently
	// available on the cluster.
	discoveryInterface discovery.ServerResourcer

	// converter uses the discoveryInterface to encode the declared fields of
	// objects in Git.
	converter *declared.ValueConverter

	// mux prevents status update conflicts.
	mux *sync.Mutex

	files
	updater
}

// Parser represents a parser that can be pointed at and continuously parse a source.
type Parser interface {
	parseSource(ctx context.Context, state sourceState) ([]ast.FileObject, status.MultiError)
	setSourceStatus(ctx context.Context, newStatus sourceStatus) error
	setRenderingStatus(ctx context.Context, oldStatus, newStatus renderingStatus) error
	SetSyncStatus(ctx context.Context, newStatus syncStatus) error
	options() *opts
	// SyncErrors returns all the sync errors, including remediator errors,
	// validation errors, applier errors, and watch update errors.
	SyncErrors() status.MultiError
	// Syncing returns true if the updater is running.
	Syncing() bool
	// K8sClient returns the Kubernetes client that talks to the API server.
	K8sClient() client.Client
}

func (o *opts) k8sClient() client.Client {
	return o.client
}

func (o *opts) discoveryClient() discovery.ServerResourcer {
	return o.discoveryInterface
}
