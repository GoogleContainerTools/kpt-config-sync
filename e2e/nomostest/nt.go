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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"go.opencensus.io/tag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/e2e"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NT represents the test environment for a single Nomos end-to-end test case.
type NT struct {
	Context context.Context

	// T is the test environment for the test.
	// Used to exit tests early when setup fails, and for logging.
	T testing.NTB

	// ClusterName is the unique name of the test run.
	ClusterName string

	// TmpDir is the temporary directory the test will write to.
	// By default, automatically deleted when the test finishes.
	TmpDir string

	// Config specifies how to create a new connection to the cluster.
	Config *rest.Config

	// Client is the underlying client used to talk to the Kubernetes cluster.
	//
	// Most tests shouldn't need to talk directly to this, unless simulating
	// direct interactions with the API Server.
	Client client.Client

	// IsGKEAutopilot indicates if the test cluster is a GKE Autopilot cluster.
	IsGKEAutopilot bool

	// DefaultWaitTimeout is the default timeout for tests to wait for sync completion.
	DefaultWaitTimeout time.Duration

	// DefaultReconcileTimeout is the default timeout for the applier to wait
	// for object reconcilition.
	DefaultReconcileTimeout time.Duration

	// RootRepos is the root repositories the cluster is syncing to.
	// The key is the RootSync name and the value points to the corresponding Repository object.
	// Each test case was set up with a default RootSync (`root-sync`) installed.
	// After the test, all other RootSync or RepoSync objects are deleted, but the default one persists.
	RootRepos map[string]*Repository

	// NonRootRepos is the Namespace repositories the cluster is syncing to.
	// Only used in multi-repo tests.
	// The key is the namespace and name of the RepoSync object, the value points to the corresponding Repository object.
	NonRootRepos map[types.NamespacedName]*Repository

	// MultiRepo indicates that the test case is for multi-repo Config Sync.
	MultiRepo bool

	// ReconcilerPollingPeriod defines how often the reconciler should poll the
	// filesystem for updates to the source or rendered configs.
	ReconcilerPollingPeriod time.Duration

	// HydrationPollingPeriod defines how often the hydration-controller should
	// poll the filesystem for rendering the DRY configs.
	HydrationPollingPeriod time.Duration

	// gitPrivateKeyPath is the path to the private key used for communicating with the Git server.
	gitPrivateKeyPath string

	// gitRepoPort is the local port that forwards to the git repo deployment.
	gitRepoPort int

	// kubeconfigPath is the path to the kubeconfig file for the kind cluster
	kubeconfigPath string

	// scheme is the Scheme for the test suite that maps from structs to GVKs.
	scheme *runtime.Scheme

	// otelCollectorPort is the local port that forwards to the otel-collector.
	otelCollectorPort int

	// otelCollectorPodName is the pod name of the otel-collector.
	otelCollectorPodName string

	// ReconcilerMetrics is a map of scraped multirepo metrics.
	ReconcilerMetrics testmetrics.ConfigSyncMetrics

	// GitProvider is the provider that hosts the Git repositories.
	GitProvider gitproviders.GitProvider

	// RemoteRepositories maintains a map between the repo local name and the remote repository.
	// It includes both root repo and namespace repos and can be shared among test cases.
	// It is used to reuse existing repositories instead of creating new ones.
	RemoteRepositories map[types.NamespacedName]*Repository
}

const (
	shared = "shared"
)

// DefaultRootReconcilerName is the root-reconciler name of the default RootSync object: "root-sync".
var DefaultRootReconcilerName = core.RootReconcilerName(configsync.RootSyncName)

// RootSyncNN returns the NamespacedName of the RootSync object.
func RootSyncNN(name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: configsync.ControllerNamespace,
		Name:      name,
	}
}

// RepoSyncNN returns the NamespacedName of the RepoSync object.
func RepoSyncNN(ns, name string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: ns,
		Name:      name,
	}
}

// DefaultRootRepoNamespacedName is the NamespacedName of the default RootSync object.
var DefaultRootRepoNamespacedName = RootSyncNN(configsync.RootSyncName)

var sharedNT *NT

// NewSharedNT sets up the shared config sync testing environment globally.
func NewSharedNT() *NT {
	tmpDir := filepath.Join(os.TempDir(), NomosE2E, shared)
	if err := os.RemoveAll(tmpDir); err != nil {
		fmt.Printf("failed to remove the shared test directory: %v\n", err)
		os.Exit(1)
	}
	fakeNTB := testing.New(&testing.FakeNTB{})
	opts := NewOptStruct(shared, tmpDir, fakeNTB)
	sharedNT = FreshTestEnv(fakeNTB, opts)
	return sharedNT
}

// SharedNT returns the shared test environment.
func SharedNT() *NT {
	if !*e2e.ShareTestEnv {
		fmt.Println("Error: the shared test environment is only available when running against GKE clusters")
		os.Exit(1)
	}
	return sharedNT
}

// GitPrivateKeyPath returns the path to the git private key.
//
// Deprecated: only the legacy bats tests should make use of this function.
func (nt *NT) GitPrivateKeyPath() string {
	return nt.gitPrivateKeyPath
}

// GitRepoPort returns the path to the git private key.
//
// Deprecated: only the legacy bats tests should make use of this function.
func (nt *NT) GitRepoPort() int {
	return nt.gitRepoPort
}

// KubeconfigPath returns the path to the kubeconifg file.
//
// Deprecated: only the legacy bats tests should make use of this function.
func (nt *NT) KubeconfigPath() string {
	return nt.kubeconfigPath
}

