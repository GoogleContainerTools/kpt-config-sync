package syncclient

import (
	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
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

// RootOptions includes options specific to RootSync objects.
type RootOptions struct {
	// Extend Options
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
