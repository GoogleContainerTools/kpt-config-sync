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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	webhookconfig "kpt.dev/configsync/pkg/webhook/configuration"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object/dependson"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// configSyncManifests is the folder of the test manifests
	configSyncManifests = "manifests"

	// e2e/raw-nomos/manifests/multi-repo-configmaps.yaml
	multiConfigMapsName = "multi-repo-configmaps.yaml"

	// clusterRoleName imitates a user-created (Cluster)Role for NS reconcilers.
	clusterRoleName = "cs-e2e"

	// shortSyncPollingPeriod is the default override for the git-sync/oci-sync
	// polling period used by tests. Using a shorter period speeds up the e2e tests.
	shortSyncPollingPeriod = 5 * time.Second
)

var (
	// baseDir is the path to the Nomos repository root from test case files.
	//
	// All paths must be relative to the test file that is running. There is probably
	// a more elegant way to do this.
	baseDir            = filepath.FromSlash("../..")
	outputManifestsDir = filepath.Join(baseDir, ".output", "staging", "oss")
	configSyncManifest = filepath.Join(outputManifestsDir, "config-sync-manifest.yaml")
	multiConfigMaps    = filepath.Join(baseDir, "e2e", "raw-nomos", configSyncManifests, multiConfigMapsName)
)

var (
	// reconcilerPollingPeriod specifies reconciler polling period as time.Duration
	reconcilerPollingPeriod time.Duration
	// hydrationPollingPeriod specifies hydration-controller polling period as time.Duration
	hydrationPollingPeriod time.Duration

	// DefaultRootReconcilerName is the root-reconciler name of the default RootSync object: "root-sync".
	DefaultRootReconcilerName = core.RootReconcilerName(configsync.RootSyncName)
	// DefaultRootRepoNamespacedName is the NamespacedName of the default RootSync object.
	DefaultRootRepoNamespacedName = RootSyncNN(configsync.RootSyncName)
	// DefaultRootSyncID is the ID of the default RootSync object.
	DefaultRootSyncID = core.RootSyncID(configsync.RootSyncName)
)

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

// IsReconcilerTemplateConfigMap returns true if passed obj is the
// reconciler-manager ConfigMap reconciler-manager-cm in config-management namespace.
func IsReconcilerTemplateConfigMap(obj client.Object) bool {
	return obj.GetName() == controllers.ReconcilerTemplateConfigMapName &&
		obj.GetNamespace() == configmanagement.ControllerNamespace &&
		obj.GetObjectKind().GroupVersionKind() == kinds.ConfigMap()
}

// IsReconcilerManagerConfigMap returns true if passed obj is the
// reconciler-manager ConfigMap reconciler-manager in config-management namespace.
func IsReconcilerManagerConfigMap(obj client.Object) bool {
	return obj.GetName() == reconcilermanager.ManagerName &&
		obj.GetNamespace() == configmanagement.ControllerNamespace &&
		obj.GetObjectKind().GroupVersionKind() == kinds.ConfigMap()
}

// isOtelCollectorDeployment returns true if passed obj is the
// otel-collector Deployment in the config-management-monitoring namespace.
func isOtelCollectorDeployment(obj client.Object) bool {
	return obj.GetName() == ocmetrics.OtelCollectorName &&
		obj.GetNamespace() == configmanagement.MonitoringNamespace &&
		obj.GetObjectKind().GroupVersionKind() == kinds.Deployment()
}

// isRootSyncCRD returns true if passed obj is the CRD for the RootSync resource.
func isRootSyncCRD(obj client.Object) bool {
	return obj.GetName() == configsync.RootSyncCRDName &&
		obj.GetObjectKind().GroupVersionKind() == kinds.CustomResourceDefinitionV1()
}

// ResetReconcilerManagerConfigMap resets the reconciler manager config map
// to what is defined in the manifest
func ResetReconcilerManagerConfigMap(nt *NT) error {
	objs, err := parseConfigSyncManifests(nt)
	if err != nil {
		return err
	}
	for _, obj := range objs {
		if !IsReconcilerTemplateConfigMap(obj) {
			continue
		}
		nt.T.Logf("ResetReconcilerManagerConfigMap obj: %v", core.GKNN(obj))
		err := nt.KubeClient.Update(obj)
		if err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to reset reconciler manager ConfigMap")
}

// InstallRootSyncCRD installs the RootSync CRD on the test cluster
func InstallRootSyncCRD(nt *NT) error {
	nt.T.Log("Installing RootSync CRD")
	objs, err := parseConfigSyncManifests(nt)
	if err != nil {
		return err
	}
	var crdObj client.Object
	for _, obj := range objs {
		if isRootSyncCRD(obj) {
			crdObj = obj
			break
		}
	}
	if crdObj == nil {
		return fmt.Errorf("RootSync CRD not found in manifests")
	}
	if err := nt.KubeClient.Apply(crdObj); err != nil {
		return err
	}
	gvk, err := kinds.Lookup(crdObj, nt.Scheme)
	if err != nil {
		return err
	}
	return nt.Watcher.WatchForCurrentStatus(gvk,
		crdObj.GetName(), crdObj.GetNamespace())
}

// UninstallRootSyncCRD uninstalls the RootSync CRD on the test cluster
func UninstallRootSyncCRD(nt *NT) error {
	nt.T.Log("Uninstalling RootSync CRD")
	rootSyncCRD := k8sobjects.CustomResourceDefinitionV1Object(
		core.Name(configsync.RootSyncCRDName))
	return DeleteObjectsAndWait(nt, rootSyncCRD)
}

func parseConfigSyncManifests(nt *NT) ([]client.Object, error) {
	tmpManifestsDir := filepath.Join(nt.TmpDir, configSyncManifests)
	objs, err := installationManifests(tmpManifestsDir)
	if err != nil {
		return nil, err
	}
	objs, err = convertToTypedObjects(nt, objs)
	if err != nil {
		return nil, err
	}
	reconcilerPollingPeriod = nt.ReconcilerPollingPeriod
	hydrationPollingPeriod = nt.HydrationPollingPeriod

	objs, err = multiRepoObjects(objs,
		setReconcilerDebugMode,
		setPollingPeriods,
		setOtelCollectorPrometheusAnnotations)
	if err != nil {
		return nil, err
	}
	return objs, nil
}

// InstallConfigSync installs ConfigSync on the test cluster
func InstallConfigSync(nt *NT) error {
	nt.T.Log("[SETUP] Installing Config Sync")
	objs, err := parseConfigSyncManifests(nt)
	if err != nil {
		return err
	}
	for _, o := range objs {
		nt.T.Logf("installConfigSync obj: %v", core.GKNN(o))
		if err := nt.KubeClient.Apply(o); err != nil {
			return err
		}
	}
	return nil
}

// uninstallConfigSync uninstalls ConfigSync on the test cluster
func uninstallConfigSync(nt *NT) error {
	nt.T.Log("[CLEANUP] Uninstalling Config Sync")
	objs, err := parseConfigSyncManifests(nt)
	if err != nil {
		return err
	}
	return DeleteObjectsAndWait(nt, objs...)
}

// convertToTypedObjects converts objects to their literal types. We can do this as
// we should have all required types in the Scheme anyway. This keeps us from
// having to do ugly Unstructured operations.
func convertToTypedObjects(nt *NT, objs []client.Object) ([]client.Object, error) {
	result := make([]client.Object, len(objs))
	for i, obj := range objs {
		uObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			// Already converted when read from the disk, or added manually.
			// We don't need to convert, so insert and go to the next element.
			result[i] = obj
			continue
		}
		rObj, err := kinds.ToTypedObject(uObj, nt.Scheme)
		if err != nil {
			return nil, err
		}
		cObj, err := kinds.ObjectAsClientObject(rObj)
		if err != nil {
			return nil, err
		}
		result[i] = cObj
	}
	return result, nil
}

