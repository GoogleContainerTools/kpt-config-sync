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
	"time"

	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/util/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Options holds configuration and dependencies required by all parsers.
type Options struct {
	// Files lists Files in the source of truth.
	// TODO: compose Files without extending.
	Files

	// Clock is used for time tracking, namely to simplify testing by allowing
	// a fake clock, instead of a RealClock.
	Clock clock.Clock

	// ConfigParser defines the minimum interface required for Reconciler to use a
	// ConfigParser to read configs from a filesystem.
	ConfigParser filesystem.ConfigParser

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

	// Scope defines the scope of the reconciler, either root or namespaced.
	Scope declared.Scope

	// DiscoveryClient is how the Parser learns what types are currently
	// available on the cluster.
	DiscoveryClient discovery.ServerResourcer

	// Converter uses the DiscoveryInterface to encode the declared fields of
	// objects in Git.
	Converter *declared.ValueConverter

	// WebhookEnabled indicates whether the Webhook is currently enabled
	WebhookEnabled bool

	// DeclaredResources is the set of valid source objects, managed by the
	// Updater and shared with the Parser & Remediator.
	// This is used by the Parser to validate that CRDs can only be removed from
	// the source when all of its CRs are removed as well.
	DeclaredResources *declared.Resources
}

// ReconcilerOptions holds configuration for the reconciler.
type ReconcilerOptions struct {
	// Extend parser options to ensure they're using the same dependencies.
	*Options

	// Updater syncs the source from the parsed cache to the cluster.
	*Updater

	// StatusUpdatePeriod is how long the Parser waits between updates of the
	// sync status, to account for management conflict errors from the Remediator.
	StatusUpdatePeriod time.Duration

	// RenderingEnabled indicates whether the hydration-controller is currently
	// running for this reconciler.
	RenderingEnabled bool
}
