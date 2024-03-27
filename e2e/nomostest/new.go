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
	"path/filepath"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// fileMode is the file mode to use for all operations.
//
// Go is inconsistent about which user/group it runs commands under, so
// anything less will either:
// 1) Make git operations not work as expected, or
// 2) Cause ssh-keygen to fail.
const fileMode = os.ModePerm

// NomosE2E is the subdirectory inside the filesystem's temporary directory in
// which we write test data.
const NomosE2E = "nomos-e2e"

// newOptStruct initializes the nomostest options.
func newOptStruct(testName, tmpDir string, ntOptions ...ntopts.Opt) *ntopts.New {
	// TODO: we should probably put ntopts.New members inside of NT use the go-convention of mutating NT with option functions.
	optsStruct := &ntopts.New{
		Name:   testName,
		TmpDir: tmpDir,
		Nomos: ntopts.Nomos{
			SourceFormat: filesystem.SourceFormatHierarchy,
			MultiRepo:    true,
		},
		MultiRepo: ntopts.MultiRepo{
			NamespaceRepos: make(map[types.NamespacedName]ntopts.RepoOpts),
			RootRepos:      map[string]ntopts.RepoOpts{configsync.RootSyncName: {}},
			// Default to 1m to keep tests fast.
			// To override, use WithReconcileTimeout.
			ReconcileTimeout: pointer.Duration(1 * time.Minute),
		},
	}
	for _, opt := range ntOptions {
		opt(optsStruct)
	}

	return optsStruct
}

var featureDurations map[nomostesting.Feature]time.Duration
var featureDurationMux sync.Mutex

// PrintFeatureDurations prints the accumulative time duration testing each
// feature. This is intended to be called at the end of the test suite.
func PrintFeatureDurations() {
	featureDurationMux.Lock()
	defer featureDurationMux.Unlock()
	if featureDurations == nil {
		fmt.Println("No feature durations were recorded")
		return
	}
	fmt.Println("Feature durations:")
	for feature, duration := range featureDurations {
		fmt.Printf("%s: %s\n", feature, duration)
	}
}

// NewTestWrapper creates a test wrapper for use by the tests. This applies a common
// set of rules across tests, such as test features for selecting sets of tests
// or rules for skipping certain tests (e.g. StressTest).
// Usually tests should use nomostest.New but this is surfaced as a public
// method for test cases which do not need all the features of NT, such
// as access to a k8s cluster.
func NewTestWrapper(t *testing.T, testFeature nomostesting.Feature, ntOptions ...ntopts.Opt) (*nomostesting.Wrapper, *ntopts.New) {
	e2e.EnableParallel(t)
	tw := nomostesting.New(t, testFeature)
	optsStruct := newOptStruct(TestClusterName(tw), TestDir(tw), ntOptions...)
	optsStruct.EvaluateSkipOptions(tw)
	start := time.Now()
	tw.Cleanup(func() {
		duration := time.Since(start)
		featureDurationMux.Lock()
		defer featureDurationMux.Unlock()
		if featureDurations == nil {
			featureDurations = make(map[nomostesting.Feature]time.Duration)
		}
		if accumulative, ok := featureDurations[testFeature]; ok {
			duration += accumulative
		}
		featureDurations[testFeature] = duration
	})
	return tw, optsStruct
}

// New establishes a connection to a test cluster and prepares it for testing.
func New(t *testing.T, testFeature nomostesting.Feature, ntOptions ...ntopts.Opt) *NT {
	t.Helper()
	tw, optsStruct := NewTestWrapper(t, testFeature, ntOptions...)

	var nt *NT
	if *e2e.ShareTestEnv {
		nt = SharedTestEnv(tw, optsStruct)
	} else {
		nt = FreshTestEnv(tw, optsStruct)
	}
	if !optsStruct.SkipConfigSyncInstall {
		setupTestCase(nt, optsStruct)
	}
	return nt
}