func copyFile(src, dst string) error {
	bytes, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, bytes, fileMode)
}

func copyDirContents(src, dest string) error {
	files, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, f := range files {
		// Explicitly not recursive
		if f.IsDir() {
			continue
		}

		from := filepath.Join(src, f.Name())
		to := filepath.Join(dest, f.Name())
		err := copyFile(from, to)
		if err != nil {
			return err
		}
	}
	return nil
}

// installationManifests generates the ConfigSync installation YAML and copies
// it to the test's temporary directory.
func installationManifests(tmpManifestsDir string) ([]client.Object, error) {
	err := os.MkdirAll(tmpManifestsDir, fileMode)
	if err != nil {
		return nil, err
	}

	// Copy all manifests to temporary dir
	if err := copyDirContents(outputManifestsDir, tmpManifestsDir); err != nil {
		return nil, err
	}
	if err := copyFile(multiConfigMaps, filepath.Join(tmpManifestsDir, multiConfigMapsName)); err != nil {
		return nil, err
	}

	return parseManifestDir(tmpManifestsDir)
}

// parseManifestDir reads YAML from all the files in a directory and parses them
// into unstructured objects.
func parseManifestDir(dirPath string) ([]client.Object, error) {
	// Create the list of paths for the File to read.
	readPath, err := cmpath.AbsoluteOS(dirPath)
	if err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}
	paths := make([]cmpath.Absolute, len(files))
	for i, f := range files {
		paths[i] = readPath.Join(cmpath.RelativeSlash(f.Name()))
	}
	// Read the manifests cached in the tmpdir.
	r := reader.File{}
	filePaths := reader.FilePaths{
		RootDir: readPath,
		Files:   paths,
	}
	fos, err := r.Read(filePaths)
	if err != nil {
		return nil, err
	}

	var objs []client.Object
	for _, o := range fos {
		objs = append(objs, o.Unstructured)
	}
	return objs, nil
}

func multiRepoObjects(objects []client.Object, opts ...func(obj client.Object) error) ([]client.Object, error) {
	var filtered []client.Object
	found := false
	for _, obj := range objects {
		if IsReconcilerTemplateConfigMap(obj) {
			// Mark that we've found the ReconcilerManager ConfigMap.
			// This way we know we've enabled debug mode.
			found = true
		}
		for _, opt := range opts {
			if err := opt(obj); err != nil {
				return nil, err
			}
		}
		filtered = append(filtered, obj)
	}
	if !found {
		return nil, fmt.Errorf("Reconciler Manager ConfigMap not found")
	}
	return filtered, nil
}

// ValidateMultiRepoDeployments waits for all Config Sync components to become available.
// RootSync root-sync will be re-created, if not found.
func ValidateMultiRepoDeployments(nt *NT) error {
	// Create root-sync RootSync, if registered.
	if source, found := nt.SyncSources[DefaultRootSyncID]; found {
		id := DefaultRootSyncID
		var rs *v1beta1.RootSync
		switch tSource := source.(type) {
		case *syncsource.GitSyncSource:
			rs = nt.RootSyncObjectGitSyncSource(id.Name, tSource)
		case *syncsource.HelmSyncSource:
			rs = nt.RootSyncObjectHelmSyncSource(id.Name, tSource)
		case *syncsource.OCISyncSource:
			rs = nt.RootSyncObjectOCISyncSource(id.Name, tSource)
		default:
			nt.T.Fatalf("Invalid RootSync source %T: %s", source, id.Name)
		}
		if err := nt.KubeClient.Create(rs); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				nt.T.Fatal(err)
			}
		}
	}

	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			configmanagement.RGControllerName, configmanagement.RGControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			reconcilermanager.ManagerName, configmanagement.ControllerNamespace)
	})
	tg.Go(func() error {
		watchOptions := []testwatcher.WatchOption{
			testwatcher.WatchPredicates(testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus)),
		}
		// If testing on GKE, ensure that the otel-collector deployment has
		// picked up the `otel-collector-googlecloud` configmap.
		// This bumps the deployment generation.
		if *e2e.TestCluster == e2e.GKE {
			watchOptions = append(watchOptions, testwatcher.WatchPredicates(
				testpredicates.HasGenerationAtLeast(2)))
		}
		return nt.Watcher.WatchObject(kinds.Deployment(),
			ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace, watchOptions...)
	})
	tg.Go(func() error {
		// The root-reconciler is created after the reconciler-manager is ready.
		// So wait longer for it to come up.
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			DefaultRootReconcilerName, configmanagement.ControllerNamespace,
			testwatcher.WatchTimeout(nt.DefaultWaitTimeout*2))
	})
	if !*nt.WebhookDisabled {
		// If disabled, we don't need to wait for the webhook to be ready.
		tg.Go(func() error {
			return nt.Watcher.WatchForCurrentStatus(kinds.ValidatingWebhookConfiguration(),
				"admission-webhook.configsync.gke.io", "")
		})
		tg.Go(func() error {
			// The webhook takes a long time to come up, because of SSL cert
			// injection. So wait longer for it to come up.
			return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
				webhookconfig.ShortName, configmanagement.ControllerNamespace,
				testwatcher.WatchTimeout(nt.DefaultWaitTimeout*2))
		})
	}
	if err := tg.Wait(); err != nil {
		return err
	}

	return validateMultiRepoPods(nt)
}

