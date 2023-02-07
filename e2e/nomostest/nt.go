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
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/e2e"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
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

	// WatchClient is the underlying client used to talk to the Kubernetes
	// cluster, when performing watches.
	//
	// Most tests shouldn't need to talk directly to this, unless simulating
	// direct interactions with the API Server.
	WatchClient client.WithWatch

	// IsGKEAutopilot indicates if the test cluster is a GKE Autopilot cluster.
	IsGKEAutopilot bool

	// DefaultWaitTimeout is the default timeout for tests to wait for sync completion.
	DefaultWaitTimeout time.Duration

	// DefaultReconcileTimeout is the default timeout for the applier to wait
	// for object reconcilition.
	DefaultReconcileTimeout time.Duration

	// DefaultMetricsTimeout is the default timeout for tests to wait for
	// metrics to match expectations. This needs to be long enough to account
	// for batched metrics in the agent and collector, but short enough that
	// the metrics don't expire in the collector.
	DefaultMetricsTimeout time.Duration

	// RootRepos is the root repositories the cluster is syncing to.
	// The key is the RootSync name and the value points to the corresponding Repository object.
	// Each test case was set up with a default RootSync (`root-sync`) installed.
	// After the test, all other RootSync or RepoSync objects are deleted, but the default one persists.
	RootRepos map[string]*Repository

	// NonRootRepos is the Namespace repositories the cluster is syncing to.
	// Only used in multi-repo tests.
	// The key is the namespace and name of the RepoSync object, the value points to the corresponding Repository object.
	NonRootRepos map[types.NamespacedName]*Repository

	// ReconcilerPollingPeriod defines how often the reconciler should poll the
	// filesystem for updates to the source or rendered configs.
	ReconcilerPollingPeriod time.Duration

	// HydrationPollingPeriod defines how often the hydration-controller should
	// poll the filesystem for rendering the DRY configs.
	HydrationPollingPeriod time.Duration

	// gitPrivateKeyPath is the path to the private key used for communicating with the Git server.
	gitPrivateKeyPath string

	// caCertPath is the path to the CA cert used for communicating with the Git server.
	caCertPath string

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

	// WebhookDisabled indicates whether the ValidatingWebhookConfiguration is deleted.
	WebhookDisabled *bool

	// Control indicates how the test case was setup.
	Control ntopts.RepoControl

	// repoSyncPermissions will grant a list of PolicyRules to NS reconcilers
	repoSyncPermissions []rbacv1.PolicyRule
}

// CSNamespaces is the namespaces of the Config Sync components.
var CSNamespaces = []string{
	configmanagement.ControllerNamespace,
	ocmetrics.MonitoringNamespace,
	configmanagement.RGControllerNamespace,
}

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

func (snt *sharedNTs) newNT(nt *NT, fake *testing.FakeNTB) {
	snt.lock.Lock()
	defer snt.lock.Unlock()
	snt.sharedNTs = append(snt.sharedNTs, &sharedNT{
		inUse:    false,
		sharedNT: nt,
		fakeNTB:  fake,
	})
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
			if *e2e.TestCluster == e2e.Kind {
				// Run Cleanup to destroy kind clusters
				nt.fakeNTB.RunCleanup()
			} else {
				Clean(nt.sharedNT, false)
			}
		}(nt)
	}
	wg.Wait()
}

// InitSharedEnvironments initializes shared test environments.
// It should be run at the beginning of the test suite if the --share-test-env
// flag is provided. It will produce a number of test environment equal to the
// go test parallelism.
func InitSharedEnvironments() {
	mySharedNTs = &sharedNTs{
		testMap: map[string]*sharedNT{},
	}
	timeStamp := time.Now().Unix()
	var wg sync.WaitGroup
	for x := 0; x < e2e.NumParallel(); x++ {
		name := fmt.Sprintf("cs-e2e-%v-%v", timeStamp, x)
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			newSharedNT(name)
		}(name)
	}
	wg.Wait()
}

// newSharedNT sets up the shared config sync testing environment globally.
func newSharedNT(name string) *NT {
	tmpDir := filepath.Join(os.TempDir(), NomosE2E, name)
	if err := os.RemoveAll(tmpDir); err != nil {
		fmt.Printf("failed to remove the shared test directory: %v\n", err)
		os.Exit(1)
	}
	fakeNTB := &testing.FakeNTB{}
	wrapper := testing.NewShared(fakeNTB)
	opts := newOptStruct(name, tmpDir, wrapper)
	nt := FreshTestEnv(wrapper, opts)
	mySharedNTs.newNT(nt, fakeNTB)
	return nt
}

// CleanSharedNTs tears down the shared test environments.
func CleanSharedNTs() {
	mySharedNTs.destroy()
}

// SharedNT returns the shared test environment.
func SharedNT(t testing.NTB) *NT {
	if !*e2e.ShareTestEnv {
		fmt.Println("Error: the shared test environment is only available when --share-test-env is set")
		os.Exit(1)
	}
	return mySharedNTs.acquire(t)
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
	opts = append(opts, client.FieldOwner(FieldManager))
	return nt.Client.Create(nt.Context, obj, opts...)
}