func fmtObj(obj client.Object) string {
	return fmt.Sprintf("%s/%s %T", obj.GetNamespace(), obj.GetName(), obj)
}

// Get is identical to Get defined for client.Client, except:
//
// 1) Context implicitly uses the one created for the test case.
// 2) name and namespace are strings instead of requiring client.ObjectKey.
//
// Leave namespace as empty string for cluster-scoped resources.
func (nt *NT) Get(name, namespace string, obj client.Object) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	if obj.GetResourceVersion() != "" {
		// If obj is already populated, this can cause the final obj to be a
		// composite of multiple states of the object on the cluster.
		//
		// If this is due to a retry loop, remember to create a new instance to
		// populate for each loop.
		return errors.Errorf("called .Get on already-populated object %v: %v", obj.GetObjectKind().GroupVersionKind(), obj)
	}
	return nt.Client.Get(nt.Context, client.ObjectKey{Name: name, Namespace: namespace}, obj)
}

// List is identical to List defined for client.Client, but without requiring Context.
func (nt *NT) List(obj client.ObjectList, opts ...client.ListOption) error {
	return nt.Client.List(nt.Context, obj, opts...)
}

// Create is identical to Create defined for client.Client, but without requiring Context.
func (nt *NT) Create(obj client.Object, opts ...client.CreateOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("creating %s", fmtObj(obj))
	AddTestLabel(obj)
	return nt.Client.Create(nt.Context, obj, opts...)
}

// Update is identical to Update defined for client.Client, but without requiring Context.
func (nt *NT) Update(obj client.Object, opts ...client.UpdateOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("updating %s", fmtObj(obj))
	return nt.Client.Update(nt.Context, obj, opts...)
}

// Delete is identical to Delete defined for client.Client, but without requiring Context.
func (nt *NT) Delete(obj client.Object, opts ...client.DeleteOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("deleting %s", fmtObj(obj))
	return nt.Client.Delete(nt.Context, obj, opts...)
}

// DeleteAllOf is identical to DeleteAllOf defined for client.Client, but without requiring Context.
func (nt *NT) DeleteAllOf(obj client.Object, opts ...client.DeleteAllOfOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("deleting all of %T", obj)
	return nt.Client.DeleteAllOf(nt.Context, obj, opts...)
}

// MergePatch uses the object to construct a merge patch for the fields provided.
func (nt *NT) MergePatch(obj client.Object, patch string, opts ...client.PatchOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("Applying patch %s", patch)
	AddTestLabel(obj)
	return nt.Client.Patch(nt.Context, obj, client.RawPatch(types.MergePatchType, []byte(patch)), opts...)
}

// MustMergePatch is like MergePatch but will call t.Fatal if the patch fails.
func (nt *NT) MustMergePatch(obj client.Object, patch string, opts ...client.PatchOption) {
	nt.T.Helper()
	if err := nt.MergePatch(obj, patch, opts...); err != nil {
		nt.T.Fatal(err)
	}
}

// updateMetrics performs a diff between the current metrics and the previously
// recorded metrics to get all the metrics with new or changed measurements.
// If unchanged, the `declared_resources` and `watches` metrics are also added
// to the map because `ValidateMultiRepoMetrics` always validates those metrics.
//
// Diffing the metrics allows us to validate incremental changes to the metrics
// instead of having to validate against the entire set of metrics every time.
func (nt *NT) updateMetrics(prev testmetrics.ConfigSyncMetrics, parsedMetrics testmetrics.ConfigSyncMetrics) {
	newCsm := make(testmetrics.ConfigSyncMetrics)
	containsMetric := func(metrics []string, metric string) bool {
		for _, m := range metrics {
			if m == metric {
				return true
			}
		}
		return false
	}
	containsMeasurement := func(entries []testmetrics.Measurement, me testmetrics.Measurement) bool {
		for _, e := range entries {
			opt := cmp.Comparer(func(x, y tag.Tag) bool {
				return reflect.DeepEqual(x, y)
			})
			if cmp.Equal(me, e, opt) {
				return true
			}
		}
		return false
	}

	// These metrics are always validated so we need to add them to the map even
	// if their measurements haven't changed.
	validatedMetrics := []string{
		ocmetrics.APICallDurationView.Name,
		ocmetrics.ApplyDurationView.Name,
		ocmetrics.ApplyOperationsView.Name,
		ocmetrics.ReconcileDurationView.Name,
		ocmetrics.RemediateDurationView.Name,
		ocmetrics.DeclaredResourcesView.Name,
	}

	// Diff the metrics if previous metrics exist
	if prev != nil {
		for metric, measurements := range parsedMetrics {
			if containsMetric(validatedMetrics, metric) {
				newCsm[metric] = measurements
			} else {
				// Check whether the previous metrics map has the metric.
				if prevMeasurements, ok := prev[metric]; ok {
					for _, measurement := range measurements {
						// Check that the previous measurements for the metric does not have the
						// new measurement.
						if !containsMeasurement(prevMeasurements, measurement) {
							newCsm[metric] = append(newCsm[metric], measurement)
						}
					}
				} else {
					newCsm[metric] = measurements
				}
			}
		}
	} else {
		newCsm = parsedMetrics
	}
	// Save the result
	nt.ReconcilerMetrics = newCsm
}