// validateMultiRepoPods validates if all Config Sync Pods are healthy. It doesn't retry
// because it is supposed to be invoked after validateMultiRepoDeployments succeeds.
// It only checks the central root reconciler Pod.
func validateMultiRepoPods(nt *NT) error {
	for _, ns := range CSNamespaces {
		pods := corev1.PodList{}
		if err := nt.KubeClient.List(&pods, client.InNamespace(ns)); err != nil {
			return err
		}
		for _, pod := range pods.Items {
			if pod.Labels["app"] == "reconciler" {
				if pod.Labels[metadata.SyncKindLabel] != configsync.RootSyncKind || pod.Labels[metadata.SyncNameLabel] != configsync.RootSyncName {
					continue // Only validate the central root reconciler Pod but skip other reconcilers because other RSync objects might be managed by the central root reconciler and may not be reconciled yet.
				}
			}
			// Check if the Pod (running Pod only) has any former abnormally terminated container.
			// If the Pod is already in a terminating state (with a deletion timestamp) due to clean up, ignore the check.
			// It uses pod.DeletionTimestamp instead of pod.Status.Phase because the Pod may not be scheduled to evict yet.
			if pod.DeletionTimestamp == nil {
				if err := nt.Validate(pod.Name, pod.Namespace, &corev1.Pod{}, noOOMKilledContainer(nt)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// noOOMKilledContainer is a predicate to validate if the Pod has a container that was terminated due to OOMKilled
// It doesn't check other non-zero exit code because the admission-webhook sometimes exits with 1 and then recovers after restart.
// Example log:
//
//	kubectl logs admission-webhook-5c79b59f86-fzqgl -n config-management-system -p
//	I0826 07:54:47.435824       1 setup.go:31] Build Version: v1.13.0-rc.5-2-gef2d1ac-dirty
//	I0826 07:54:47.435992       1 logr.go:249] setup "msg"="starting manager"
//	E0826 07:55:17.436921       1 logr.go:265]  "msg"="Failed to get API Group-Resources" "error"="Get \"https://10.96.0.1:443/api?timeout=32s\": dial tcp 10.96.0.1:443: i/o timeout"
//	E0826 07:55:17.436945       1 logr.go:265] setup "msg"="starting manager" "error"="Get \"https://10.96.0.1:443/api?timeout=32s\": dial tcp 10.96.0.1:443: i/o timeout"
func noOOMKilledContainer(nt *NT) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		pod, ok := o.(*corev1.Pod)
		if !ok {
			return testpredicates.WrongTypeErr(o, pod)
		}

		for _, cs := range pod.Status.ContainerStatuses {
			terminated := cs.LastTerminationState.Terminated
			if terminated != nil && terminated.ExitCode != 0 && terminated.Reason == "OOMKilled" {
				// Display the full state of the malfunctioning Pod to aid in debugging.
				jsn, err := json.MarshalIndent(pod, "", "  ")
				if err != nil {
					return err
				}
				// Display the logs for the previous instance of the container.
				args := []string{"logs", pod.Name, "-n", pod.Namespace, "-p"}
				out, err := nt.Shell.Kubectl(args...)
				// Print a standardized header before each printed log to make ctrl+F-ing the
				// log you want easier.
				cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
				if err != nil {
					nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
				}
				return fmt.Errorf("%w for pod/%s in namespace %q, container %q terminated with exit code %d and reason %q\n\n%s\n\n%s",
					testpredicates.ErrFailedPredicate, pod.Name, pod.Namespace, cs.Name, terminated.ExitCode, terminated.Reason, string(jsn), fmt.Sprintf("%s\n%s", cmd, out))
			}
		}
		return nil
	}
}

// RepoSyncRoleBinding returns rolebinding that grants service account
// permission to manage resources in the namespace.
func RepoSyncRoleBinding(nn types.NamespacedName) *rbacv1.RoleBinding {
	rb := k8sobjects.RoleBindingObject(core.Name(nn.Name), core.Namespace(nn.Namespace))
	sb := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      core.NsReconcilerName(nn.Namespace, nn.Name),
			Namespace: configmanagement.ControllerNamespace,
		},
	}
	rf := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     clusterRoleName,
	}
	rb.Subjects = sb
	rb.RoleRef = rf
	return rb
}

func setupRepoSyncRoleBinding(nt *NT, nn types.NamespacedName) error {
	if err := nt.KubeClient.Create(RepoSyncRoleBinding(nn)); err != nil {
		nt.T.Fatal(err)
	}

	// Validate rolebinding is present.
	return nt.Validate(nn.Name, nn.Namespace, &rbacv1.RoleBinding{})
}

