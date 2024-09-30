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
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/clusterversion"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/portforwarder"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/util/log"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FieldManager is the field manager to use when creating, updating, and
// patching kubernetes objects with kubectl and client-go. This is used to
// uniquely identify the nomostest client, to enable field pruning and merging.
// This must be different from the field manager used by config sync, in order
// to allow both clients to manage different fields on the same objects.
const FieldManager = configsync.GroupName + "/nomostest"

// NT represents the test environment for a single Nomos end-to-end test case.
type NT struct {
	Context context.Context

	// T is the test environment for the test.
	// Used to exit tests early when setup fails, and for logging.
	T testing.NTB

	// Logger wraps testing.NTB and provides a simple logging interface for
	// test utilities to use without needing access to the whole testing.NTB.
	// Use if you want Debug and Log, but don't need to Error, Fatal, or Fail.
	Logger *testlogger.TestLogger

	// Shell is a test wrapper used to run shell commands.
	Shell *testshell.TestShell

	// ClusterName is the unique name of the test run.
	ClusterName string

	// TmpDir is the temporary directory the test will write to.
	// By default, automatically deleted when the test finishes.
	TmpDir string

	// Config specifies how to create a new connection to the cluster.
	Config *rest.Config

	// KubeClient is a test wrapper used to make Kubernetes calls.
	KubeClient *testkubeclient.KubeClient

	// Watcher is a test helper used to make Watch calls
	Watcher *testwatcher.Watcher

	// WatchClient is the underlying client used to talk to the Kubernetes
	// cluster, when performing watches.
	//
	// Most tests shouldn't need to talk directly to this, unless simulating
	// direct interactions with the API Server.
	WatchClient client.WithWatch

	// IsGKEAutopilot indicates if the test cluster is a GKE Autopilot cluster.
	IsGKEAutopilot bool

	// ClusterVersion is the parsed version of the target test cluster
	ClusterVersion *clusterversion.ClusterVersion

	// ClusterSupportsBursting indicates if the cluster supports bursting.
	ClusterSupportsBursting bool

	// ClusterHash is a 64 character long unique identifier formed in hex used to identify a GKE cluster.
	ClusterHash string

	// DefaultWaitTimeout is the default timeout for tests to wait for sync completion.
	DefaultWaitTimeout time.Duration

	// DefaultReconcileTimeout is the default timeout for the applier to wait
	// for object reconciliation.
	DefaultReconcileTimeout *time.Duration

	// SyncSources tracks the RootSyncs & RepoSyncs used by the current test.
	// Each one is mapped to a SyncSource or nil, if no source is configured.
	// The list of IDs is used for iterating over test RSyncs.
	// The ID group and kind must always be either RootSync of RepoSync.
	SyncSources syncsource.Set

	// MetricsExpectations tracks the objects expected to be declared in the
	// source and the operations expected to be performed on them by the set of
	// RootSyncs and RepoSyncs managed by this test.
	MetricsExpectations *testmetrics.SyncSetExpectations

	// ReconcilerPollingPeriod defines how often the reconciler should poll the
	// filesystem for updates to the source or rendered configs.
	ReconcilerPollingPeriod time.Duration

	// HydrationPollingPeriod defines how often the hydration-controller should
	// poll the filesystem for rendering the DRY configs.
	HydrationPollingPeriod time.Duration

	// gitPrivateKeyPath is the path to the private key used for communicating with the Git server.
	gitPrivateKeyPath string

	// gitCACertPath is the path to the CA cert used for communicating with the Git server.
	gitCACertPath string

	// registryCACertPath is the path to the CA cert used for communicating with the Registry server.
	registryCACertPath string

	// prometheusPortForwarder is the local port forwarding for the prometheus deployment.
	prometheusPortForwarder *portforwarder.PortForwarder

	// kubeconfigPath is the path to the kubeconfig file for the kind cluster
	kubeconfigPath string

	// Scheme is the Scheme for the test suite that maps from structs to GVKs.
	Scheme *runtime.Scheme

	// otelCollectorPort is the local port forwarding for the otel-collector.
	otelCollectorPortForwarder *portforwarder.PortForwarder

	// GitProvider is the provider that hosts the Git repositories.
	GitProvider gitproviders.GitProvider

	// OCIProvider is the provider that hosts the OCI repositories.
	OCIProvider registryproviders.OCIRegistryProvider

	// HelmProvider is the provider that hosts the helm packages.
	HelmProvider registryproviders.HelmRegistryProvider

	// HelmClient provides helm and crane clients to connect to
	// the environment-specific Helm repository.
	HelmClient *testshell.HelmClient

	// OCIClient provides a crane client to connect to the
	// environment-specific OCI repository.
	OCIClient *testshell.OCIClient

	// RemoteRepositories maintains a map between the repo local name and the remote repository.
	// It includes both root repo and namespace repos and can be shared among test cases.
	// It is used to reuse existing repositories instead of creating new ones.
	RemoteRepositories map[types.NamespacedName]*gitproviders.ReadWriteRepository

	// WebhookDisabled indicates whether the ValidatingWebhookConfiguration is deleted.
	WebhookDisabled *bool

	// Control indicates how the test case was setup.
	Control ntopts.RepoControl

	// repoSyncPermissions will grant a list of PolicyRules to NS reconcilers
	repoSyncPermissions []rbacv1.PolicyRule
}

