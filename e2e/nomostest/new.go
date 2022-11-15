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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
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
func newOptStruct(testName, tmpDir string, t nomostesting.NTB, ntOptions ...ntopts.Opt) *ntopts.New {
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
		},
	}
	for _, opt := range ntOptions {
		opt(optsStruct)
	}

	e2eTest := !optsStruct.LoadTest && !optsStruct.StressTest
	if !*e2e.E2E && e2eTest {
		t.Skip("Test skipped since it is an e2e test")
	}

	if !*e2e.Load && optsStruct.LoadTest {
		t.Skip("Test skipped since it is a load test")
	}

	if !*e2e.Stress && optsStruct.StressTest {
		t.Skip("Test skipped since it is a stress test")
	}

	if !*e2e.Kcc && optsStruct.KccTest {
		t.Skip("Test skipped since it is a KCC test")
	}

	if !*e2e.GceNode && optsStruct.GCENodeTest {
		t.Skip("Test skipped since it is a test for GCENode auth type, which requires a GKE cluster without workload identity")
	}

	if *e2e.GceNode && !optsStruct.GCENodeTest {
		t.Skip("Test skipped for non-gcenode auth types")
	}

	if *e2e.GitProvider != e2e.Local && optsStruct.SkipNonLocalGitProvider {
		t.Skip("Test skipped for non-local GitProvider types")
	}

	if optsStruct.RESTConfig == nil {
		RestConfig(t, optsStruct)
		// Increase the QPS for the clients used by the e2e tests.
		// This does not affect the client used by Config Sync itself.
		optsStruct.RESTConfig.QPS = 50
		optsStruct.RESTConfig.Burst = 75
	}

	return optsStruct
}

func skipTestOnAutopilotCluster(nt *NT, skipAutopilot bool) bool {
	isGKEAutopilot, err := util.IsGKEAutopilotCluster(nt.Client)
	if err != nil {
		nt.T.Fatal(err)
	}
	if isGKEAutopilot && skipAutopilot {
		nt.T.Skip("Test skipped when running on Autopilot clusters")
	}
	return isGKEAutopilot
}

// New establishes a connection to a test cluster and prepares it for testing.
func New(t *testing.T, testFeature nomostesting.Feature, ntOptions ...ntopts.Opt) *NT {
	t.Helper()
	e2e.EnableParallel(t)
	tw := nomostesting.New(t, testFeature)

	if *e2e.ShareTestEnv {
		sharedNt := SharedNT(tw)
		t.Logf("using shared test env %s", sharedNt.ClusterName)
		ntOptions = append(ntOptions, ntopts.WithRestConfig(sharedNt.Config))
	}

	optsStruct := newOptStruct(TestClusterName(tw), TestDir(tw), tw, ntOptions...)

	if *e2e.ShareTestEnv {
		return SharedTestEnv(tw, optsStruct)
	}
	return FreshTestEnv(tw, optsStruct)
}