// ValidateMetrics pulls the latest metrics, updates the metrics on NT and
// executes the parameter function.
func (nt *NT) ValidateMetrics(syncOption MetricsSyncOption, fn func() error) error {
	if nt.MultiRepo {
		nt.T.Log("validating metrics...")
		prevMetrics := nt.ReconcilerMetrics
		var once sync.Once
		duration, err := Retry(nt.DefaultWaitTimeout, func() error {
			duration, currentMetrics := nt.GetCurrentMetrics(syncOption)
			nt.updateMetrics(prevMetrics, currentMetrics)
			once.Do(func() {
				// Only log this once. Afterwards GetCurrentMetrics will return immediately.
				nt.T.Logf("waited %v for metrics to be current", duration)
			})
			return fn()
		})
		nt.T.Logf("waited %v for metrics to be valid", duration)
		if err != nil {
			return fmt.Errorf("validating metrics: %v", err)
		}
		return nil
	}
	return nil
}

// Validate returns an error if the indicated object does not exist.
//
// Validates the object against each of the passed Predicates, returning error
// if any Predicate fails.
func (nt *NT) Validate(name, namespace string, o client.Object, predicates ...Predicate) error {
	err := nt.Get(name, namespace, o)
	if err != nil {
		return err
	}
	for _, p := range predicates {
		err = p(o)
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidateNotFound returns an error if the indicated object exists.
//
// `o` must either be:
// 1) a struct pointer to the type of the object to search for, or
// 2) an unstructured.Unstructured with the type information filled in.
func (nt *NT) ValidateNotFound(name, namespace string, o client.Object) error {
	err := nt.Get(name, namespace, o)
	if err == nil {
		return errors.Errorf("%T %v %s/%s found", o, o.GetObjectKind().GroupVersionKind(), namespace, name)
	}
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// ValidateMultiRepoMetrics validates all the multi-repo metrics.
// It checks all non-error metrics are recorded with the correct tags and values.
func (nt *NT) ValidateMultiRepoMetrics(reconciler string, numResources int, gvkMetrics ...testmetrics.GVKMetric) error {
	if nt.MultiRepo {
		// Validate metrics emitted from the reconciler-manager.
		if err := nt.ReconcilerMetrics.ValidateReconcilerManagerMetrics(); err != nil {
			return err
		}
		// Validate non-typed and non-error metrics in the given reconciler.
		if err := nt.ReconcilerMetrics.ValidateReconcilerMetrics(reconciler, numResources); err != nil {
			return err
		}
		// Validate metrics that have a GVK "type" TagKey.
		for _, tm := range gvkMetrics {
			if err := nt.ReconcilerMetrics.ValidateGVKMetrics(reconciler, tm); err != nil {
				return errors.Wrapf(err, "%s %s operation", tm.GVK, tm.APIOp)
			}
		}
	}
	return nil
}

// ValidateErrorMetricsNotFound validates that no error metrics are emitted from
// any of the reconcilers.
func (nt *NT) ValidateErrorMetricsNotFound() error {
	if nt.MultiRepo {
		if err := nt.ReconcilerMetrics.ValidateErrorMetrics(DefaultRootReconcilerName); err != nil {
			return err
		}
		for name := range nt.RootRepos {
			if err := nt.ReconcilerMetrics.ValidateErrorMetrics(core.RootReconcilerName(name)); err != nil {
				return err
			}
		}
		for nn := range nt.NonRootRepos {
			if err := nt.ReconcilerMetrics.ValidateErrorMetrics(core.NsReconcilerName(nn.Namespace, nn.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateResourceOverrideCount validates that the `resource_override_count` metric exists
// for the correct reconciler.
func (nt *NT) ValidateResourceOverrideCount(reconciler, containerName, resourceType string, count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateResourceOverrideCount(reconciler, containerName, resourceType, count)
	}
	return nil
}

// ValidateResourceOverrideCountMissingTags checks that the `resource_override_count` metric misses the specific the tags.
func (nt *NT) ValidateResourceOverrideCountMissingTags(tags []tag.Tag) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateResourceOverrideCountMissingTags(tags)
	}
	return nil
}

// ValidateGitSyncDepthOverrideCount validates the `git_sync_depth_override_count` metric.
func (nt *NT) ValidateGitSyncDepthOverrideCount(count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateGitSyncDepthOverrideCount(count)
	}
	return nil
}

// ValidateNoSSLVerifyCount checks that the `no_ssl_verify_count` metric has the correct value.
func (nt *NT) ValidateNoSSLVerifyCount(count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateNoSSLVerifyCount(count)
	}
	return nil
}

// ValidateMetricNotFound validates that a metric does not exist.
func (nt *NT) ValidateMetricNotFound(metricName string) error {
	if nt.MultiRepo {
		if _, ok := nt.ReconcilerMetrics[metricName]; ok {
			return errors.Errorf("Found an unexpected metric: %s", metricName)
		}
	}
	return nil
}

// ValidateReconcilerErrors validates that the `reconciler_error` metric exists
// for the correct reconciler and the tagged component has the correct value.
func (nt *NT) ValidateReconcilerErrors(reconciler, component string) error {
	if nt.MultiRepo {
		var sourceCount, syncCount int
		switch component {
		case "source":
			sourceCount = 1
			syncCount = 0
		case "sync":
			sourceCount = 0
			syncCount = 1
		case "":
			sourceCount = 0
			syncCount = 0
		default:
			return errors.Errorf("unexpected component tag value: %v", component)
		}
		return nt.ReconcilerMetrics.ValidateReconcilerErrors(reconciler, sourceCount, syncCount)
	}
	return nil
}

// DefaultRootSha1Fn is the default function to retrieve the commit hash of the root repo.
func DefaultRootSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	// Get the repository this RootSync is syncing to, and ensure it is synced to HEAD.
	repo, exists := nt.RootRepos[nn.Name]
	if !exists {
		return "", fmt.Errorf("nt.RootRepos doesn't include RootSync %q", nn.Name)
	}
	return repo.Hash(), nil
}

// DefaultRepoSha1Fn is the default function to retrieve the commit hash of the namespace repo.
func DefaultRepoSha1Fn() Sha1Func {
	return func(nt *NT, nn types.NamespacedName) (string, error) {
		// Get the repository this RepoSync is syncing to, and ensure it is synced
		// to HEAD.
		repo, exists := nt.NonRootRepos[nn]
		if !exists {
			return "", fmt.Errorf("checked if nonexistent repo is synced")
		}
		return repo.Hash(), nil
	}
}

// WaitForRepoSyncs is a convenience method that waits for all repositories
// to sync.
//
// Unless you're testing pre-CSMR functionality related to in-cluster objects,
// you should be using this function to block on ConfigSync to sync everything.
//
// If you want to check the internals of specific objects (e.g. the error field
// of a RepoSync), use nt.Validate() - possibly in a Retry.
func (nt *NT) WaitForRepoSyncs(options ...WaitForRepoSyncsOption) {
	nt.T.Helper()

	waitForRepoSyncsOptions := newWaitForRepoSyncsOptions(nt.DefaultWaitTimeout, DefaultRootSha1Fn)
	for _, option := range options {
		option(&waitForRepoSyncsOptions)
	}

	syncTimeout := waitForRepoSyncsOptions.timeout

	if nt.MultiRepo {
		for name := range nt.RootRepos {
			syncDir := syncDirectory(waitForRepoSyncsOptions.syncDirectoryMap, RootSyncNN(name))
			nt.WaitForSync(kinds.RootSyncV1Beta1(), name,
				configmanagement.ControllerNamespace, syncTimeout,
				waitForRepoSyncsOptions.rootSha1Fn, RootSyncHasStatusSyncCommit,
				&SyncDirPredicatePair{syncDir, RootSyncHasStatusSyncDirectory})
		}

		syncNamespaceRepos := waitForRepoSyncsOptions.syncNamespaceRepos
		if syncNamespaceRepos {
			for nn := range nt.NonRootRepos {
				nt.WaitForSync(kinds.RepoSyncV1Beta1(), nn.Name, nn.Namespace,
					syncTimeout, DefaultRepoSha1Fn(), RepoSyncHasStatusSyncCommit, nil)
			}
		}
	} else {
		nt.WaitForSync(kinds.Repo(), "repo", "", syncTimeout,
			waitForRepoSyncsOptions.rootSha1Fn, RepoHasStatusSyncLatestToken, nil)
	}
}

// GetCurrentMetrics fetches metrics from the otel-collector ensuring that the
// metrics have been updated for with the most recent commit hashes.
func (nt *NT) GetCurrentMetrics(syncOptions ...MetricsSyncOption) (time.Duration, testmetrics.ConfigSyncMetrics) {
	nt.T.Helper()

	if nt.MultiRepo {
		var metrics testmetrics.ConfigSyncMetrics

		// Metrics are buffered and sent in batches to the collector.
		// So we may have to retry a few times until they're current.
		took, err := Retry(nt.DefaultWaitTimeout, func() error {
			var err error
			metrics, err = testmetrics.ParseMetrics(nt.otelCollectorPort)
			if err != nil {
				// Port forward again to fix intermittent "exit status 56" errors when
				// parsing from the port.
				nt.PortForwardOtelCollector()
				return err
			}

			for _, syncOption := range syncOptions {
				err = syncOption(&metrics)
				if err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			nt.T.Fatalf("unable to get latest metrics: %v", err)
		}

		return took, metrics
	}

	return 0, nil
}

// WaitForSync waits for the specified object to be synced.
//
// o returns a new object of the type to check is synced. It can't just be a
// struct pointer as calling .Get on the same struct pointer multiple times
// has undefined behavior.
//
// name and namespace identify the specific object to check.
//
// timeout specifies the maximum duration allowed for the object to sync.
//
// sha1Func is the function that dynamically computes the expected commit sha1.
//
// syncSha1 is a Predicate to use to tell whether the object is synced as desired.
//
// syncDirPair is a pair of sync dir and the corresponding predicate that tells
// whether it is synced to the expected directory.
// It will skip the validation if it is not provided.
func (nt *NT) WaitForSync(gvk schema.GroupVersionKind, name, namespace string, timeout time.Duration,
	sha1Func Sha1Func, syncSha1 func(string) Predicate, syncDirPair *SyncDirPredicatePair) {
	nt.T.Helper()

	// Wait for the repository to report it is synced.
	took, err := Retry(timeout, func() error {
		obj, err := nt.scheme.New(gvk)
		if err != nil {
			return fmt.Errorf("%w: got unrecognized GVK %v", ErrWrongType, gvk)
		}
		o, ok := obj.(client.Object)
		if !ok {
			// This means the GVK corresponded to a type registered in the Scheme
			// which is not a valid Kubernetes object. We expect the only way this
			// can happen is if gvk is for a List type, like NamespaceList.
			return errors.Wrapf(ErrWrongType, "trying to wait for List type to sync: %T", o)
		}

		var nn types.NamespacedName
		if namespace == "" {
			// If namespace is empty, that is the monorepo mode. So using the default root-sync.
			nn = DefaultRootRepoNamespacedName
		} else {
			nn = RepoSyncNN(namespace, name)
		}
		sha1, err := sha1Func(nt, nn)
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve sha1")
		}
		isSynced := []Predicate{syncSha1(sha1)}
		if syncDirPair != nil {
			isSynced = append(isSynced, syncDirPair.predicate(syncDirPair.dir))
		}

		return nt.Validate(name, namespace, o, isSynced...)
	})
	if err != nil {
		nt.T.Logf("failed after %v to wait for %s/%s %v to be synced", took, namespace, name, gvk)

		nt.T.Fatal(err)
	}
	nt.T.Logf("took %v to wait for %s/%s %v to be synced", took, namespace, name, gvk)

	// Automatically renew the Client. We don't have tests that depend on behavior
	// when the test's client is out of date, and if ConfigSync reports that
	// everything has synced properly then (usually) new types should be available.
	if gvk == kinds.Repo() || gvk == kinds.RepoSyncV1Beta1() {
		nt.RenewClient()
	}
}

// RenewClient gets a new Client for talking to the cluster.
//
// Required whenever we expect the set of available types on the cluster
// to change. Called automatically at the end of WaitForRootSync.
//
// The only reason to call this manually from within a test is if we expect a
// controller to create a CRD dynamically, or if the test requires applying a
// CRD directly to the API Server.
func (nt *NT) RenewClient() {
	nt.T.Helper()

	nt.Client = connect(nt.T, nt.Config, nt.scheme)
}

// Kubectl is a convenience method for calling kubectl against the
// currently-connected cluster. Returns STDOUT, and an error if kubectl exited
// abnormally.
//
// If you want to fail the test immediately on failure, use MustKubectl.
func (nt *NT) Kubectl(args ...string) ([]byte, error) {
	nt.T.Helper()

	prefix := []string{"--kubeconfig", nt.kubeconfigPath}
	args = append(prefix, args...)
	nt.DebugLogf("kubectl %s", strings.Join(args, " "))
	return exec.Command("kubectl", args...).CombinedOutput()
}

// MustKubectl fails the test immediately if the kubectl command fails. On
// success, returns STDOUT.
func (nt *NT) MustKubectl(args ...string) []byte {
	nt.T.Helper()

	out, err := nt.Kubectl(args...)
	if err != nil {
		nt.T.Log(append([]string{"kubectl"}, args...))
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}
	return out
}

// DebugLog is like nt.T.Log, but only prints the message if --debug is passed.
// Use for fine-grained information that is unlikely to cause failures in CI.
func (nt *NT) DebugLog(args ...interface{}) {
	if *e2e.Debug {
		nt.T.Log(args...)
	}
}

// DebugLogf is like nt.T.Logf, but only prints the message if --debug is passed.
// Use for fine-grained information that is unlikely to cause failures in CI.
func (nt *NT) DebugLogf(format string, args ...interface{}) {
	if *e2e.Debug {
		nt.T.Logf(format, args...)
	}
}

// PodLogs prints the logs from the specified deployment.
// If there is an error getting the logs for the specified deployment, prints
// the error.
func (nt *NT) PodLogs(namespace, deployment, container string, previousPodLog bool) {
	nt.T.Helper()

	args := []string{"logs", fmt.Sprintf("deployment/%s", deployment), "-n", namespace}
	if previousPodLog {
		args = append(args, "-p")
	}
	if container != "" {
		args = append(args, container)
	}
	out, err := nt.Kubectl(args...)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
	if err != nil {
		nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
		return
	}
	nt.T.Logf("%s\n%s", cmd, out)
}

// testLogs print the logs for the current container instances when `previousPodLog` is false.
// testLogs print the logs for the previous container instances if they exist when `previousPodLog` is true.
func (nt *NT) testLogs(previousPodLog bool) {
	// These pods/containers are noisy and rarely contain useful information:
	// - git-sync
	// - fs-watcher
	// - monitor
	// Don't merge with any of these uncommented, but feel free to uncomment
	// temporarily to see how presubmit responds.
	if nt.MultiRepo {
		nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.ManagerName, reconcilermanager.ManagerName, previousPodLog)
		nt.PodLogs(configmanagement.ControllerNamespace, configuration.ShortName, configuration.ShortName, previousPodLog)
		nt.PodLogs(configmanagement.ControllerNamespace, DefaultRootReconcilerName, reconcilermanager.Reconciler, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, DefaultRootReconcilerName, reconcilermanager.GitSync, previousPodLog)
		for nn := range nt.NonRootRepos {
			nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(nn.Namespace, nn.Name),
				reconcilermanager.Reconciler, previousPodLog)
			//nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.NsReconcilerName(ns), reconcilermanager.GitSync, previousPodLog)
		}
	} else {
		nt.PodLogs(configmanagement.ControllerNamespace, filesystem.GitImporterName, importer.Name, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, filesystem.GitImporterName, "fs-watcher", previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, filesystem.GitImporterName, reconcilermanager.GitSync, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, state.MonitorName, "", previousPodLog)
	}
}

// testPods prints the output of `kubectl get pods`, which includes a 'RESTARTS' column
// indicating how many times each pod has restarted. If a pod has restarted, the following
// two commands can be used to get more information:
//   1) kubectl get pods -n config-management-system -o yaml
//   2) kubectl logs deployment/<deploy-name> <container-name> -n config-management-system -p
func (nt *NT) testPods() {
	out, err := nt.Kubectl("get", "pods", "-n", configmanagement.ControllerNamespace)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	nt.T.Logf("kubectl get pods -n %s: \n%s", configmanagement.ControllerNamespace, string(out))
	if err != nil {
		nt.T.Log("error running `kubectl get pods`:", err)
	}
}

func (nt *NT) describeNotRunningTestPods() {
	cmPods := &corev1.PodList{}
	if err := nt.List(cmPods, client.InNamespace(configmanagement.ControllerNamespace)); err != nil {
		nt.T.Fatal(err)
		return
	}

	for _, pod := range cmPods.Items {
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == "Ready" {
				ready = (condition.Status == "True")
				break
			}
		}
		// Only describe pods that are NOT ready.
		if !ready {
			args := []string{"describe", "pod", pod.GetName(), "-n", pod.GetNamespace()}
			cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
			out, err := nt.Kubectl(args...)
			if err != nil {
				nt.T.Logf("error running `%s`: %s\n%s", cmd, err, out)
				continue
			}
			nt.T.Logf("%s\n%s", cmd, out)
		}
	}
}