// Must not error.
//
// This is a test helper that immediately fails the test if any of the arguments
// are a non-nil error. All nil and non-error argument are ignored.
// Usage Example: nt.Must(DoSomething())
//
// Note: Because nil objects lose their type when passed through an interface{},
// There's no way to validate that an error type was actually passed. Consider
// using require.NoError instead, when the only return value is an error.
func (nt *NT) Must(args ...interface{}) {
	nt.T.Helper()
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			if err != nil {
				nt.T.Fatal(err)
			}
		}
	}
}

// CSNamespaces is the namespaces of the Config Sync components.
var CSNamespaces = []string{
	configmanagement.ControllerNamespace,
	configmanagement.MonitoringNamespace,
	configmanagement.RGControllerNamespace,
}

var mySharedNTs *sharedNTs

type sharedNT struct {
	inUse    bool
	sharedNT *NT
	fakeNTB  *testing.FakeNTB
}

type sharedNTs struct {
	lock      sync.Mutex
	testMap   map[string]*sharedNT
	sharedNTs []*sharedNT
}

func (snt *sharedNTs) newNT(fake *testing.FakeNTB) *sharedNT {
	snt.lock.Lock()
	defer snt.lock.Unlock()
	newSNT := &sharedNT{
		inUse:   false,
		fakeNTB: fake,
	}
	snt.sharedNTs = append(snt.sharedNTs, newSNT)
	return newSNT
}

func (snt *sharedNTs) acquire(t testing.NTB) *NT {
	snt.lock.Lock()
	defer snt.lock.Unlock()
	if nt, ok := snt.testMap[t.Name()]; ok {
		return nt.sharedNT
	}
	for _, nt := range snt.sharedNTs {
		if !nt.inUse {
			nt.inUse = true
			snt.testMap[t.Name()] = nt
			t.Cleanup(func() {
				snt.release(t)
			})
			return nt.sharedNT
		}
	}
	t.Fatal("failed to get shared test environment")
	return nil
}

func (snt *sharedNTs) release(t testing.NTB) {
	snt.lock.Lock()
	defer snt.lock.Unlock()
	testName := t.Name()
	// if the test failed, mark the "fake" shared test environment as failed.
	// this way the Cleanup functions will honor the --debug flag.
	if t.Failed() {
		snt.testMap[testName].fakeNTB.Fail()
	}
	snt.testMap[testName].inUse = false
	delete(snt.testMap, testName)
}

func (snt *sharedNTs) destroy() {
	snt.lock.Lock()
	defer snt.lock.Unlock()
	var wg sync.WaitGroup
	for _, nt := range snt.sharedNTs {
		wg.Add(1)
		go func(nt *sharedNT) {
			defer wg.Done()
			// Run cleanup on shared test environment. This will run all registered
			// cleanups on the fake NTB, e.g.
			// - destroy clusters created at the beginning
			// - run `Clean` from FreshTestEnv
			nt.fakeNTB.RunCleanup()
		}(nt)
	}
	wg.Wait()
}

// InitSharedEnvironments initializes shared test environments.
// It should be run at the beginning of the test suite if the --share-test-env
// flag is provided. It will produce a number of test environment equal to the
// go test parallelism.
func InitSharedEnvironments() error {
	mySharedNTs = &sharedNTs{
		testMap: map[string]*sharedNT{},
	}
	prefix := *e2e.ClusterPrefix
	if prefix == "" {
		timeStamp := time.Now().Unix()
		prefix = fmt.Sprintf("cs-e2e-%v", timeStamp)
	}
	tg := taskgroup.New()
	clusters := *e2e.ClusterNames
	if len(clusters) == 0 { // generate names for ephemeral clusters
		for x := 0; x < e2e.NumParallel(); x++ {
			clusters = append(clusters, fmt.Sprintf("%s-%d", prefix, x))
		}
	}
	for _, name := range clusters {
		clusterName := name
		tg.Go(func() (err error) {
			defer func() {
				if recoverErr := recover(); recoverErr != nil {
					err = fmt.Errorf("recovered from panic in InitSharedEnvironments (%s): %v", clusterName, recoverErr)
				}
			}()
			return newSharedNT(clusterName)
		})
	}
	return tg.Wait()
}