// SharedTestEnv connects to a shared test cluster.
func SharedTestEnv(t nomostesting.NTB, opts *ntopts.New) *NT {
	t.Helper()

	sharedNt := SharedNT(t)
	nt := &NT{
		Context:                 sharedNt.Context,
		T:                       t,
		ClusterName:             opts.Name,
		TmpDir:                  opts.TmpDir,
		Config:                  opts.RESTConfig,
		repoSyncPermissions:     opts.RepoSyncPermissions,
		Client:                  sharedNt.Client,
		IsGKEAutopilot:          sharedNt.IsGKEAutopilot,
		DefaultWaitTimeout:      sharedNt.DefaultWaitTimeout,
		DefaultReconcileTimeout: sharedNt.DefaultReconcileTimeout,
		DefaultMetricsTimeout:   sharedNt.DefaultMetricsTimeout,
		kubeconfigPath:          sharedNt.kubeconfigPath,
		ReconcilerPollingPeriod: sharedNt.ReconcilerPollingPeriod,
		HydrationPollingPeriod:  sharedNt.HydrationPollingPeriod,
		RootRepos:               sharedNt.RootRepos,
		NonRootRepos:            make(map[types.NamespacedName]*Repository),
		gitPrivateKeyPath:       sharedNt.gitPrivateKeyPath,
		caCertPath:              sharedNt.caCertPath,
		gitRepoPort:             sharedNt.gitRepoPort,
		scheme:                  sharedNt.scheme,
		otelCollectorPort:       sharedNt.otelCollectorPort,
		otelCollectorPodName:    sharedNt.otelCollectorPodName,
		ReconcilerMetrics:       make(testmetrics.ConfigSyncMetrics),
		GitProvider:             sharedNt.GitProvider,
		RemoteRepositories:      sharedNt.RemoteRepositories,
		WebhookDisabled:         sharedNt.WebhookDisabled,
	}

	if opts.SkipConfigSyncInstall {
		return nt
	}

	// Print container logs in its own cleanup block to catch fatal errors from
	// tests and test setup (including resetSyncedRepos).
	t.Cleanup(func() {
		if t.Failed() {
			nt.printTestLogs()
		}
	})
	t.Cleanup(func() {
		// Reset the otel-collector pod name to get a new forwarding port because the current process is killed.
		nt.otelCollectorPodName = ""
		nt.T.Log("`resetSyncedRepos` after a test as a part of `Cleanup` on SharedTestEnv")
		resetSyncedRepos(nt, opts)
	})

	skipTestOnAutopilotCluster(nt, opts.SkipAutopilot)

	nt.T.Log("`resetSyncedRepos` before a test on SharedTestEnv")
	resetSyncedRepos(nt, opts)
	// a previous e2e test may stop the Config Sync webhook, so always call `installWebhook` here to make sure the test starts
	// with the webhook enabled.
	installWebhook(nt)
	setupTestCase(nt, opts)
	return nt
}

func resetSyncedRepos(nt *NT, opts *ntopts.New) {
	nnList := nt.NonRootRepos
	// clear the namespace resources in the namespace repo to avoid admission validation failure.
	resetNamespaceRepos(nt)
	resetRootRepos(nt, opts.SourceFormat)

	deleteRootRepos(nt)
	deleteNamespaceRepos(nt)
	// delete the out-of-sync namespaces in case they're set up in the delegated mode.
	for nn := range nnList {
		revokeRepoSyncNamespace(nt, nn.Namespace)
	}
	nt.NonRootRepos = map[types.NamespacedName]*Repository{}
	for name := range nt.RootRepos {
		if name != configsync.RootSyncName {
			delete(nt.RootRepos, name)
		}
	}
	nt.WaitForRepoSyncs()
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

	scheme := newScheme(t)
	c := connect(t, opts.RESTConfig, scheme)
	ctx := context.Background()

	webhookDisabled := false
	nt := &NT{
		Context:                 ctx,
		T:                       t,
		ClusterName:             opts.Name,
		TmpDir:                  opts.TmpDir,
		Config:                  opts.RESTConfig,
		repoSyncPermissions:     opts.RepoSyncPermissions,
		Client:                  c,
		DefaultReconcileTimeout: 1 * time.Minute,
		kubeconfigPath:          opts.KubeconfigPath,
		ReconcilerPollingPeriod: 50 * time.Millisecond,
		HydrationPollingPeriod:  50 * time.Millisecond,
		RootRepos:               make(map[string]*Repository),
		NonRootRepos:            make(map[types.NamespacedName]*Repository),
		scheme:                  scheme,
		ReconcilerMetrics:       make(testmetrics.ConfigSyncMetrics),
		GitProvider:             gitproviders.NewGitProvider(t, *e2e.GitProvider),
		RemoteRepositories:      make(map[types.NamespacedName]*Repository),
		WebhookDisabled:         &webhookDisabled,
	}

	if opts.SkipConfigSyncInstall {
		return nt
	}

	nt.IsGKEAutopilot = skipTestOnAutopilotCluster(nt, opts.SkipAutopilot)
	if nt.IsGKEAutopilot {
		nt.DefaultWaitTimeout = 10 * time.Minute
		nt.DefaultMetricsTimeout = 3 * time.Minute
	} else {
		nt.DefaultWaitTimeout = 6 * time.Minute
		nt.DefaultMetricsTimeout = 1 * time.Minute
	}

	if *e2e.TestCluster == e2e.Kind {
		// We're using an ephemeral Kind cluster, so connect to the local Docker
		// repository. No need to clean before/after as these tests only exist for
		// a single test.
		connectToLocalRegistry(nt)
		checkImages(nt.T)
	} else {
		// We aren't using an ephemeral Kind cluster, so make sure the cluster is
		// clean before and after running the test.
		t.Log("`Clean` before running the test on FreshTestEnv")
		Clean(nt, true)
		t.Cleanup(func() {
			// Clean the cluster now that the test is over.
			t.Log("`Clean` after running the test on FreshTestEnv")
			Clean(nt, false)
		})
	}

	t.Cleanup(func() {
		DeleteRemoteRepos(nt)
	})

	// You can't add Secrets to Namespaces that don't exist, so create them now.
	if err := nt.Create(fake.NamespaceObject(configmanagement.ControllerNamespace)); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Create(fake.NamespaceObject(metrics.MonitoringNamespace)); err != nil {
		nt.T.Fatal(err)
	}

	if nt.GitProvider.Type() == e2e.Local {
		if err := nt.Create(gitNamespace()); err != nil {
			nt.T.Fatal(err)
		}
		// Pods don't always restart if the secrets don't exist, so we have to
		// create the Namespaces + Secrets before anything else.
		nt.gitPrivateKeyPath = generateSSHKeys(nt)

		nt.caCertPath = generateSSLKeys(nt)

		waitForGit := installGitServer(nt)
		if err := waitForGit(); err != nil {
			nt.describeNotRunningTestPods(testGitNamespace)
			t.Fatalf("waiting for git-server Deployment to become available: %v", err)
		}
	} else {
		nt.gitPrivateKeyPath = downloadSSHKey(nt)
	}

	t.Cleanup(func() {
		if t.Failed() {
			nt.printTestLogs()
		}
	})

	installConfigSync(nt, opts.Nomos)

	setupTestCase(nt, opts)
	return nt
}