// ApplyGatekeeperTestData is an exception to the "all test data is specified inline"
// rule. It isn't informative to literally have the CRD specifications in the
// test code, and we have strict type requirements on how the CRD is laid out.
func (nt *NT) ApplyGatekeeperTestData(file, crd string) error {
	nt.T.Logf("Adding gatekeeper CRD %s", crd)
	absPath := filepath.Join(baseDir, "e2e", "testdata", "gatekeeper", file)

	// We have to set validate=false because the default Gatekeeper YAMLs include
	// fields introduced in 1.13 and can't be applied without it, and we aren't
	// going to define our own compliant version.
	nt.MustKubectl("apply", "-f", absPath, "--validate=false")
	err := WaitForCRDs(nt, []string{crd})
	if err != nil {
		nt.RenewClient()
	}
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", absPath, "--ignore-not-found")
	})
	return err
}

// PortForwardOtelCollector forwards the otel-collector pod.
func (nt *NT) PortForwardOtelCollector() {
	if nt.MultiRepo {
		ocPods := &corev1.PodList{}
		// Retry otel-collector port-forwarding in case it is in the process of upgrade.
		took, err := Retry(60*time.Second, func() error {
			if err := nt.List(ocPods, client.InNamespace(ocmetrics.MonitoringNamespace)); err != nil {
				return err
			}
			if nPods := len(ocPods.Items); nPods != 1 {
				return fmt.Errorf("otel-collector: got len(podList.Items) = %d, want 1", nPods)
			}

			pod := ocPods.Items[0]
			if pod.Status.Phase != corev1.PodRunning {
				return fmt.Errorf("pod %q status is %q, want %q", pod.Name, pod.Status.Phase, corev1.PodRunning)
			}
			// The otel-collector forwarding port needs to be updated after otel-collector restarts or starts for the first time.
			// It sets otelCollectorPodName and otelCollectorPort to point to the current running pod that forwards the port.
			if pod.Name != nt.otelCollectorPodName {
				port, err := nt.ForwardToFreePort(ocmetrics.MonitoringNamespace, pod.Name, testmetrics.MetricsPort)
				if err != nil {
					return err
				}
				nt.otelCollectorPort = port
				nt.otelCollectorPodName = pod.Name
			}
			return nil
		})
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.T.Logf("took %v to wait for otel-collector port-forward", took)
	}
}