// newSharedNT sets up the shared config sync testing environment globally.
func newSharedNT(name string) error {
	tmpDir := filepath.Join(os.TempDir(), NomosE2E, name)
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("failed to remove the shared test directory: %w", err)
	}
	fakeNTB := testing.NewFakeNTB(name)
	// Register a new SharedNT immediately with the fakeNTB. This way in the case
	// of an error (e.g. during cluster creation) the cleanup can still run.
	newSNT := mySharedNTs.newNT(fakeNTB)
	// Create the actual test environment that will be reused by tests.
	wrapper := testing.NewShared(fakeNTB)
	opts := newOptStruct(name, tmpDir)
	newSNT.sharedNT = FreshTestEnv(wrapper, opts)
	return nil
}

// CleanSharedNTs tears down the shared test environments.
func CleanSharedNTs() {
	fmt.Println("Cleaning up shared environments")
	mySharedNTs.destroy()
}

// SharedNT returns the shared test environment.
func SharedNT(t testing.NTB) *NT {
	if !*e2e.ShareTestEnv {
		t.Fatal("Error: the shared test environment is only available when --share-test-env is set")
	}
	return mySharedNTs.acquire(t)
}

// MustMergePatch is like MergePatch but will call t.Fatal if the patch fails.
func (nt *NT) MustMergePatch(obj client.Object, patch string, opts ...client.PatchOption) {
	nt.T.Helper()
	if err := nt.KubeClient.MergePatch(obj, patch, opts...); err != nil {
		nt.T.Fatal(err)
	}
}

// NumRepoSyncNamespaces returns the number of unique namespaces managed by RepoSyncs.
func (nt *NT) NumRepoSyncNamespaces() int {
	if len(nt.SyncSources) == 0 {
		return 0
	}
	rsNamespaces := map[string]struct{}{}
	for id := range nt.SyncSources {
		if id.Kind == configsync.RepoSyncKind {
			rsNamespaces[id.Namespace] = struct{}{}
		}
	}
	return len(rsNamespaces)
}

// SyncSourceGitReadWriteRepository returns the git ReadWriteRepository for the
// specified RSync, if it exists in NT.SyncSources.
func (nt *NT) SyncSourceGitReadWriteRepository(id core.ID) *gitproviders.ReadWriteRepository {
	source, found := nt.SyncSources[id]
	if !found {
		nt.T.Fatalf("Missing %s: %s", id.Kind, id.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("Expected *GitSyncSource for %s %s, but found %T",
			id.Kind, id.ObjectKey, source)
	}
	gitRepo, ok := gitSource.Repository.(*gitproviders.ReadWriteRepository)
	if !ok {
		nt.T.Fatalf("Expected GitSyncSource.Repository for %s %s to be a ReadWriteRepository, but found %T",
			id.Kind, id.ObjectKey, gitSource.Repository)
	}
	return gitRepo
}

// DefaultRootSha1Fn is the default function to retrieve the commit hash of the root repo.
func DefaultRootSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	// Get the repository this RootSync is syncing to, and ensure it is synced to HEAD.
	source, exists := nt.SyncSources[core.RootSyncID(nn.Name)]
	if !exists {
		return "", fmt.Errorf("nt.SyncSources doesn't include RootSync %q", nn)
	}
	return source.Commit()
}

// DefaultRepoSha1Fn is the default function to retrieve the commit hash of the namespace repo.
func DefaultRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	// Get the repository this RepoSync is syncing to, and ensure it is synced
	// to HEAD.
	source, exists := nt.SyncSources[core.RepoSyncID(nn.Name, nn.Namespace)]
	if !exists {
		return "", fmt.Errorf("nt.SyncSources doesn't include RepoSync %q", nn)
	}
	return source.Commit()
}

// RenewClient gets a new Client for talking to the cluster.
//
// Call this manually from within a test if you expect a controller to create a
// CRD dynamically, or if the test applies a CRD directly to the API Server.
func (nt *NT) RenewClient() {
	nt.T.Helper()
	nt.KubeClient = newTestClient(nt)
	nt.Watcher = testwatcher.NewWatcher(nt.Context, nt.Logger, nt.KubeClient, nt.Config, nt.Scheme, &nt.DefaultWaitTimeout)
}

// MustKubectl fails the test immediately if the kubectl command fails. On
// success, returns STDOUT.
func (nt *NT) MustKubectl(args ...string) []byte {
	nt.T.Helper()

	out, err := nt.Shell.Kubectl(args...)
	if err != nil {
		nt.T.Fatal(err)
	}
	return out
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
	out, err := nt.Shell.Kubectl(args...)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
	if err != nil {
		nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
		return
	}
	nt.T.Logf("%s\n%s", cmd, out)
}