// SharedTestEnv connects to a shared test cluster.
func SharedTestEnv(t nomostesting.NTB, opts *ntopts.New) *NT {
	t.Helper()

	sharedNt := SharedNT(t)
	// Set t on the logger to ensure proper interleaving of logs
	sharedNt.Logger.SetNTBForTest(t)
	t.Logf("using shared test env %s", sharedNt.ClusterName)

	nt := &NT{
		Context:                 sharedNt.Context,
		T:                       t,
		Logger:                  sharedNt.Logger,
		Shell:                   sharedNt.Shell,
		ClusterName:             sharedNt.ClusterName,
		TmpDir:                  opts.TmpDir,
		Config:                  sharedNt.Config,
		repoSyncPermissions:     opts.RepoSyncPermissions,
		KubeClient:              sharedNt.KubeClient,
		Watcher:                 sharedNt.Watcher,
		WatchClient:             sharedNt.WatchClient,
		IsGKEAutopilot:          sharedNt.IsGKEAutopilot,
		DefaultWaitTimeout:      sharedNt.DefaultWaitTimeout,
		DefaultReconcileTimeout: opts.ReconcileTimeout,
		kubeconfigPath:          sharedNt.kubeconfigPath,
		ReconcilerPollingPeriod: sharedNt.ReconcilerPollingPeriod,
		HydrationPollingPeriod:  sharedNt.HydrationPollingPeriod,
		RootRepos:               sharedNt.RootRepos,
		NonRootRepos:            sharedNt.NonRootRepos,
		MetricsExpectations:     sharedNt.MetricsExpectations,
		gitPrivateKeyPath:       sharedNt.gitPrivateKeyPath,
		gitCACertPath:           sharedNt.gitCACertPath,
		registryCACertPath:      sharedNt.registryCACertPath,
		Scheme:                  sharedNt.Scheme,
		RemoteRepositories:      sharedNt.RemoteRepositories,
		WebhookDisabled:         sharedNt.WebhookDisabled,
		GitProvider:             sharedNt.GitProvider,
		OCIProvider:             sharedNt.OCIProvider,
		HelmProvider:            sharedNt.HelmProvider,
		HelmClient:              sharedNt.HelmClient,
		OCIClient:               sharedNt.OCIClient,
	}

	if opts.SkipConfigSyncInstall {
		return nt
	}

	nt.detectGKEAutopilot(opts.SkipAutopilot)

	t.Cleanup(func() {
		if t.Failed() {
			nt.printTestLogs()
		}
		// Reset after `printTestLogs` to catch pods status and logs before resetting.
		nt.T.Log("[RESET] SharedTestEnv after test")
		if err := Reset(nt); err != nil {
			nt.T.Errorf("[RESET] Failed to reset test environment: %v", err)
			// Print test logs if reset fails.
			nt.printTestLogs()
		}
	})

	nt.T.Log("[RESET] SharedTestEnv before test")
	if err := Reset(nt); err != nil {
		nt.T.Fatalf("[RESET] Failed to reset test environment: %v", err)
		// No need to print test logs here because the cleanup block will print them.
	}
	// a previous e2e test may stop the Config Sync webhook, so always call `InstallWebhook` here to make sure the test starts
	// with the webhook enabled.
	if err := InstallWebhook(nt); err != nil {
		nt.T.Fatal(err)
	}
	return nt
}