// setReconcilerDebugMode ensures the Reconciler deployments are run in debug mode.
func setReconcilerDebugMode(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	if !IsReconcilerTemplateConfigMap(obj) {
		return nil
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return fmt.Errorf("parsed Reconciler Manager ConfigMap was %T %v", obj, obj)
	}

	key := "deployment.yaml"
	deploymentYAML, found := cm.Data[key]
	if !found {
		return fmt.Errorf("Reconciler Manager ConfigMap has no deployment.yaml entry")
	}

	// The Deployment YAML for Reconciler deployments is a raw YAML string embedded
	// in the ConfigMap. Unmarshalling/marshalling is likely to lead to errors, so
	// this modifies the YAML string directly by finding the line we want to insert
	// the debug flag after, and then inserting the line we want to add.
	lines := strings.Split(deploymentYAML, "\n")
	found = false
	for i, line := range lines {
		// We want to set the debug flag immediately after setting the source-dir flag.
		if strings.Contains(line, "- \"--source-dir=/repo/source/rev\"") {
			// Standard Go "insert into slice" idiom.
			lines = append(lines, "")
			copy(lines[i+2:], lines[i+1:])
			// Prefix of 8 spaces as the run arguments are indented 8 spaces relative
			// to the embedded YAML string. The embedded YAML is indented 3 spaces,
			// so this is equivalent to indenting 11 spaces in the original file:
			// manifests/templates/reconciler-manager-configmap.yaml.
			lines[i+1] = "        - \"--debug\""
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Unable to set debug mode for reconciler")
	}

	cm.Data[key] = strings.Join(lines, "\n")
	return nil
}

// setPollingPeriods update Reconciler Manager configmap
// reconciler-manager-cm with reconciler and hydration-controller polling
// periods to override the default.
func setPollingPeriods(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	if !IsReconcilerManagerConfigMap(obj) {
		return nil
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return fmt.Errorf("parsed Reconciler Manager ConfigMap was not ConfigMap %T %v", obj, obj)
	}

	cm.Data[reconcilermanager.ReconcilerPollingPeriod] = reconcilerPollingPeriod.String()
	cm.Data[reconcilermanager.HydrationPollingPeriod] = hydrationPollingPeriod.String()
	return nil
}

// setOtelCollectorPrometheusAnnotations updates the otel-collector Deployment
// to add pod annotations to enable metrics scraping.
func setOtelCollectorPrometheusAnnotations(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	if !isOtelCollectorDeployment(obj) {
		return nil
	}
	dep, ok := obj.(*appsv1.Deployment)
	if !ok {
		return fmt.Errorf("failed to cast %T to *appsv1.Deployment", obj)
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = make(map[string]string, 2)
	}
	dep.Spec.Template.Annotations["prometheus.io/scrape"] = "true"
	dep.Spec.Template.Annotations["prometheus.io/port"] = strconv.Itoa(metrics.OtelCollectorMetricsPort)
	return nil
}

func setupDelegatedControl(nt *NT) {
	nt.T.Log("[SETUP] Delegated control")

	// Just create one RepoSync ClusterRole, even if there are no Namespace repos.
	if err := nt.KubeClient.Create(nt.RepoSyncClusterRole()); err != nil {
		nt.T.Fatal(err)
	}

	// Apply all the RootSyncs
	for id, source := range nt.SyncSources.RootSyncs() {
		// The default RootSync is created manually, no need to set up again.
		if id.Name == configsync.RootSyncName {
			continue
		}
		var rs *v1beta1.RootSync
		switch tSource := source.(type) {
		case *syncsource.GitSyncSource:
			rs = nt.RootSyncObjectGitSyncSource(id.Name, tSource)
		// TODO: setup OCI & Helm RootSyncs
		// case *syncsource.HelmSyncSource:
		// 	rs = nt.RootSyncObjectHelmSyncSource(id.Name, tSource)
		// case *syncsource.OCISyncSource:
		// 	rs = nt.RootSyncObjectOCISyncSource(id.Name, tSource)
		default:
			nt.T.Fatalf("Invalid %s source %T: %s", id.Kind, source, id.Name)
		}
		if err := nt.KubeClient.Apply(rs); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Then apply the RepoSyncs
	for id, source := range nt.SyncSources.RepoSyncs() {
		// create namespace for namespace reconciler.
		err := nt.KubeClient.Create(k8sobjects.NamespaceObject(id.Namespace))
		if err != nil {
			nt.T.Fatal(err)
		}

		nt.Must(CreateNamespaceSecrets(nt, id.Namespace))

		if err := setupRepoSyncRoleBinding(nt, id.ObjectKey); err != nil {
			nt.T.Fatal(err)
		}

		var rs *v1beta1.RepoSync
		switch tSource := source.(type) {
		case *syncsource.GitSyncSource:
			rs = nt.RepoSyncObjectGitSyncSource(id.ObjectKey, tSource)
		// TODO: setup OCI & Helm RootSyncs
		// case *syncsource.HelmSyncSource:
		// 	rs = nt.RepoSyncObjectHelmSyncSource(id.ObjectKey, tSource)
		// case *syncsource.OCISyncSource:
		// 	rs = nt.RepoSyncObjectOCISyncSource(id.ObjectKey, tSource)
		default:
			nt.T.Fatalf("Invalid %s source %T: %s", id.Kind, source, id.Name)
		}
		if err := nt.KubeClient.Apply(rs); err != nil {
			nt.T.Fatal(err)
		}
	}
}

// SetRSyncTestDefaults populates defaults for test RSyncs.
func SetRSyncTestDefaults(nt *NT, obj client.Object) {
	switch rs := obj.(type) {
	case *v1alpha1.RootSync:
		if nt.DefaultReconcileTimeout != nil && (rs.Spec.Override == nil || rs.Spec.Override.ReconcileTimeout == nil) {
			rs.Spec.SafeOverride().ReconcileTimeout = toMetav1Duration(*nt.DefaultReconcileTimeout)
		}
	case *v1beta1.RootSync:
		if nt.DefaultReconcileTimeout != nil && (rs.Spec.Override == nil || rs.Spec.Override.ReconcileTimeout == nil) {
			rs.Spec.SafeOverride().ReconcileTimeout = toMetav1Duration(*nt.DefaultReconcileTimeout)
		}
	case *v1alpha1.RepoSync:
		if nt.DefaultReconcileTimeout != nil && (rs.Spec.Override == nil || rs.Spec.Override.ReconcileTimeout == nil) {
			rs.Spec.SafeOverride().ReconcileTimeout = toMetav1Duration(*nt.DefaultReconcileTimeout)
		}
		// Add dependencies to ensure managed objects can be deleted.
		nt.Must(SetRepoSyncDependencies(nt, obj))
	case *v1beta1.RepoSync:
		if nt.DefaultReconcileTimeout != nil && (rs.Spec.Override == nil || rs.Spec.Override.ReconcileTimeout == nil) {
			rs.Spec.SafeOverride().ReconcileTimeout = toMetav1Duration(*nt.DefaultReconcileTimeout)
		}
		// Add dependencies to ensure managed objects can be deleted.
		nt.Must(SetRepoSyncDependencies(nt, obj))
	default:
		nt.T.Fatalf("Invalid RSync type %T", obj)
	}
	// Set global defaults
	// Enable automatic deletion of managed objects by default.
	// This helps ensure that test artifacts are cleaned up.
	EnableDeletionPropagation(obj)
}

// rootSyncObjectV1Alpha1Git returns the default RootSync object.
func rootSyncObjectV1Alpha1Git(name, repoURL string, sourceFormat configsync.SourceFormat) *v1alpha1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Alpha1(name)
	rs.Spec.SourceFormat = sourceFormat
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1alpha1.Git{
		Repo:   repoURL,
		Branch: gitproviders.MainBranch,
		Dir:    gitproviders.DefaultSyncDir,
		Period: metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.GitProvider {
	case e2e.CSR:
		// CSR provider requires auth because the repository is private
		if *e2e.GceNode {
			rs.Spec.Git.Auth = configsync.AuthGCENode
		} else {
			rs.Spec.Git.Auth = configsync.AuthGCPServiceAccount
			rs.Spec.Git.GCPServiceAccountEmail = gitproviders.CSRReaderEmail()
		}
	default:
		rs.Spec.Git.Auth = configsync.AuthSSH
		rs.Spec.Git.SecretRef = &v1alpha1.SecretReference{
			Name: controllers.GitCredentialVolume,
		}
	}
	return rs
}

// RootSyncObjectV1Alpha1FromRootRepo returns a v1alpha1 RootSync object which
// uses a RootSync repo from nt.SyncSources.
func RootSyncObjectV1Alpha1FromRootRepo(nt *NT, name string) *v1alpha1.RootSync {
	id := core.RootSyncID(name)
	source, found := nt.SyncSources[id]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", id.Kind, id.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", id, source)
	}
	// Use the existing Server expectations.
	rs := rootSyncObjectV1Alpha1Git(name, gitSource.Repository.SyncURL(), gitSource.SourceFormat)
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// rootSyncObjectV1Beta1Git returns the default RootSync object with version v1beta1.
func rootSyncObjectV1Beta1Git(name, repoURL string, branch, revision, syncPath string, sourceFormat configsync.SourceFormat) *v1beta1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Beta1(name)
	rs.Spec.SourceFormat = sourceFormat
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo:     repoURL,
		Branch:   branch,
		Revision: revision,
		Dir:      syncPath,
		Period:   metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.GitProvider {
	case e2e.CSR:
		// CSR provider requires auth because the repository is private
		if *e2e.GceNode {
			rs.Spec.Git.Auth = configsync.AuthGCENode
		} else {
			rs.Spec.Git.Auth = configsync.AuthGCPServiceAccount
			rs.Spec.Git.GCPServiceAccountEmail = gitproviders.CSRReaderEmail()
		}
	default:
		rs.Spec.Git.Auth = configsync.AuthSSH
		rs.Spec.Git.SecretRef = &v1beta1.SecretReference{
			Name: controllers.GitCredentialVolume,
		}
	}
	return rs
}

// RootSyncObjectV1Beta1FromRootRepo returns a v1beta1 RootSync object which
// uses a RootSync repo from nt.SyncSources.
// Use nt.RootSyncObjectGit is you want to change the source expectation.
func RootSyncObjectV1Beta1FromRootRepo(nt *NT, name string) *v1beta1.RootSync {
	id := core.RootSyncID(name)
	source, found := nt.SyncSources[id]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", id.Kind, id.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", id, source)
	}
	// Use the existing Server expectations.
	return nt.RootSyncObjectGitSyncSource(name, gitSource)
}

// RootSyncObjectV1Beta1FromOtherRootRepo returns a v1beta1 RootSync object which
// uses a repo from a specific nt.RootRepo.
// Use nt.RootSyncObjectGit is you want to change the source expectation.
func RootSyncObjectV1Beta1FromOtherRootRepo(nt *NT, syncName, repoName string) *v1beta1.RootSync {
	repoID := core.RootSyncID(repoName)
	source, found := nt.SyncSources[repoID]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", repoID.Kind, repoID.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", repoID, source)
	}
	// Copy the existing Server expectations to allow independent updates.
	return nt.RootSyncObjectGit(syncName, gitSource.Repository, gitSource.Branch, gitSource.Revision, gitSource.Directory, gitSource.SourceFormat)
}