// ForwardToFreePort forwards a local port to a port on the pod and returns the
// local port chosen by kubectl.
func (nt *NT) ForwardToFreePort(ns, pod, port string) (int, error) {
	nt.T.Helper()

	cmd := exec.Command("kubectl", "--kubeconfig", nt.kubeconfigPath, "port-forward",
		"-n", ns, pod, port)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Start()
	if err != nil {
		return 0, err
	}
	if stderr.Len() != 0 {
		return 0, fmt.Errorf(stderr.String())
	}

	nt.T.Cleanup(func() {
		err := cmd.Process.Kill()
		if err != nil {
			nt.T.Errorf("killing port forward process: %v", err)
		}
	})

	localPort := 0
	// In CI, 1% of the time this takes longer than 20 seconds, so 30 seconds seems
	// like a reasonable amount of time to wait.
	took, err := Retry(30*time.Second, func() error {
		s := stdout.String()
		if !strings.Contains(s, "\n") {
			return fmt.Errorf("nothing written to stdout for kubectl port-forward, stdout=%s", s)
		}

		line := strings.Split(s, "\n")[0]

		// Sample output:
		// Forwarding from 127.0.0.1:44043
		_, err = fmt.Sscanf(line, "Forwarding from 127.0.0.1:%d", &localPort)
		if err != nil {
			nt.T.Fatalf("unable to parse port-forward output: %q", s)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	nt.T.Logf("took %v to wait for port-forward", took)

	return localPort, nil
}

func validateRootSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RootSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
	if err := validateError(statusErrs, code, message); err != nil {
		return err
	}
	if syncingCondition == nil {
		return fmt.Errorf("syncingCondition is nil")
	}
	if syncingCondition.Status == metav1.ConditionTrue {
		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
	}
	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
	}
	return nil
}

func validateRepoSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RepoSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
	if err := validateError(statusErrs, code, message); err != nil {
		return err
	}
	if syncingCondition == nil {
		return fmt.Errorf("syncingCondition is nil")
	}
	if syncingCondition.Status == metav1.ConditionTrue {
		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
	}
	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
	}
	return nil
}