// FreshTestEnv establishes a connection to a test cluster based on the passed
//
// options.
//
// Marks the test as parallel. For now we have no tests which *can't* be made
// parallel; if we need that in the future we can make a version of this
// function that doesn't do this. As below keeps us from forgetting to mark
// tests as parallel, and unnecessarily waiting.
//
// The following are guaranteed to be available when this function returns:
// 1) A connection to the Kubernetes cluster.
// 2) A functioning git server hosted on the cluster.
// 3) A fresh ACM installation.
func FreshTestEnv(t nomostesting.NTB, opts *ntopts.New) *NT {
	t.Helper()

	RestConfig(t, opts)
	// Increase the QPS for the clients used by the e2e tests.
	// This does not affect the client used by Config Sync itself.
	opts.RESTConfig.QPS = 50
	opts.RESTConfig.Burst = 75

	scheme := newScheme(t)
	ctx := context.Background()
	logger := testlogger.New(t, *e2e.Debug)

	shell := &testshell.TestShell{
		Context: ctx,
		Env:     append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", opts.KubeconfigPath)),
		Logger:  logger,
	}
	helmClient := testshell.NewHelmClient(opts.TmpDir, shell)
	ociClient := testshell.NewOCIClient(opts.TmpDir, shell)

	webhookDisabled := false
	nt := &NT{
		Context:                 ctx,
		T:                       t,
		Logger:                  logger,
		Shell:                   shell,
		ClusterName:             opts.ClusterName,
		TmpDir:                  opts.TmpDir,
		Config:                  opts.RESTConfig,
		repoSyncPermissions:     opts.RepoSyncPermissions,
		DefaultReconcileTimeout: opts.ReconcileTimeout,
		kubeconfigPath:          opts.KubeconfigPath,
		RootRepos:               make(map[string]*gitproviders.Repository),
		NonRootRepos:            make(map[types.NamespacedName]*gitproviders.Repository),
		MetricsExpectations:     testmetrics.NewSyncSetExpectations(t, scheme),
		Scheme:                  scheme,
		RemoteRepositories:      make(map[types.NamespacedName]*gitproviders.Repository),
		WebhookDisabled:         &webhookDisabled,
		GitProvider:             gitproviders.NewGitProvider(t, *e2e.GitProvider, opts.ClusterName, logger, shell),
		OCIProvider:             registryproviders.NewOCIProvider(*e2e.OCIProvider, opts.ClusterName, shell, ociClient),
		HelmProvider:            registryproviders.NewHelmProvider(*e2e.HelmProvider, opts.ClusterName, shell, helmClient),
		HelmClient:              helmClient,
		OCIClient:               ociClient,
	}

	// TODO: Try speeding up the reconciler and hydration polling.
	// It seems that speeding them up too much can cause failures in
	// our tests, so we will have to experiment and investigate to
	// see how much we can speed it up.
	// See original discussion in https://github.com/GoogleContainerTools/kpt-config-sync/pull/707#discussion_r1240911054

	// Speed up the delay between sync attempts to speed up testing
	// (default: 15s), but leave the remediator (paused while syncing) and retry
	// code (1s delay) time to execute between sync attempts.
	nt.ReconcilerPollingPeriod = 15 * time.Second
	// Speed up the delay between rendering attempts to speed up testing (default: 5s)
	nt.HydrationPollingPeriod = 5 * time.Second

	// init the Client & WatchClient
	nt.RenewClient()

	// Create workload to disable scale-to-zero.
	// Scale-to-zero breaks API discovery on GKE Autopilot: b/209800496S
	if *e2e.GKEAutopilot {
		if err := applyAutoPilotKeepAlive(nt); err != nil {
			nt.T.Fatal(err)
		}
	}

	if opts.SkipConfigSyncInstall {
		return nt
	}

	nt.detectGKEAutopilot(opts.SkipAutopilot)

	if nt.IsGKEAutopilot {
		nt.DefaultWaitTimeout = 10 * time.Minute
	} else {
		nt.DefaultWaitTimeout = 6 * time.Minute
	}

	// Longer default wait timeout for stress tests
	// Particularly the reset/finalizer takes a long time for the stress tests
	// Current slowest test is TestStressLargeRequest
	if *e2e.Stress {
		nt.DefaultWaitTimeout = 30 * time.Minute
	}

	if *e2e.TestCluster == e2e.Kind {
		// We're using an ephemeral Kind cluster, so connect to the local Docker
		// repository. No need to clean before/after as these tests only exist for
		// a single test.
		connectToLocalRegistry(nt)
	}
	t.Cleanup(func() {
		if opts.IsEphemeralCluster {
			nt.T.Logf("Skipping cleanup because cluster will be destroyed (--destroy-clusters=%s)", *e2e.DestroyClusters)
			return
		} else if t.Failed() && *e2e.Debug {
			nt.T.Logf("Skipping cleanup because test failed with --debug")
			return
		}
		// We aren't deleting the cluster after the test, so clean up
		nt.T.Log("[CLEANUP] FreshTestEnv after test")
		if err := Clean(nt); err != nil {
			nt.T.Errorf("[CLEANUP] Failed to clean test environment: %v", err)
		}
	})
	if *e2e.CreateClusters != e2e.CreateClustersEnabled {
		// We may not be using a fresh cluster, so make sure the cluster is
		// cleaned before running the test.
		nt.T.Log("[CLEANUP] FreshTestEnv before test")
		if err := Clean(nt); err != nil {
			nt.T.Fatalf("[CLEANUP] Failed to clean test environment: %v", err)
		}

	}

	t.Cleanup(func() {
		DeleteRemoteRepos(nt)
	})

	// You can't add Secrets to Namespaces that don't exist, so create them now.
	if err := nt.KubeClient.Create(fake.NamespaceObject(configmanagement.ControllerNamespace)); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.KubeClient.Create(fake.NamespaceObject(configmanagement.MonitoringNamespace)); err != nil {
		nt.T.Fatal(err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			nt.printTestLogs()
		}
	})

	tg := taskgroup.New()
	tg.Go(func() error {
		return setupGit(nt)
	})
	tg.Go(func() error {
		return setupRegistry(nt)
	})
	tg.Go(func() error {
		return installConfigSync(nt, opts.Nomos)
	})
	tg.Go(func() error {
		return installPrometheus(nt)
	})
	if *e2e.KCC {
		tg.Go(func() error {
			return setupConfigConnector(nt)
		})
	}
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	return nt
}