func setupTestCase(nt *NT, opts *ntopts.New) {
	// allRepos specifies the slice all repos for port forwarding.
	var allRepos []types.NamespacedName
	for repo := range opts.RootRepos {
		allRepos = append(allRepos, RootSyncNN(repo))
	}
	for repo := range opts.NamespaceRepos {
		allRepos = append(allRepos, repo)
	}

	if nt.GitProvider.Type() == e2e.Local {
		nt.gitRepoPort = portForwardGitServer(nt, allRepos...)
	}

	for name := range opts.RootRepos {
		nt.RootRepos[name] = resetRepository(nt, RootRepo, RootSyncNN(name), opts.SourceFormat)
	}
	for nsr := range opts.NamespaceRepos {
		nt.NonRootRepos[nsr] = resetRepository(nt, NamespaceRepo, nsr, filesystem.SourceFormatUnstructured)
	}

	if opts.InitialCommit != nil {
		for path, obj := range opts.InitialCommit.Files {
			nt.RootRepos[configsync.RootSyncName].Add(path, obj)
		}
		nt.RootRepos[configsync.RootSyncName].CommitAndPush(opts.InitialCommit.Message)
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

	if err := WaitForConfigSyncReady(nt, opts.Nomos); err != nil {
		nt.T.Fatalf("waiting for ConfigSync Deployments to become available: %v", err)
	}

	nt.PortForwardOtelCollector()

	nt.Control = opts.Control
	switch opts.Control {
	case ntopts.DelegatedControl:
		setupDelegatedControl(nt, opts)
	case ntopts.CentralControl:
		setupCentralizedControl(nt, opts)
	default:
		nt.Control = ntopts.CentralControl
		// Most tests don't care about centralized/delegated control, but can
		// specify the behavior if that distinction is under test.
		setupCentralizedControl(nt, opts)
	}

	nt.WaitForRepoSyncs()
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
	tmpDir, err := ioutil.TempDir(filepath.Join(os.TempDir(), NomosE2E), name)
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