// LogDeploymentPodResources logs the resources of the deployment's pod's containers
func (nt *NT) LogDeploymentPodResources(namespace, deployment string) {
	nt.T.Helper()
	pod, err := nt.KubeClient.GetDeploymentPod(
		deployment, namespace, 30*time.Second)
	if err != nil {
		nt.T.Error(err)
		return
	}
	nt.T.Logf("Deployment %s/%s pod container resources:", namespace, deployment)
	for _, container := range pod.Spec.Containers {
		nt.T.Logf("%s: %s", container.Name, log.AsJSON(container.Resources))
	}
}

// printTestLogs prints test logs and pods information for debugging.
func (nt *NT) printTestLogs() {
	nt.T.Log("[CLEANUP] Printing test logs for current container instances")
	nt.testLogs(false)
	nt.T.Log("[CLEANUP] Printing test logs for previous container instances")
	nt.testLogs(true)
	nt.T.Log("[CLEANUP] Printing test logs for running pods")
	for _, ns := range CSNamespaces {
		nt.listAndDescribePods(ns)
	}
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
	nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.ManagerName, reconcilermanager.ManagerName, previousPodLog)
	nt.PodLogs(configmanagement.ControllerNamespace, configuration.ShortName, configuration.ShortName, previousPodLog)
	nt.PodLogs("resource-group-system", "resource-group-controller-manager", "manager", false)
	for id := range nt.SyncSources {
		var reconcilerName string
		switch id.Kind {
		case configsync.RootSyncKind:
			reconcilerName = core.RootReconcilerName(id.Name)
		case configsync.RepoSyncKind:
			reconcilerName = core.NsReconcilerName(id.Namespace, id.Name)
		default:
			nt.T.Fatalf("Invalid SyncSources key: %#v", id)
		}
		nt.PodLogs(configmanagement.ControllerNamespace, reconcilerName, reconcilermanager.Reconciler, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.NsReconcilerName(ns), reconcilermanager.GitSync, previousPodLog)
		nt.LogDeploymentPodResources(configmanagement.ControllerNamespace, reconcilerName)
	}
}

// listAndDescribePods prints the output of `kubectl get pods`, which includes a 'RESTARTS' column
// indicating how many times each pod has restarted. If a pod has restarted, the following
// two commands can be used to get more information:
//  1. kubectl get pods -n config-management-system -o yaml
//  2. kubectl logs deployment/<deploy-name> <container-name> -n config-management-system -p
//
// It also prints the output of `kubectl describe pods` for PodSpec.
func (nt *NT) listAndDescribePods(ns string) {
	out, err := nt.Shell.Kubectl("get", "pods", "-n", ns)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	nt.T.Logf("kubectl get pods -n %s: \n%s", ns, string(out))
	if err != nil {
		nt.T.Log("error running `kubectl get pods`:", err)
	}
	out, err = nt.Shell.Kubectl("describe", "pods", "-n", ns)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	nt.T.Logf("kubectl describe pods -n %s: \n%s", ns, string(out))
	if err != nil {
		nt.T.Log("error running `kubectl describe pods`:", err)
	}
}

func (nt *NT) describeNotRunningTestPods(namespace string) {
	cmPods := &corev1.PodList{}
	if err := nt.KubeClient.List(cmPods, client.InNamespace(namespace)); err != nil {
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
			out, err := nt.Shell.Kubectl(args...)
			if err != nil {
				nt.T.Logf("error running `%s`: %s\n%s", cmd, err, out)
				continue
			}
			nt.T.Logf("%s\n%s", cmd, out)
			nt.printNotReadyContainerLogs(pod)
		}
	}
}

func (nt *NT) printNotReadyContainerLogs(pod corev1.Pod) {
	for _, cs := range pod.Status.ContainerStatuses {
		// Only print logs for containers that are not ready.
		// The reconciler container's logs have been printed in testLogs, so ignore it.
		if !cs.Ready && cs.Name != reconcilermanager.Reconciler {
			args := []string{"logs", pod.GetName(), "-n", pod.GetNamespace(), "-c", cs.Name}
			cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
			out, err := nt.Shell.Kubectl(args...)
			if err != nil {
				nt.T.Logf("error running `%s`: %s\n%s", cmd, err, out)
				continue
			}
			nt.T.Logf("%s\n%s", cmd, out)
		}
	}
}