// Update is identical to Update defined for client.Client, but without requiring Context.
// All fields will be adopted by the nomostest field manager.
func (nt *NT) Update(obj client.Object, opts ...client.UpdateOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("updating %s", fmtObj(obj))
	opts = append(opts, client.FieldOwner(FieldManager))
	return nt.Client.Update(nt.Context, obj, opts...)
}

// Apply wraps Patch to perform a server-side apply.
// All non-nil fields will be adopted by the nomostest field manager.
func (nt *NT) Apply(obj client.Object, opts ...client.PatchOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("applying %s", fmtObj(obj))
	AddTestLabel(obj)
	opts = append(opts, client.FieldOwner(FieldManager), client.ForceOwnership)
	return nt.Client.Patch(nt.Context, obj, client.Apply, opts...)
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
// All specified fields will be adopted by the nomostest field manager.
func (nt *NT) MergePatch(obj client.Object, patch string, opts ...client.PatchOption) error {
	FailIfUnknown(nt.T, nt.scheme, obj)
	nt.DebugLogf("Applying patch %s", patch)
	AddTestLabel(obj)
	opts = append(opts, client.FieldOwner(FieldManager))
	return nt.Client.Patch(nt.Context, obj, client.RawPatch(types.MergePatchType, []byte(patch)), opts...)
}

// MustMergePatch is like MergePatch but will call t.Fatal if the patch fails.
func (nt *NT) MustMergePatch(obj client.Object, patch string, opts ...client.PatchOption) {
	nt.T.Helper()
	if err := nt.MergePatch(obj, patch, opts...); err != nil {
		nt.T.Fatal(err)
	}
}

// DefaultRootSyncObjectCount returns the number of objects expected to be in
// the default RootSync repo, based on how many RepoSyncs it's managing.
func (nt *NT) DefaultRootSyncObjectCount() int {
	numObjects := 2 // 2 for the 'safety' Namespace & 'safety' ClusteRole

	// Count the number of unique RepoSyncs
	numRepoSyncs := len(nt.NonRootRepos)

	// Account for objects added to the default RootRepo by setupCentralizedControl
	if nt.Control == ntopts.CentralControl && numRepoSyncs > 0 {
		numObjects++                             // 1 for the ClusterRole, shared by all RepoSyncs
		numObjects += nt.NumRepoSyncNamespaces() // 1 for each unique RepoSync Namespace
		numObjects += numRepoSyncs               // 1 for each RepoSync
		numObjects += numRepoSyncs               // 1 for each RepoSync RoleBinding
		if isPSPCluster() {
			numObjects += numRepoSyncs // 1 for each RepoSync ClusterRoleBinding
		}
	}
	return numObjects
}

// NumRepoSyncNamespaces returns the number of unique namespaces managed by RepoSyncs.
func (nt *NT) NumRepoSyncNamespaces() int {
	if len(nt.NonRootRepos) == 0 {
		return 0
	}
	rsNamespaces := map[string]struct{}{}
	for nn := range nt.NonRootRepos {
		rsNamespaces[nn.Namespace] = struct{}{}
	}
	return len(rsNamespaces)
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
func DefaultRepoSha1Fn(nt *NT, nn types.NamespacedName) (string, error) {
	// Get the repository this RepoSync is syncing to, and ensure it is synced
	// to HEAD.
	repo, exists := nt.NonRootRepos[nn]
	if !exists {
		return "", fmt.Errorf("checked if nonexistent repo is synced")
	}
	return repo.Hash(), nil
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
	// Ensure field manager is specified
	if stringArrayContains(args, "apply") && !stringArrayContains(args, "--field-manager") {
		args = append(args, "--field-manager", FieldManager)
	}
	nt.DebugLogf("kubectl %s", strings.Join(args, " "))
	return exec.Command("kubectl", args...).CombinedOutput()
}

// KubectlContext is similar to nt.Kubectl but allows using a context to cancel
// (kill signal) the kubectl command.
func (nt *NT) KubectlContext(ctx context.Context, args ...string) ([]byte, error) {
	nt.T.Helper()

	prefix := []string{"--kubeconfig", nt.kubeconfigPath}
	args = append(prefix, args...)
	// Ensure field manager is specified
	if stringArrayContains(args, "apply") && !stringArrayContains(args, "--field-manager") {
		args = append(args, "--field-manager", FieldManager)
	}
	nt.DebugLogf("kubectl %s", strings.Join(args, " "))
	return exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
}

// Command is a convenience method for invoking a subprocess with the
// KUBECONFIG environment variable set. Setting the environment variable
// directly in the test process is not thread safe.
func (nt *NT) Command(name string, args ...string) *exec.Cmd {
	nt.T.Helper()

	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", nt.KubeconfigPath()))
	return cmd
}

// MustKubectl fails the test immediately if the kubectl command fails. On
// success, returns STDOUT.
func (nt *NT) MustKubectl(args ...string) []byte {
	nt.T.Helper()

	out, err := nt.Kubectl(args...)
	if err != nil {
		if !*e2e.Debug {
			nt.T.Logf("kubectl %s", strings.Join(args, " "))
		}
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

// printTestLogs prints test logs and pods information for debugging.
func (nt *NT) printTestLogs() {
	nt.T.Log("[CLEANUP] Printing test logs for current container instances")
	nt.testLogs(false)
	nt.T.Log("[CLEANUP] Printing test logs for previous container instances")
	nt.testLogs(true)
	nt.T.Log("[CLEANUP] Printing test logs for running pods")
	for _, ns := range CSNamespaces {
		nt.testPods(ns)
	}
	nt.T.Log("[CLEANUP] Describing not-ready pods")
	for _, ns := range CSNamespaces {
		nt.describeNotRunningTestPods(ns)
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
	for name := range nt.RootRepos {
		nt.PodLogs(configmanagement.ControllerNamespace, core.RootReconcilerName(name),
			reconcilermanager.Reconciler, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.NsReconcilerName(ns), reconcilermanager.GitSync, previousPodLog)
	}
	for nn := range nt.NonRootRepos {
		nt.PodLogs(configmanagement.ControllerNamespace, core.NsReconcilerName(nn.Namespace, nn.Name),
			reconcilermanager.Reconciler, previousPodLog)
		//nt.PodLogs(configmanagement.ControllerNamespace, reconcilermanager.NsReconcilerName(ns), reconcilermanager.GitSync, previousPodLog)
	}
}

// testPods prints the output of `kubectl get pods`, which includes a 'RESTARTS' column
// indicating how many times each pod has restarted. If a pod has restarted, the following
// two commands can be used to get more information:
//  1. kubectl get pods -n config-management-system -o yaml
//  2. kubectl logs deployment/<deploy-name> <container-name> -n config-management-system -p
func (nt *NT) testPods(ns string) {
	out, err := nt.Kubectl("get", "pods", "-n", ns)
	// Print a standardized header before each printed log to make ctrl+F-ing the
	// log you want easier.
	nt.T.Logf("kubectl get pods -n %s: \n%s", ns, string(out))
	if err != nil {
		nt.T.Log("error running `kubectl get pods`:", err)
	}
}

func (nt *NT) describeNotRunningTestPods(namespace string) {
	cmPods := &corev1.PodList{}
	if err := nt.List(cmPods, client.InNamespace(namespace)); err != nil {
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
			out, err := nt.Kubectl(args...)
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
	out, err := nt.Kubectl("get", "-f", absPath)
	if err != nil {
		nt.T.Logf("Skipping cleanup of gatekeeper %s: %s", name, out)
		return
	}
	nt.T.Logf("Deleting gatekeeper %s", name)
	nt.MustKubectl("delete", "-f", absPath, "--ignore-not-found", "--wait")
}

// PortForwardOtelCollector forwards the otel-collector pod.
func (nt *NT) PortForwardOtelCollector() {
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

// SupportV1Beta1CRDAndRBAC checks if v1beta1 CRD and RBAC resources are supported
// in the current testing cluster.
// v1beta1 APIs for CRD and RBAC resources are deprecated in K8s 1.22.
func (nt *NT) SupportV1Beta1CRDAndRBAC() (bool, error) {
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

// GetDeploymentPod is a convenience method to look up the pod for a deployment.
// It requires that exactly one pod exist and that the deoployment uses label
// selectors to uniquely identify its pods.
// This is promarily useful for finding the current pod for a reconciler or
// other single-replica controller deployments.
// Any error will cause a fatal test failure.
func (nt *NT) GetDeploymentPod(deploymentName, namespace string) *corev1.Pod {
	deployment := &appsv1.Deployment{}
	if err := nt.Get(deploymentName, namespace, deployment); err != nil {
		nt.T.Fatal(err)
	}
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		nt.T.Fatal(err)
	}
	if selector.Empty() {
		nt.T.Fatal("deployment has no label selectors: %s/%s", namespace, deploymentName)
	}

	pods := &corev1.PodList{}
	Wait(nt.T, fmt.Sprintf("listing pods for deployment %s/%s", namespace, deploymentName), 1*time.Minute, func() error {
		err = nt.List(pods, client.InNamespace(namespace), client.MatchingLabelsSelector{
			Selector: selector,
		})
		if err != nil {
			return err
		}
		if len(pods.Items) != 1 {
			return errors.Errorf("deployment has %d pods but expected %d: %s/%s", len(pods.Items), 1, namespace, deploymentName)
		}
		return nil
	})
	pod := pods.Items[0]
	nt.DebugLogf("Found deployment pod: %s/%s", pod.Namespace, pod.Name)
	return &pod
}

// RepoSyncClusterRole returns the NS reconciler ClusterRole
func (nt *NT) RepoSyncClusterRole() *rbacv1.ClusterRole {
	cr := fake.ClusterRoleObject(core.Name(clusterRoleName))
	cr.Rules = append(cr.Rules, nt.repoSyncPermissions...)
	if isPSPCluster() {
		cr.Rules = append(cr.Rules, rbacv1.PolicyRule{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{"acm-psp"},
			Verbs:         []string{"use"},
		})
	}
	return cr
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

func stringArrayContains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