// StructuredNSPath returns structured path with namespace and resourcename in repo.
func StructuredNSPath(namespace, resourceName string) string {
	return fmt.Sprintf("acme/namespaces/%s/%s.yaml", namespace, resourceName)
}

// repoSyncObjectV1Alpha1Git returns the default RepoSync object in the given namespace.
// SourceFormat for RepoSync must be Unstructured (default), so it's left unspecified.
func repoSyncObjectV1Alpha1Git(nn types.NamespacedName, repoURL string) *v1alpha1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Alpha1(nn.Namespace, nn.Name)
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1alpha1.Git{
		Repo:   repoURL,
		Branch: gitproviders.MainBranch,
		Dir:    gitproviders.DefaultSyncDir,
		Period: metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.GitProvider {
	case e2e.CSR:
		// CSR provider requires auth because the repository is private
		if *e2e.GceNode {
			rs.Spec.Git.Auth = configsync.AuthGCENode
		} else {
			rs.Spec.Git.Auth = configsync.AuthGCPServiceAccount
			rs.Spec.Git.GCPServiceAccountEmail = gitproviders.CSRReaderEmail()
		}
	default:
		rs.Spec.Git.Auth = configsync.AuthSSH
		rs.Spec.Git.SecretRef = &v1alpha1.SecretReference{
			Name: "ssh-key",
		}
	}
	return rs
}

// RepoSyncObjectV1Alpha1FromNonRootRepo returns a v1alpha1 RepoSync object which
// uses a RepoSync repo from nt.SyncSources.
func RepoSyncObjectV1Alpha1FromNonRootRepo(nt *NT, nn types.NamespacedName) *v1alpha1.RepoSync {
	id := core.RepoSyncID(nn.Name, nn.Namespace)
	source, found := nt.SyncSources[id]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", id.Kind, id.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", id, source)
	}
	// Use the existing Server expectations.
	// RepoSync is always Unstructured. So ignore repo.Format.
	rs := repoSyncObjectV1Alpha1Git(nn, gitSource.Repository.SyncURL())
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// repoSyncObjectV1Beta1Git returns the default RepoSync object
// with version v1beta1 in the given namespace.
func repoSyncObjectV1Beta1Git(nn types.NamespacedName, repoURL, branch, revision, syncPath string, sourceFormat configsync.SourceFormat) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1(nn.Namespace, nn.Name)
	rs.Spec.SourceFormat = sourceFormat
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo:     repoURL,
		Branch:   branch,
		Revision: revision,
		Dir:      syncPath,
		Period:   metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.GitProvider {
	case e2e.CSR:
		// CSR provider requires auth because the repository is private
		if *e2e.GceNode {
			rs.Spec.Git.Auth = configsync.AuthGCENode
		} else {
			rs.Spec.Git.Auth = configsync.AuthGCPServiceAccount
			rs.Spec.Git.GCPServiceAccountEmail = gitproviders.CSRReaderEmail()
		}
	default:
		rs.Spec.Git.Auth = configsync.AuthSSH
		rs.Spec.Git.SecretRef = &v1beta1.SecretReference{
			Name: "ssh-key",
		}
	}
	return rs
}