// ApplyGatekeeperCRD applies the specified gatekeeper testdata file and waits
// for the specified CRD to be established, then resets the client RESTMapper.
func (nt *NT) ApplyGatekeeperCRD(file, crd string) error {
	nt.T.Logf("Applying gatekeeper CRD %s", crd)
	absPath := filepath.Join(baseDir, "e2e", "testdata", "gatekeeper", file)

	nt.T.Cleanup(func() {
		nt.MustDeleteGatekeeperTestData(file, fmt.Sprintf("CRD %s", crd))
		// Refresh the client to reset the RESTMapper to update discovered CRDs.
		nt.RenewClient()
	})

	// We have to set validate=false because the default Gatekeeper YAMLs include
	// fields introduced in 1.13 and can't be applied without it, and we aren't
	// going to define our own compliant version.
	nt.MustKubectl("apply", "-f", absPath, "--validate=false")
	err := WaitForCRDs(nt, []string{crd})
	if err != nil {
		// Refresh the client to reset the RESTMapper to update discovered CRDs.
		nt.RenewClient()
	}
	return err
}

// MustDeleteGatekeeperTestData deletes the specified gatekeeper testdata file,
// then resets the client RESTMapper.
func (nt *NT) MustDeleteGatekeeperTestData(file, name string) {
	absPath := filepath.Join(baseDir, "e2e", "testdata", "gatekeeper", file)
	out, err := nt.Shell.Kubectl("get", "-f", absPath)
	if err != nil {
		nt.T.Logf("Skipping cleanup of gatekeeper %s: %s", name, out)
		return
	}
	nt.T.Logf("Deleting gatekeeper %s", name)
	nt.MustKubectl("delete", "-f", absPath, "--ignore-not-found", "--wait")
}

func (nt *NT) newPortForwarder(ns, deployment, port string, opts ...portforwarder.PortForwardOpt) *portforwarder.PortForwarder {
	return portforwarder.NewPortForwarder(
		nt.KubeClient,
		nt.Watcher,
		nt.Logger,
		nt.DefaultWaitTimeout,
		nt.kubeconfigPath,
		ns, deployment, port,
		opts...,
	)
}

func (nt *NT) startPortForwarder(ns, deployment string, pf *portforwarder.PortForwarder) {
	ctx, cancel := context.WithCancel(nt.Context)
	eventCh := pf.Start(ctx)
	doneCh := make(chan struct{})
	readyCh := make(chan struct{})
	nt.T.Cleanup(func() {
		nt.T.Logf("killing port-forward %s/%s", ns, deployment)
		cancel()
		<-doneCh
		nt.T.Logf("finished waiting for port-forward %s/%s to exit", ns, deployment)
	})
	go func() {
		defer close(doneCh)
		// first event should be Init. signal if it's ready
		event := <-eventCh
		if event.Type == portforwarder.InitEvent {
			close(readyCh)
		}
		for event := range eventCh {
			if event.Type == portforwarder.ErrorEvent {
				nt.T.Error(event.Message)
			}
		}
	}()
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, nt.DefaultWaitTimeout)
	defer timeoutCancel()
	now := time.Now()
	select {
	case <-timeoutCtx.Done():
		since := time.Since(now)
		nt.T.Fatalf("Waited %v for PortForwarder %s/%s to become ready: %v", since, ns, deployment, timeoutCtx.Err())
	case <-readyCh:
		nt.T.Logf("PortForwarder for %s/%s is ready", ns, deployment)
	}
}

// portForwardOtelCollector forwards the otel-collector deployment to a port.
func (nt *NT) portForwardOtelCollector() {
	if nt.otelCollectorPortForwarder != nil {
		nt.T.Fatal("otel collector port forward already initialized")
	}
	nt.otelCollectorPortForwarder = nt.newPortForwarder(
		configmanagement.MonitoringNamespace,
		ocmetrics.OtelCollectorName,
		fmt.Sprintf(":%d", testmetrics.OtelCollectorMetricsPort),
	)
	nt.startPortForwarder(
		configmanagement.MonitoringNamespace,
		ocmetrics.OtelCollectorName,
		nt.otelCollectorPortForwarder,
	)
}