func applyAutoPilotKeepAlive(nt *NT) error {
	yamlPath := "../testdata/autopilot-keepalive/deployment.yaml"
	yamlBytes, err := os.ReadFile(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to read file %q: %w", yamlPath, err)
	}
	uObj := &unstructured.Unstructured{}
	if err := yaml.Unmarshal(yamlBytes, uObj); err != nil {
		return fmt.Errorf("failed to decode %q as yaml: %w", yamlPath, err)
	}
	// Apply with nt.KubeClient.Client directly, not nt.KubeClient.Apply
	// to avoid adding the test label, to avoid deletion during cleanup
	nt.Logger.Infof("applying %s", yamlPath)
	patchOpts := []client.PatchOption{client.FieldOwner(FieldManager), client.ForceOwnership}
	if err := nt.KubeClient.Client.Patch(nt.Context, uObj, client.Apply, patchOpts...); err != nil {
		return fmt.Errorf("failed to apply %s: %w", kinds.ObjectSummary(uObj), err)
	}
	return nil
}

func setupTestCase(nt *NT, opts *ntopts.New) {
	nt.T.Log("[SETUP] New test case")
	// init all repositories. This ensures that:
	// - local repo is initialized and empty
	// - remote repo exists (except for LocalProvider, which is done in portForwardGitServer)
	for name := range opts.RootRepos {
		nt.RootRepos[name] = initRepository(nt, gitproviders.RootRepo, RootSyncNN(name), opts.SourceFormat)
	}
	for nsr := range opts.NamespaceRepos {
		nt.NonRootRepos[nsr] = initRepository(nt, gitproviders.NamespaceRepo, nsr, filesystem.SourceFormatUnstructured)
	}
	// set up port forward if using in-cluster git server
	if *e2e.GitProvider == e2e.Local {
		nt.portForwardGitServer()
	}
	// Setup registry authentication, port-forwarding, and cleanup
	if err := setupRegistryClient(nt, opts); err != nil {
		nt.T.Fatalf("configuring registry client: %v", err)
	}
	// The following prerequisites have been met, so we can now push commits
	// - local repo initialized
	// - remote repo exists
	// - port forward started (only applicable to in-cluster git server)
	for name := range opts.RootRepos {
		initialCommit(nt, gitproviders.RootRepo, RootSyncNN(name), opts.SourceFormat)
	}
	for nsr := range opts.NamespaceRepos {
		initialCommit(nt, gitproviders.NamespaceRepo, nsr, filesystem.SourceFormatUnstructured)
	}

	if opts.InitialCommit != nil {
		rootSyncNN := RootSyncNN(configsync.RootSyncName)
		for path, obj := range opts.InitialCommit.Files {
			nt.Must(nt.RootRepos[configsync.RootSyncName].Add(path, obj))
			// Some source objects are not included in the declared resources.
			if testmetrics.IsObjectDeclarable(obj) {
				// Some source objects are included in the declared resources, but not applied.
				nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, obj)
			}
		}
		nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(opts.InitialCommit.Message))
	}

	// First wait for CRDs to be established.
	err := WaitForCRDs(nt, multiRepoCRDs)
	if err != nil {
		nt.T.Fatalf("waiting for ConfigSync CRDs to become established: %v", err)
	}

	// ConfigSync custom types weren't available when the cluster was initially
	// created. Create a new Client, since it'll automatically be configured to
	// understand the Repo and RootSync types as ConfigSync is now installed.
	nt.RenewClient()

	// Create the RootSync if it doesn't exist, and wait for it to be Synced.
	if err := WaitForConfigSyncReady(nt); err != nil {
		nt.T.Fatalf("waiting for ConfigSync Deployments to become available: %v", err)
	}

	nt.portForwardOtelCollector()
	nt.portForwardPrometheus()

	nt.Control = opts.Control
	switch opts.Control {
	case ntopts.DelegatedControl:
		setupDelegatedControl(nt)
	case ntopts.CentralControl:
		setupCentralizedControl(nt)
	default:
		nt.Control = ntopts.CentralControl
		// Most tests don't care about centralized/delegated control, but can
		// specify the behavior if that distinction is under test.
		setupCentralizedControl(nt)
	}

	// Wait for all RootSyncs and all RepoSyncs to be reconciled
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	// Validate all expected syncs exist (some may have been deployed by others)
	if err := validateRootSyncsExist(nt); err != nil {
		nt.T.Fatal(err)
	}
	if err := validateRepoSyncsExist(nt); err != nil {
		nt.T.Fatal(err)
	}
	// Validate metrics from each reconciler
	if err := ValidateStandardMetrics(nt); err != nil {
		nt.T.Fatal(err)
	}
}

// TestDir creates a unique temporary directory for the E2E test.
//
// Returned directory is absolute and OS-specific.
func TestDir(t nomostesting.NTB) string {
	t.Helper()

	name := testDirName(t)
	err := os.MkdirAll(filepath.Join(os.TempDir(), NomosE2E), fileMode)
	if err != nil {
		t.Fatalf("creating nomos-e2e tmp directory: %v", err)
	}
	tmpDir, err := os.MkdirTemp(filepath.Join(os.TempDir(), NomosE2E), name)
	if err != nil {
		t.Fatalf("creating nomos-e2e tmp test subdirectory %s: %v", tmpDir, err)
	}
	t.Cleanup(func() {
		if t.Failed() && *e2e.Debug {
			t.Errorf("temporary directory: %s", tmpDir)
			return
		}
		err := os.RemoveAll(tmpDir)
		if err != nil {
			// If you're seeing this error, the test does something that prevents
			// cleanup. This is a potential leak for interaction between tests.
			t.Errorf("removing temporary directory %q: %v", tmpDir, err)
		}
	})
	t.Logf("created temporary directory %q", tmpDir)
	return tmpDir
}