func validateError(errs []v1beta1.ConfigSyncError, code string, message string) error {
	if len(errs) == 0 {
		return errors.Errorf("no errors present")
	}
	var codes []string
	for _, e := range errs {
		if e.Code == code {
			if message == "" || strings.Contains(e.ErrorMessage, message) {
				return nil
			}
		}
		codes = append(codes, e.Code)
	}
	return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
}

// WaitForRootSyncSourceError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSourceError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s source error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			if err := nt.Get(rs.GetName(), rs.GetNamespace(), rs); err != nil {
				return err
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRootSyncRenderingError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncRenderingError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Rendering.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.RenderingError})
		},
		opts...,
	)
}

// WaitForRootSyncSyncError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSyncError(rsName, code string, message string, ignoreSyncingCondition bool, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			if ignoreSyncingCondition {
				return validateError(rs.Status.Sync.Errors, code, message)
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSyncError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSyncError(ns, rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s rendering error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			return validateRepoSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSourceError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSourceError(ns, rsName, code, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s source error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()

			rs := fake.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			return validateRepoSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRepoSourceError waits until the given error (code and message) is present on the Repo resource
func (nt *NT) WaitForRepoSourceError(code string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("Repo source error code %s", code), nt.DefaultWaitTimeout,
		func() error {
			repo := &v1.Repo{}
			err := nt.Get("repo", "", repo)
			if err != nil {
				return err
			}
			errs := repo.Status.Source.Errors
			if len(errs) == 0 {
				return errors.Errorf("no errors present")
			}
			var codes []string
			for _, e := range errs {
				if e.Code == fmt.Sprintf("KNV%s", code) {
					return nil
				}
				codes = append(codes, e.Code)
			}
			return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
		},
		opts...,
	)
}

// WaitForRepoSourceErrorClear waits until the given error code disappears from the Repo resource
func (nt *NT) WaitForRepoSourceErrorClear(opts ...WaitOption) {
	Wait(nt.T, "Repo source errors cleared", nt.DefaultWaitTimeout,
		func() error {
			repo := &v1.Repo{}
			err := nt.Get("repo", "", repo)
			if err != nil {
				return err
			}
			errs := repo.Status.Source.Errors
			if len(errs) == 0 {
				return nil
			}
			var messages []string
			for _, e := range errs {
				messages = append(messages, e.ErrorMessage)
			}
			return errors.Errorf("got errors %s", strings.Join(messages, ", "))
		},
		opts...,
	)
}

// WaitForRepoImportErrorCode waits until the given error code is present on the Repo resource.
func (nt *NT) WaitForRepoImportErrorCode(code string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("Repo import error code %s", code), nt.DefaultWaitTimeout,
		func() error {
			r := &v1.Repo{}
			err := nt.Get("repo", "", r)

			if err != nil {
				return err
			}
			errs := r.Status.Import.Errors
			if len(errs) == 0 {
				return errors.Errorf("no errors present")
			}
			var codes []string
			for _, e := range errs {
				if e.Code == fmt.Sprintf("KNV%s", code) {
					return nil
				}
				codes = append(codes, e.Code)
			}
			return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
		},
		opts...,
	)
}