// portForwardGitServer forwards the git-server deployment to a port.
func (nt *NT) portForwardGitServer() {
	nt.T.Helper()
	provider := nt.GitProvider.(*gitproviders.LocalProvider)
	prevPodName := ""
	resetGitRepos := func(newPort int, podName string) {
		// pod unchanged, don't reset
		if prevPodName == podName {
			return
		}
		// allGitRepos specifies the slice all repos for port forwarding.
		var allGitRepos []types.NamespacedName
		// allGitRepoMap is a map of repoNN->Repository for port forwarding.
		allGitRepoMap := make(map[types.NamespacedName]*gitproviders.ReadWriteRepository)
		for id, source := range nt.SyncSources {
			if gitSource, ok := source.(*syncsource.GitSyncSource); ok {
				if gitRepo, ok := gitSource.Repository.(*gitproviders.ReadWriteRepository); ok {
					allGitRepos = append(allGitRepos, id.ObjectKey)
					allGitRepoMap[id.ObjectKey] = gitRepo
				}
				// Ignore ReadOnlyRepository - doesn't need a proxy, because it
				// doesn't run on our test cluster.
			}
		}
		// re-init all repos
		InitGitRepos(nt, allGitRepos...)
		for repoNN, repo := range allGitRepoMap {
			// construct remoteURL with provided port. Calling LocalPort would lead to deadlock
			remoteURL, err := provider.RemoteURLWithPort(newPort, repoNN.String())
			if err != nil {
				nt.T.Fatal(err)
			}
			if err := repo.PushAllBranches(remoteURL); err != nil {
				nt.T.Fatal(err)
			}
		}
		prevPodName = podName
	}
	nt.T.Cleanup(func() {
		// clear PortForwarder after each test. at this point it has been stopped
		// a new PortForwarder is created for each test.
		provider.PortForwarder = nil
	})
	provider.PortForwarder = nt.newPortForwarder(
		testGitNamespace,
		testGitServer,
		":22",
		portforwarder.WithOnReadyCallback(resetGitRepos),
	)
	nt.startPortForwarder(
		testGitNamespace,
		testGitServer,
		provider.PortForwarder,
	)
}

// portForwardRegistryServer forwards the registry-server deployment to a port.
func (nt *NT) portForwardRegistryServer(helmTest, ociTest bool) {
	// The local registry uses ephemeral storage. So login and re-push all
	// the oci images and helm charts after the registry recovers from a crash.
	// Before the test, nt.ociImages & nt.helmPackages will be empty.
	var onReadyCallbacks []func(int, string)
	if provider, ok := nt.OCIProvider.(registryproviders.ProxiedRegistryProvider); ok && ociTest {
		setupFn := func(newPort int, _ string) {
			if err := provider.Restore(provider.ProxyAddress(newPort)); err != nil {
				nt.T.Fatalf("restoring proxy provider: %v", err)
			}
		}
		onReadyCallbacks = append(onReadyCallbacks, setupFn)
	}
	if provider, ok := nt.HelmProvider.(registryproviders.ProxiedRegistryProvider); ok && helmTest {
		setupFn := func(newPort int, _ string) {
			if err := provider.Restore(provider.ProxyAddress(newPort)); err != nil {
				nt.T.Fatalf("restoring proxy provider: %v", err)
			}
		}
		onReadyCallbacks = append(onReadyCallbacks, setupFn)
	}
	portForwarder := nt.newPortForwarder(
		TestRegistryNamespace,
		TestRegistryServer,
		fmt.Sprintf(":%d", RegistryHTTPPort),
		portforwarder.WithOnReadyCallback(func(newPort int, podName string) {
			for _, fn := range onReadyCallbacks {
				fn(newPort, podName)
			}
		}),
	)
	// Register Cleanup to clear the PortForwarder BEFORE startPortForwarder, so
	// it runs AFTER the Cleanup in startPortForwarder which stops the PortForwarder.
	if provider, ok := nt.OCIProvider.(*registryproviders.LocalOCIProvider); ok && ociTest {
		nt.T.Cleanup(func() {
			// clear PortForwarder after each test. at this point it has been stopped
			// a new PortForwarder is created for each test.
			provider.PortForwarder = nil
		})
		provider.PortForwarder = portForwarder
	}
	if provider, ok := nt.HelmProvider.(*registryproviders.LocalHelmProvider); ok && helmTest {
		nt.T.Cleanup(func() {
			// clear PortForwarder after each test. at this point it has been stopped
			// a new PortForwarder is created for each test.
			provider.PortForwarder = nil
		})
		provider.PortForwarder = portForwarder
	}
	// Start the PortForwarder and register Cleanup to stop it when the test ends.
	nt.startPortForwarder(
		TestRegistryNamespace,
		TestRegistryServer,
		portForwarder,
	)
}

// portForwardPrometheus forwards the prometheus deployment to a port.
func (nt *NT) portForwardPrometheus() {
	nt.T.Helper()
	if nt.prometheusPortForwarder != nil {
		nt.T.Fatal("prometheus port forward already initialized")
	}
	nt.prometheusPortForwarder = nt.newPortForwarder(
		prometheusNamespace,
		prometheusServerDeploymentName,
		fmt.Sprintf(":%d", prometheusServerPort),
	)
	nt.startPortForwarder(
		prometheusNamespace,
		prometheusServerDeploymentName,
		nt.prometheusPortForwarder,
	)

	if err := nt.emptyPrometheusCache(); err != nil {
		nt.T.Fatalf("failed to empty prometheus cache: %v", err)
	}
}