// RootSyncObjectGit creates a new RootSync object with a Git source, using the
// default test config plus the specified inputs.
// Creates, updates, or replaces the Server expectation for this RootSync.
func (nt *NT) RootSyncObjectGit(name string, repo gitproviders.Repository, branch, revision, syncPath string, sourceFormat configsync.SourceFormat) *v1beta1.RootSync {
	id := core.RootSyncID(name)
	source := &syncsource.GitSyncSource{
		Repository:   repo,
		Branch:       branch,
		Revision:     revision,
		Directory:    syncPath,
		SourceFormat: sourceFormat,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RootSyncObjectGitSyncSource(name, source)
}

// RootSyncObjectGitSyncSource creates a new RootSync object with a Git source,
// using the default test config plus the specified inputs.
func (nt *NT) RootSyncObjectGitSyncSource(name string, source *syncsource.GitSyncSource) *v1beta1.RootSync {
	rs := rootSyncObjectV1Beta1Git(name, source.Repository.SyncURL(), source.Branch, source.Revision, source.Directory, source.SourceFormat)
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RepoSyncObjectGit creates a new RepoSync object with a Git source, using the
// default test config plus the specified inputs.
// Creates, updates, or replaces the Server expectation for this RepoSync.
func (nt *NT) RepoSyncObjectGit(nn types.NamespacedName, repo gitproviders.Repository, branch, revision, syncPath string, sourceFormat configsync.SourceFormat) *v1beta1.RepoSync {
	id := core.RepoSyncID(nn.Name, nn.Namespace)
	source := &syncsource.GitSyncSource{
		Repository:   repo,
		Branch:       branch,
		Revision:     revision,
		Directory:    syncPath,
		SourceFormat: sourceFormat,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RepoSyncObjectGitSyncSource(nn, source)
}

// RepoSyncObjectGitSyncSource creates a new RepoSync object with a Git source,
// using the default test config plus the specified inputs.
func (nt *NT) RepoSyncObjectGitSyncSource(nn types.NamespacedName, source *syncsource.GitSyncSource) *v1beta1.RepoSync {
	// RepoSync is always Unstructured. So ignore repo.Format.
	rs := repoSyncObjectV1Beta1Git(nn, source.Repository.SyncURL(), source.Branch, source.Revision, source.Directory, source.SourceFormat)
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RootSyncObjectOCI returns a RootSync object that syncs the provided OCI image.
// ImageID is used for pulling. ImageDigest is used for validating the result.
// Creates, updates, or replaces the Server expectation for this RootSync.
func (nt *NT) RootSyncObjectOCI(name string, imageID registryproviders.OCIImageID, syncPath, expectedImageDigest string) *v1beta1.RootSync {
	id := core.RootSyncID(name)
	source := &syncsource.OCISyncSource{
		ImageID:             imageID,
		Directory:           syncPath,
		ExpectedImageDigest: expectedImageDigest,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RootSyncObjectOCISyncSource(name, source)
}

// RootSyncObjectOCISyncSource returns a RootSync object that syncs the provided
// OCI image.
func (nt *NT) RootSyncObjectOCISyncSource(name string, source *syncsource.OCISyncSource) *v1beta1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Beta1(name)
	// OCI Images can use hierarchy mode, but we don't have any tests for it yet.
	// So just hard-code unstructured by default for now.
	rs.Spec.SourceFormat = configsync.SourceFormatUnstructured
	rs.Spec.SourceType = configsync.OciSource
	rs.Spec.Oci = &v1beta1.Oci{
		Image:  source.ImageID.Address(),
		Dir:    source.Directory,
		Auth:   configsync.AuthNone,
		Period: metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.OCIProvider {
	case e2e.Local:
		// Local provider requires CA cert because host is not well known
		rs.Spec.Oci.CACertSecretRef = &v1beta1.SecretReference{
			Name: PublicCertSecretName(RegistrySyncSource),
		}
	case e2e.ArtifactRegistry:
		// AR provider requires auth because the registry is private
		rs.Spec.Oci.Auth = configsync.AuthGCPServiceAccount
		rs.Spec.Oci.GCPServiceAccountEmail = registryproviders.ArtifactRegistryReaderEmail()
	default:
		nt.T.Fatalf("Unrecognized OCIProvider: %s", *e2e.OCIProvider)
	}
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RepoSyncObjectOCI returns a RepoSync object that syncs the provided OCIImage.
// ImageID is used for pulling. ImageDigest is used for validating the result.
// Creates, updates, or replaces the Server expectation for this RepoSync.
func (nt *NT) RepoSyncObjectOCI(nn types.NamespacedName, imageID registryproviders.OCIImageID, syncPath, expectedImageDigest string) *v1beta1.RepoSync {
	id := core.RepoSyncID(nn.Name, nn.Namespace)
	source := &syncsource.OCISyncSource{
		ImageID:             imageID,
		Directory:           syncPath,
		ExpectedImageDigest: expectedImageDigest,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RepoSyncObjectOCISyncSource(nn, source)
}

// RepoSyncObjectOCISyncSource returns a RepoSync object that syncs the provided
// OCI image.
func (nt *NT) RepoSyncObjectOCISyncSource(nn types.NamespacedName, source *syncsource.OCISyncSource) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1(nn.Namespace, nn.Name)
	// RepoSync always uses unstructured. No need to set it.
	// rs.Spec.SourceFormat = configsync.SourceFormatUnstructured
	rs.Spec.SourceType = configsync.OciSource
	rs.Spec.Oci = &v1beta1.Oci{
		Image:  source.ImageID.Address(),
		Dir:    source.Directory,
		Auth:   configsync.AuthNone,
		Period: metav1.Duration{Duration: shortSyncPollingPeriod},
	}
	switch *e2e.OCIProvider {
	case e2e.Local:
		// Local provider requires CA cert because host is not well known
		rs.Spec.Oci.CACertSecretRef = &v1beta1.SecretReference{
			Name: PublicCertSecretName(RegistrySyncSource),
		}
	case e2e.ArtifactRegistry:
		// AR provider requires auth because the registry is private
		rs.Spec.Oci.Auth = configsync.AuthGCPServiceAccount
		rs.Spec.Oci.GCPServiceAccountEmail = registryproviders.ArtifactRegistryReaderEmail()
	default:
		nt.T.Fatalf("Unrecognized OCIProvider: %s", *e2e.OCIProvider)
	}
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RootSyncObjectHelm returns a RootSync object that syncs the provided Helm chart.
// Creates, updates, or replaces the Server expectation for this RootSync.
func (nt *NT) RootSyncObjectHelm(name string, chartID registryproviders.HelmChartID) *v1beta1.RootSync {
	id := core.RootSyncID(name)
	source := &syncsource.HelmSyncSource{
		ChartID: chartID,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RootSyncObjectHelmSyncSource(name, source)
}

// RootSyncObjectHelmSyncSource returns a RootSync object that syncs the
// provided Helm chart.
func (nt *NT) RootSyncObjectHelmSyncSource(name string, source *syncsource.HelmSyncSource) *v1beta1.RootSync {
	rs := k8sobjects.RootSyncObjectV1Beta1(name)
	rs.Spec.SourceFormat = configsync.SourceFormatUnstructured
	rs.Spec.SourceType = configsync.HelmSource
	rs.Spec.Helm = &v1beta1.HelmRootSync{
		HelmBase: v1beta1.HelmBase{
			Repo:    nt.HelmProvider.RepositoryRemoteURL(),
			Chart:   source.ChartID.Name,
			Version: source.ChartID.Version,
			Auth:    configsync.AuthNone,
		},
	}
	switch *e2e.HelmProvider {
	case e2e.Local:
		// Local provider requires CA cert because host is not well known
		rs.Spec.Helm.CACertSecretRef = &v1beta1.SecretReference{
			Name: PublicCertSecretName(RegistrySyncSource),
		}
	case e2e.ArtifactRegistry:
		// AR provider requires auth because the registry is private
		rs.Spec.Helm.Auth = configsync.AuthGCPServiceAccount
		rs.Spec.Helm.GCPServiceAccountEmail = registryproviders.ArtifactRegistryReaderEmail()
	default:
		nt.T.Fatalf("Unrecognized HelmProvider: %s", *e2e.OCIProvider)
	}
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RepoSyncObjectHelm returns a RepoSync object that syncs the provided HelmPackage.
// Creates, updates, or replaces the Server expectation for this RepoSync.
func (nt *NT) RepoSyncObjectHelm(nn types.NamespacedName, chartID registryproviders.HelmChartID) *v1beta1.RepoSync {
	id := core.RepoSyncID(nn.Name, nn.Namespace)
	source := &syncsource.HelmSyncSource{
		ChartID: chartID,
	}
	SetExpectedSyncSource(nt, id, source)
	return nt.RepoSyncObjectHelmSyncSource(nn, source)
}

// RepoSyncObjectHelmSyncSource returns a RepoSync object that syncs the
// provided Helm chart.
func (nt *NT) RepoSyncObjectHelmSyncSource(nn types.NamespacedName, source *syncsource.HelmSyncSource) *v1beta1.RepoSync {
	rs := k8sobjects.RepoSyncObjectV1Beta1(nn.Namespace, nn.Name)
	rs.Spec.SourceFormat = configsync.SourceFormatUnstructured
	rs.Spec.SourceType = configsync.HelmSource
	rs.Spec.Helm = &v1beta1.HelmRepoSync{
		HelmBase: v1beta1.HelmBase{
			Repo:    nt.HelmProvider.RepositoryRemoteURL(),
			Chart:   source.ChartID.Name,
			Version: source.ChartID.Version,
			Auth:    configsync.AuthNone,
		},
	}
	switch *e2e.HelmProvider {
	case e2e.Local:
		// Local provider requires CA cert because host is not well known
		rs.Spec.Helm.CACertSecretRef = &v1beta1.SecretReference{
			Name: PublicCertSecretName(RegistrySyncSource),
		}
	case e2e.ArtifactRegistry:
		// AR provider requires auth because the registry is private
		rs.Spec.Helm.Auth = configsync.AuthGCPServiceAccount
		rs.Spec.Helm.GCPServiceAccountEmail = registryproviders.ArtifactRegistryReaderEmail()
	default:
		nt.T.Fatalf("Unrecognized HelmProvider: %s", *e2e.OCIProvider)
	}
	SetRSyncTestDefaults(nt, rs)
	return rs
}

// RepoSyncObjectV1Beta1FromNonRootRepo returns a v1beta1 RepoSync object which
// uses a RepoSync repo from nt.SyncSources.
// Use nt.RepoSyncObjectGit is you want to change the source expectation.
func RepoSyncObjectV1Beta1FromNonRootRepo(nt *NT, nn types.NamespacedName) *v1beta1.RepoSync {
	id := core.RepoSyncID(nn.Name, nn.Namespace)
	source, found := nt.SyncSources[id]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", id.Kind, id.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", id, source)
	}
	// Use the existing Server expectations.
	return nt.RepoSyncObjectGitSyncSource(nn, gitSource)
}

// RepoSyncObjectV1Beta1FromOtherRootRepo returns a v1beta1 RepoSync object which
// uses a RootSync repo from nt.SyncSources.
// Use nt.RepoSyncObjectGit is you want to change the source expectation.
func RepoSyncObjectV1Beta1FromOtherRootRepo(nt *NT, nn types.NamespacedName, repoName string) *v1beta1.RepoSync {
	repoID := core.RootSyncID(repoName)
	source, found := nt.SyncSources[repoID]
	if !found {
		nt.T.Fatalf("nonexistent %s source: %s", repoID.Kind, repoID.ObjectKey)
	}
	gitSource, ok := source.(*syncsource.GitSyncSource)
	if !ok {
		nt.T.Fatalf("expected SyncSources for %s to have *GitSyncSource, but got %T", repoID, source)
	}
	// Copy the existing Server expectations to allow independent updates.
	return nt.RepoSyncObjectGit(nn, gitSource.Repository, gitSource.Branch, gitSource.Revision, gitSource.Directory, gitSource.SourceFormat)
}

// setupCentralizedControl is a pure central-control mode.
// A default root repo (root-sync) manages all other root repos and namespace repos.
func setupCentralizedControl(nt *NT) {
	nt.T.Log("[SETUP] Centralized control")

	rootSyncID := core.RootSyncID(configsync.RootSyncName)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	// Add any RootSyncs specified by the test options
	rootSyncSources := nt.SyncSources.RootSyncs()
	rootSyncs := make(map[core.ID]*v1beta1.RootSync, len(rootSyncSources))
	for id, source := range rootSyncSources {
		// The default RootSync is created manually, don't check it in the repo.
		if id.Name == configsync.RootSyncName {
			continue
		}
		switch tSource := source.(type) {
		case *syncsource.GitSyncSource:
			rootSyncs[id] = nt.RootSyncObjectGitSyncSource(id.Name, tSource)
		// TODO: setup OCI & Helm RootSyncs
		// case *syncsource.HelmSyncSource:
		// 	rootSyncs[id] = nt.RootSyncObjectHelmSyncSource(id.Name, tSource)
		// case *syncsource.OCISyncSource:
		// 	rootSyncs[id] = nt.RootSyncObjectOCISyncSource(id.Name, tSource)
		default:
			nt.T.Fatalf("Invalid %s source %T: %s", id.Kind, source, id.Name)
		}
	}
	for id, rs := range rootSyncs {
		// Add RootSync
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/%s.yaml", id.Namespace, id.Name), rs))
		nt.MetricsExpectations.AddObjectApply(metrics.SyncKind(rootSyncID.Kind), rootSyncID.ObjectKey, rs)
	}
	if len(rootSyncs) > 0 {
		nt.Must(rootSyncGitRepo.CommitAndPush("Adding RootSyncs"))
	}

	repoSyncSources := nt.SyncSources.RepoSyncs()
	if len(repoSyncSources) == 0 {
		return
	}

	// Add ClusterRole for all RepoSync reconcilers to use.
	// TODO: Test different permissions for different RepoSyncs.
	cr := nt.RepoSyncClusterRole()
	nt.Must(rootSyncGitRepo.Add("acme/cluster/cr.yaml", cr))
	nt.MetricsExpectations.AddObjectApply(metrics.SyncKind(rootSyncID.Kind), rootSyncID.ObjectKey, cr)

	// Use a map to record the number of RepoSync namespaces
	rsNamespaces := map[string]struct{}{}

	// Add any RootSyncs specified by the test options
	repoSyncs := make(map[core.ID]*v1beta1.RepoSync, len(repoSyncSources))
	for id, source := range repoSyncSources {
		switch tSource := source.(type) {
		case *syncsource.GitSyncSource:
			repoSyncs[id] = nt.RepoSyncObjectGitSyncSource(id.ObjectKey, tSource)
		// TODO: setup OCI & Helm RootSyncs
		// case *syncsource.HelmSyncSource:
		// 	repoSyncs[id] = nt.RepoSyncObjectHelmSyncSource(id.ObjectKey, tSource)
		// case *syncsource.OCISyncSource:
		// 	repoSyncs[id] = nt.RepoSyncObjectOCISyncSource(id.ObjectKey, tSource)
		default:
			nt.T.Fatalf("Invalid %s source %T: %s", id.Kind, source, id.Name)
		}
	}
	for id, rs := range repoSyncs {
		rsNamespaces[id.Namespace] = struct{}{}

		// Add Namespace for this RepoSync.
		// This may replace an existing Namespace, if there are multiple
		// RepoSyncs in the same namespace.
		nsObj := k8sobjects.NamespaceObject(id.Namespace)
		nt.Must(rootSyncGitRepo.Add(StructuredNSPath(id.Namespace, id.Namespace), nsObj))
		nt.MetricsExpectations.AddObjectApply(metrics.SyncKind(rootSyncID.Kind), rootSyncID.ObjectKey, nsObj)

		// Add RoleBinding to bind the ClusterRole to the RepoSync reconciler ServiceAccount
		rb := RepoSyncRoleBinding(id.ObjectKey)
		nt.Must(rootSyncGitRepo.Add(StructuredNSPath(id.Namespace, fmt.Sprintf("rb-%s", id.Name)), rb))
		nt.MetricsExpectations.AddObjectApply(metrics.SyncKind(rootSyncID.Kind), rootSyncID.ObjectKey, rb)

		// Add RepoSync
		nt.Must(rootSyncGitRepo.Add(StructuredNSPath(id.Namespace, id.Name), rs))
		nt.MetricsExpectations.AddObjectApply(metrics.SyncKind(rootSyncID.Kind), rootSyncID.ObjectKey, rs)
	}
	if len(repoSyncs) > 0 {
		nt.Must(rootSyncGitRepo.CommitAndPush("Adding RepoSyncs, Namespaces, ClusterRoles, RoleBindings, and ClusterRoleBindings"))
	}

	// Convert namespace set to list
	namespaces := make([]string, 0, len(rsNamespaces))
	for namespace := range rsNamespaces {
		namespaces = append(namespaces, namespace)
	}

	// Wait for all Namespaces to be applied and reconciled.
	WaitForNamespaces(nt, nt.DefaultWaitTimeout, namespaces...)

	// Now that the Namespaces exist, create the Secrets,
	// which are required for the RepoSyncs to reconcile.
	for nn := range repoSyncs {
		nt.Must(CreateNamespaceSecrets(nt, nn.Namespace))
	}
}

func validateRootSyncsExist(nt *NT) error {
	var err error
	for id := range nt.SyncSources.RootSyncs() {
		err = multierr.Append(err,
			nt.Validate(id.Name, id.Namespace, &v1beta1.RootSync{}))
	}
	return err
}

func validateRepoSyncsExist(nt *NT) error {
	var err error
	for id := range nt.SyncSources.RepoSyncs() {
		err = multierr.Append(err,
			nt.Validate(id.Name, id.Namespace, &v1beta1.RepoSync{}))
	}
	return err
}

// SetRepoSyncDependencies sets the depends-on annotation on the RepoSync for
// other objects managed by CentralControl (`setupCentralizedControl`).
//
// RootSync dependencies aren't managed by another RootSync, and
// DelegatedControl doesn't apply with ConfigSync, so depends-on won't have
// any effect in those scenerios.
//
// Takes an Object because it supports both v1alpha1 and v1beta1 RepoSyncs.
func SetRepoSyncDependencies(nt *NT, rs client.Object) error {
	if nt.Control != ntopts.CentralControl {
		return nil
	}
	gvk, err := kinds.Lookup(rs, nt.Scheme)
	if err != nil {
		return err
	}
	if gvk.Kind != "RepoSync" {
		return fmt.Errorf("expected RepoSync, but got %s", gvk.Kind)
	}

	rsNN := client.ObjectKeyFromObject(rs)
	dependencies := []client.Object{
		nt.RepoSyncClusterRole(),
		RepoSyncRoleBinding(rsNN),
	}
	return SetDependencies(rs, dependencies...)
}

// SetDependencies sets the specified objects as dependencies of the first object.
func SetDependencies(obj client.Object, dependencies ...client.Object) error {
	var deps dependson.DependencySet
	for _, dep := range dependencies {
		deps = append(deps, applier.ObjMetaFromObject(dep))
	}
	if err := setDependsOnAnnotation(obj, deps); err != nil {
		return fmt.Errorf("failed to set dependencies on %T %s: %w", obj, client.ObjectKeyFromObject(obj), err)
	}
	return nil
}

// setDependsOnAnnotation is a copy of cli-utils dependson.WriteAnnotation, but
// it accepts a core.Annotated instead of an Unstructured.
func setDependsOnAnnotation(obj core.Annotated, depSet dependson.DependencySet) error {
	if obj == nil {
		return errors.New("object is nil")
	}
	if depSet.Equal(dependson.DependencySet{}) {
		return errors.New("dependency set is empty")
	}
	depSetStr, err := dependson.FormatDependencySet(depSet)
	if err != nil {
		return fmt.Errorf("failed to format depends-on annotation: %w", err)
	}
	core.SetAnnotation(obj, dependson.Annotation, depSetStr)
	return nil
}

// podHasReadyCondition checks if a pod status has a READY condition.
func podHasReadyCondition(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// NewPodReady checks if the new created pods are ready.
// It also checks if the new children pods that are managed by the pods are ready.
func NewPodReady(nt *NT, labelName, currentLabel, childLabel string, oldCurrentPods, oldChildPods []corev1.Pod) error {
	if len(oldCurrentPods) == 0 {
		return nil
	}
	newPods := &corev1.PodList{}
	if err := nt.KubeClient.List(newPods, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{labelName: currentLabel}); err != nil {
		nt.T.Fatal(err)
	}
	for _, newPod := range newPods.Items {
		for _, oldPod := range oldCurrentPods {
			if newPod.Name == oldPod.Name {
				return fmt.Errorf("old pod %s is still alive", oldPod.Name)
			}
		}
		if !podHasReadyCondition(newPod.Status.Conditions) {
			return fmt.Errorf("new pod %s is not ready yet", currentLabel)
		}
	}
	return NewPodReady(nt, labelName, childLabel, "", oldChildPods, nil)
}

// DeletePodByLabel deletes pods that have the label and waits until new pods come up.
func DeletePodByLabel(nt *NT, label, value string, waitForChildren bool) {
	oldPods := &corev1.PodList{}
	if err := nt.KubeClient.List(oldPods, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: value}); err != nil {
		nt.T.Fatal(err)
	}
	oldReconcilers := &corev1.PodList{}
	if value == reconcilermanager.ManagerName && waitForChildren {
		// Get the children pods managed by the reconciler-manager.
		if err := nt.KubeClient.List(oldReconcilers, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: reconcilermanager.Reconciler}); err != nil {
			nt.T.Fatal(err)
		}
	}
	if err := nt.KubeClient.DeleteAllOf(&corev1.Pod{}, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: value}); err != nil {
		nt.T.Fatalf("Pod delete failed: %v", err)
	}
	Wait(nt.T, "new pods come up", nt.DefaultWaitTimeout, func() error {
		if value == reconcilermanager.ManagerName && waitForChildren {
			// Wait for both the new reconciler-manager pod and the new reconcilers are ready.
			return NewPodReady(nt, label, value, reconcilermanager.Reconciler, oldPods.Items, oldReconcilers.Items)
		}
		return NewPodReady(nt, label, value, "", oldPods.Items, nil)
	}, WaitTimeout(nt.DefaultWaitTimeout))
}

// SetRootSyncGitDir updates the root-sync object with the provided git dir.
func SetRootSyncGitDir(nt *NT, syncName, syncPath string) {
	nt.T.Logf("Set spec.git.dir to %q", syncPath)
	rs := k8sobjects.RootSyncObjectV1Beta1(syncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": %q}}}`, syncPath))
}

// SetRootSyncGitBranch updates the root-sync object with the provided git branch
func SetRootSyncGitBranch(nt *NT, syncName, branch string) {
	nt.T.Logf("Set spec.git.branch to %q", branch)
	rs := k8sobjects.RootSyncObjectV1Beta1(syncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"branch": %q, "revision": null}}}`, branch))
}

// SetRootSyncGitRevision updates the root-sync object with the provided git branch
func SetRootSyncGitRevision(nt *NT, syncName, revision string) {
	nt.T.Logf("Set spec.git.revision to %q", revision)
	rs := k8sobjects.RootSyncObjectV1Beta1(syncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"revision": %q, "branch": null}}}`, revision))
}

func toMetav1Duration(t time.Duration) *metav1.Duration {
	return &metav1.Duration{
		Duration: t,
	}
}

// SetupFakeSSHCreds sets up the RSync Secret when needed.
// The Secret may or may not exist depending on the test settings.
//   - If the Git provider is local, bitbucket, or gitlab, the test scaffolding
//     uses 'ssh' as the auth type. The Secret should already exist in this case.
//   - If the Git provider is CSR, it uses 'gcpserviceaccount' as the auth type.
//     The Secret won't be created. Hence, this function creates a fake one to
//     bypass the validation in the reconciler-manager.
func SetupFakeSSHCreds(nt *NT, rsKind string, rsRef types.NamespacedName, auth configsync.AuthType, secretName string) error {
	if controllers.SkipForAuth(auth) {
		nt.T.Logf("The auth type %s doesn't need a Secret", auth)
		return nil
	}
	secret := k8sobjects.SecretObject(secretName, core.Namespace(rsRef.Namespace))
	err := nt.KubeClient.Get(secret.Name, secret.Namespace, secret)
	if err == nil {
		// The Secret is already created by the test scaffolding.
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	nt.T.Logf("The %s/%s Secret doesn't exist with auth %q, so creating a fake one", rsRef.Namespace, secretName, auth)
	msg := fmt.Sprintf("Secret %s not found: create one to allow client authentication", secretName)
	if rsKind == kinds.RootSyncV1Beta1().Kind {
		nt.WaitForRootSyncStalledError(rsRef.Name, "Validation", msg)
	} else {
		nt.WaitForRepoSyncStalledError(rsRef.Namespace, rsRef.Name, "Validation", msg)
	}
	secret.Data = map[string][]byte{"ssh": {}}
	if err = nt.KubeClient.Create(secret); err != nil {
		return err
	}
	nt.T.Cleanup(func() {
		if err = nt.KubeClient.Delete(secret); err != nil {
			nt.T.Error(err)
		}
	})
	return nil
}