// WaitForRootSyncStalledError waits until the given Stalled error is present on the RootSync resource.
func (nt *NT) WaitForRootSyncStalledError(rsNamespace, rsName, reason, message string) {
	Wait(nt.T, fmt.Sprintf("RootSync %s/%s stalled error", rsNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RootSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNamespace,
				},
				TypeMeta: fake.ToTypeMeta(kinds.RootSyncV1Beta1()),
			}
			if err := nt.Get(rsName, rsNamespace, rs); err != nil {
				return err
			}
			stalledCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
			if stalledCondition == nil {
				return fmt.Errorf("stalled condition not found")
			}
			if stalledCondition.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected status.conditions['Stalled'].status is True, got %s", stalledCondition.Status)
			}
			if stalledCondition.Reason != reason {
				return fmt.Errorf("expected status.conditions['Stalled'].reason is %s, got %s", reason, stalledCondition.Reason)
			}
			if !strings.Contains(stalledCondition.Message, message) {
				return fmt.Errorf("expected status.conditions['Stalled'].message to contain %s, got %s", message, stalledCondition.Message)
			}
			return nil
		})
}

// WaitForRepoSyncStalledError waits until the given Stalled error is present on the RepoSync resource.
func (nt *NT) WaitForRepoSyncStalledError(rsNamespace, rsName, reason, message string) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s stalled error", rsNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RepoSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNamespace,
				},
				TypeMeta: fake.ToTypeMeta(kinds.RepoSyncV1Beta1()),
			}
			if err := nt.Get(rsName, rsNamespace, rs); err != nil {
				return err
			}
			stalledCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
			if stalledCondition == nil {
				return fmt.Errorf("stalled condition not found")
			}
			if stalledCondition.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected status.conditions['Stalled'].status is True, got %s", stalledCondition.Status)
			}
			if stalledCondition.Reason != reason {
				return fmt.Errorf("expected status.conditions['Stalled'].reason is %s, got %s", reason, stalledCondition.Reason)
			}
			if !strings.Contains(stalledCondition.Message, message) {
				return fmt.Errorf("expected status.conditions['Stalled'].message to contain %s, got %s", message, stalledCondition.Message)
			}
			return nil
		})
}

// SupportV1Beta1CRD checks if v1beta1 CRD is supported
// in the current testing cluster.
func (nt *NT) SupportV1Beta1CRD() (bool, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(nt.Config)
	if err != nil {
		return false, errors.Wrapf(err, "failed to create discoveryclient")
	}
	serverVersion, err := dc.ServerVersion()
	if err != nil {
		return false, errors.Wrapf(err, "failed to get server version")
	}
	// Use only major version and minor version in case other parts
	// may affect the comparison.
	v := fmt.Sprintf("v%s.%s", serverVersion.Major, serverVersion.Minor)
	cmp := strings.Compare(v, "v1.22")
	return cmp < 0, nil
}