// emptyPrometheusCache empty the cache in Prometheus.
func (nt *NT) emptyPrometheusCache() error {
	nt.T.Helper()
	ctx, cancel := context.WithCancel(nt.Context)
	defer cancel()

	duration, err := retry.Retry(nt.DefaultWaitTimeout, func() error {
		port, err := nt.prometheusPortForwarder.LocalPort()
		if err != nil {
			return err
		}
		c, err := prometheusapi.NewClient(prometheusapi.Config{
			Address: fmt.Sprintf("http://localhost:%d", port),
		})
		if err != nil {
			return err
		}
		v1api := prometheusv1.NewAPI(c)
		// DeleteSeries only marks the time series for deletion
		if err = v1api.DeleteSeries(ctx, []string{`{__name__=~".+"}`}, time.Time{}, time.Now()); err != nil {
			return fmt.Errorf("failed to mark Prometheus data for deletion: %v", err)
		}
		// CleanTombstones remove them from the disk to clear the space instantly
		if err = v1api.CleanTombstones(ctx); err != nil {
			return fmt.Errorf("failed to remove tombstones from the disk: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("timed out waiting for deleting Prometheus cache: %w", err)
	}
	nt.T.Logf("waited %v for Prometheus cache to be cleared", duration)
	return nil
}

func (nt *NT) detectGKEAutopilot(skipAutopilot bool) {
	if !nt.IsGKEAutopilot {
		isGKEAutopilot, err := util.IsGKEAutopilotCluster(nt.KubeClient.Client)
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.IsGKEAutopilot = isGKEAutopilot
	}
	if nt.IsGKEAutopilot && skipAutopilot {
		nt.T.Skip("Test skipped when running on Autopilot clusters")
	}
}

// Prerequisites for autopilot bursting:
// - You originally created the cluster with GKE version 1.26 or later
// - The cluster is running GKE version 1.30.2-gke.1394000 or later
// See: https://cloud.google.com/kubernetes-engine/docs/how-to/pod-bursting-gke#availability-in-gke
func (nt *NT) autopilotClusterSupportsBursting() (bool, error) {
	args := []string{"container", "clusters", "describe", nt.ClusterName,
		"--project", *e2e.GCPProject, "--location", *e2e.GCPRegion,
		"--format", `value(initialClusterVersion)`}
	out, err := nt.Shell.Gcloud(args...)
	if err != nil {
		return false, err
	}
	initialClusterVersion, err := clusterversion.ParseClusterVersion(strings.TrimSpace(string(out)))
	if err != nil {
		return false, err
	}
	minInitialVersion := clusterversion.ClusterVersion{Major: 1, Minor: 26}
	if !initialClusterVersion.IsAtLeast(minInitialVersion) {
		return false, nil
	}

	minCurrentVersion := clusterversion.ClusterVersion{Major: 1, Minor: 30, Patch: 2, Suffix: "-gke.1394000"}
	args = []string{"get", "nodes", "-o", `jsonpath={.items[*].status.nodeInfo.kubeletVersion}`}
	out, err = nt.Shell.Kubectl(args...)
	if err != nil {
		return false, err
	}
	versions := strings.Split(strings.TrimSpace(string(out)), " ")
	for _, nodeVersion := range versions {
		v, err := clusterversion.ParseClusterVersion(nodeVersion)
		if err != nil {
			return false, err
		}
		if !v.IsAtLeast(minCurrentVersion) {
			return false, nil
		}
	}
	return true, nil
}

func (nt *NT) detectClusterSupportsBursting() {
	if *e2e.GKEAutopilot {
		var err error
		nt.ClusterSupportsBursting, err = nt.autopilotClusterSupportsBursting()
		if err != nil {
			nt.T.Fatal(err)
		}
	} else {
		nt.ClusterSupportsBursting = true
	}
}

func (nt *NT) detectClusterVersion() {
	if nt.ClusterVersion == nil {
		dc, err := discovery.NewDiscoveryClientForConfig(nt.Config)
		if err != nil {
			nt.T.Fatalf("failed to create discovery client: %w", err)
		}
		serverVersion, err := dc.ServerVersion()
		if err != nil {
			nt.T.Fatalf("failed to get server version: %w", err)
		}
		clusterVersion, err := clusterversion.ParseClusterVersion(serverVersion.GitVersion)
		if err != nil {
			nt.T.Fatalf("failed to parse cluster version %q: %w", serverVersion.GitVersion, err)
		}
		nt.ClusterVersion = &clusterVersion
	}
}

// RepoSyncClusterRole returns the NS reconciler ClusterRole
func (nt *NT) RepoSyncClusterRole() *rbacv1.ClusterRole {
	cr := k8sobjects.ClusterRoleObject(core.Name(clusterRoleName))
	cr.Rules = append(cr.Rules, nt.repoSyncPermissions...)
	return cr
}

// RemoteRootRepoSha1Fn returns the latest commit from a RootSync Git spec.
func RemoteRootRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	rs := &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(nn.Name, nn.Namespace, rs); err != nil {
		return "", err
	}
	commit, err := GitCommitFromSpec(nt, rs.Spec.Git)
	if err != nil {
		return "", fmt.Errorf("failed to lookup git commit for RootSync: %w", err)
	}
	return commit, nil
}

// RemoteNsRepoSha1Fn returns the latest commit from a RepoSync Git spec.
func RemoteNsRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	rs := &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(nn.Name, nn.Namespace, rs); err != nil {
		return "", err
	}
	commit, err := GitCommitFromSpec(nt, rs.Spec.Git)
	if err != nil {
		return "", fmt.Errorf("failed to lookup git commit for RepoSync: %w", err)
	}
	return commit, nil
}

// GitCommitFromSpec returns the latest commit from a Git spec.
// Uses git ls-remote to avoid needing to clone the repo.
// Warning: may not work if authentication is required.
func GitCommitFromSpec(nt *NT, gitSpec *v1beta1.Git) (string, error) {
	if gitSpec == nil {
		return "", errors.New("spec.git is nil")
	}
	if gitSpec.Repo == "" {
		return "", errors.New("spec.git.repo is empty")
	}
	var pattern string
	if gitSpec.Branch != "" {
		// HEAD of specified branch
		pattern = gitSpec.Branch // HEAD of specified branch
	} else {
		// HEAD of default branch
		pattern = "HEAD"
	}
	// revision specified
	if gitSpec.Revision != "" {
		pattern = gitSpec.Revision
	}
	var args []string
	if strings.Contains(gitSpec.Repo, testing.CSRHost) {
		cloneDir, err := cloneCloudSourceRepo(nt, gitSpec.Repo)
		if err != nil {
			return "", err
		}
		// use cloneDir as working directory so that git can authenticate
		args = append(args, "-C", cloneDir)
	}
	// List remote references (branches and tags).
	// Expected Output: GIT_COMMIT\tREF_NAME
	args = append(args, "ls-remote", gitSpec.Repo, pattern)
	out, err := nt.Shell.ExecWithDebug("git", args...)
	if err != nil {
		return "", err
	}
	// Trim subsequent lines, if more than one
	lines := strings.SplitN(string(out), "\n", 2)
	if len(lines) == 0 {
		return "", fmt.Errorf("empty output from command: git %s",
			strings.Join(args, " "))
	}
	// Trim subsequent columns, if more than one
	columns := strings.SplitN(lines[0], "\t", 2)
	if len(lines) == 0 {
		return "", fmt.Errorf("invalid output from command: git %s: %s",
			strings.Join(args, " "), lines[0])
	}
	return columns[0], nil
}

// cloneCloudSourceRepo clones the provided Cloud Source Repository to a local
// temp directory and returns the directory path.
// This special logic is needed to handle authentication to the repo in CI.
func cloneCloudSourceRepo(nt *NT, repo string) (string, error) {
	repoName := path.Base(repo)
	cloneDir := path.Join(nt.TmpDir, "csr-repos", repoName)
	// return if directory was already created
	if _, err := os.Stat(cloneDir); err == nil {
		return cloneDir, nil
	}
	args := []string{
		"source", "repos", "clone", "--project", *e2e.GCPProject, repoName, cloneDir,
	}
	cmd := nt.Shell.Command("gcloud", args...)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return cloneDir, nil
}

// ListReconcilerRoleBindings is a convenience method for listing all RoleBindings
// associated with a reconciler.
func (nt *NT) ListReconcilerRoleBindings(syncKind string, rsRef types.NamespacedName) ([]rbacv1.RoleBinding, error) {
	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(controllers.ManagedObjectLabelMap(syncKind, rsRef)),
	}
	rbList := rbacv1.RoleBindingList{}
	if err := nt.KubeClient.List(&rbList, opts); err != nil {
		return nil, fmt.Errorf("listing RoleBindings: %w", err)
	}
	return rbList.Items, nil
}

// ListReconcilerClusterRoleBindings is a convenience method for listing all
// ClusterRoleBindings associated with a reconciler.
func (nt *NT) ListReconcilerClusterRoleBindings(syncKind string, rsRef types.NamespacedName) ([]rbacv1.ClusterRoleBinding, error) {
	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(controllers.ManagedObjectLabelMap(syncKind, rsRef)),
	}
	crbList := rbacv1.ClusterRoleBindingList{}
	if err := nt.KubeClient.List(&crbList, opts); err != nil {
		return nil, fmt.Errorf("listing ClusterRoleBindings: %w", err)
	}
	return crbList.Items, nil
}