// WaitOption is an optional parameter for Wait
type WaitOption func(wait *waitSpec)

type waitSpec struct {
	timeout time.Duration
	// failOnError is the flag to control whether to fail the test or not when errors occur.
	failOnError bool
}

// WaitTimeout provides the timeout option to Wait.
func WaitTimeout(timeout time.Duration) WaitOption {
	return func(wait *waitSpec) {
		wait.timeout = timeout
	}
}

// WaitNoFail sets failOnError to false so the Wait function only logs the error but not fails the test.
func WaitNoFail() WaitOption {
	return func(wait *waitSpec) {
		wait.failOnError = false
	}
}

// Wait provides a logged wait for condition to return nil with options for timeout.
// It fails the test on errors.
func Wait(t testing.NTB, opName string, timeout time.Duration, condition func() error, opts ...WaitOption) {
	t.Helper()

	wait := waitSpec{
		timeout:     timeout,
		failOnError: true,
	}
	for _, opt := range opts {
		opt(&wait)
	}

	// Wait for the repository to report it is synced.
	took, err := Retry(wait.timeout, condition)
	if err != nil {
		t.Logf("failed after %v to wait for %s", took, opName)
		if wait.failOnError {
			t.Fatal(err)
		}
	}
	t.Logf("took %v to wait for %s", took, opName)
}

// MetricsSyncOption determines where metrics will be synced to
type MetricsSyncOption func(csm *testmetrics.ConfigSyncMetrics) error

// SyncMetricsToLatestCommit syncs metrics to the latest commit
func SyncMetricsToLatestCommit(nt *NT) MetricsSyncOption {
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		for nn := range nt.RootRepos {
			if err := metrics.ValidateMetricsCommitApplied(nt.RootRepos[nn].Hash()); err != nil {
				return err
			}
		}

		for ns := range nt.NonRootRepos {
			if err := metrics.ValidateMetricsCommitApplied(nt.NonRootRepos[ns].Hash()); err != nil {
				return err
			}
		}

		return nil
	}
}

// SyncMetricsToReconcilerSourceError syncs metrics to a reconciler source error
func SyncMetricsToReconcilerSourceError(reconciler string) MetricsSyncOption {
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		return metrics.ValidateReconcilerErrors(reconciler, 1, 0)
	}
}

// SyncMetricsToReconcilerSyncError syncs metrics to a reconciler sync error
func SyncMetricsToReconcilerSyncError(reconciler string) MetricsSyncOption {
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		return metrics.ValidateReconcilerErrors(reconciler, 0, 1)
	}
}

type waitForRepoSyncsOptions struct {
	timeout            time.Duration
	syncNamespaceRepos bool
	rootSha1Fn         Sha1Func
	syncDirectoryMap   map[types.NamespacedName]string
}

func newWaitForRepoSyncsOptions(timeout time.Duration, fn Sha1Func) waitForRepoSyncsOptions {
	return waitForRepoSyncsOptions{
		timeout:            timeout,
		syncNamespaceRepos: true,
		rootSha1Fn:         fn,
		syncDirectoryMap:   map[types.NamespacedName]string{},
	}
}

// WaitForRepoSyncsOption is an optional parameter for WaitForRepoSyncs.
type WaitForRepoSyncsOption func(*waitForRepoSyncsOptions)

// WithTimeout provides the timeout to WaitForRepoSyncs.
func WithTimeout(timeout time.Duration) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.timeout = timeout
	}
}

// Sha1Func is the function type that retrieves the commit sha1.
type Sha1Func func(nt *NT, nn types.NamespacedName) (string, error)

// WithRootSha1Func provides the function to get commit sha1 to WaitForRepoSyncs.
func WithRootSha1Func(fn Sha1Func) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.rootSha1Fn = fn
	}
}

// RootSyncOnly specifies that only the root-sync repo should be synced.
func RootSyncOnly() WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.syncNamespaceRepos = false
	}
}

// SyncDirPredicatePair is a pair of the sync directory and the predicate.
type SyncDirPredicatePair struct {
	dir       string
	predicate func(string) Predicate
}

// WithSyncDirectoryMap provides a map of RootSync|RepoSync and the corresponding sync directory.
// The function is used to get the sync directory based on different RootSync|RepoSync name.
func WithSyncDirectoryMap(syncDirectoryMap map[types.NamespacedName]string) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.syncDirectoryMap = syncDirectoryMap
	}
}

// syncDirectory returns the sync directory if RootSync|RepoSync is in the map.
// Otherwise, it returns the default sync directory: acme.
func syncDirectory(syncDirectoryMap map[types.NamespacedName]string, nn types.NamespacedName) string {
	if syncDir, found := syncDirectoryMap[nn]; found {
		return syncDir
	}
	return AcmeDir
}

// RemoteRootRepoSha1Fn returns .status.lastSyncedCommit as the latest sha1.
func RemoteRootRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	rs := &v1beta1.RootSync{}
	if err := nt.Get(nn.Name, nn.Namespace, rs); err != nil {
		return "", err
	}
	return rs.Status.LastSyncedCommit, nil
}

// RemoteNsRepoSha1Fn returns .status.lastSyncedCommit as the latest sha1 for the Namespace Repo.
func RemoteNsRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	rs := &v1beta1.RepoSync{}
	if err := nt.Get(nn.Name, nn.Namespace, rs); err != nil {
		return "", err
	}
	return rs.Status.LastSyncedCommit, nil
}
