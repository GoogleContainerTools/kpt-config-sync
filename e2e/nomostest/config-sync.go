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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/monitor/state"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	webhookconfig "kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// AcmeDir is the sync directory of the test source repository.
	AcmeDir = "acme"
	// Manifests is the folder of the test manifests
	Manifests     = "manifests"
	testResources = "test-resources"

	// e2e/raw-nomos/manifests/mono-repo-configmaps.yaml
	monoConfigMapsName = "mono-repo-configmaps.yaml"
	// e2e/raw-nomos/manifests/multi-repo-configmaps.yaml
	multiConfigMapsName = "multi-repo-configmaps.yaml"
)

var (
	// baseDir is the path to the Nomos repository root from test case files.
	//
	// All paths must be relative to the test file that is running. There is probably
	// a more elegant way to do this.
	baseDir          = filepath.FromSlash("../..")
	manifestsDir     = filepath.Join(baseDir, Manifests)
	testResourcesDir = filepath.Join(manifestsDir, testResources)
	templateDir      = filepath.Join(manifestsDir, "templates")

	monoConfigMaps  = filepath.Join(baseDir, "e2e", "raw-nomos", Manifests, monoConfigMapsName)
	multiConfigMaps = filepath.Join(baseDir, "e2e", "raw-nomos", Manifests, multiConfigMapsName)

	// clusterRoleName is the ClusterRole used by Namespace Reconciler.
	clusterRoleName = fmt.Sprintf("%s:%s", configsync.GroupName, reconciler.NsReconcilerPrefix)

	templates = []string{
		"admission-webhook.yaml",
		"git-importer.yaml",
		"monitor.yaml",
		"otel-collector.yaml",
		"reconciler-manager.yaml",
		"reconciler-manager-configmap.yaml",
	}

	// monoObjects contains the names of all objects that are necessary to install
	// and run mono-repo Config Sync.
	monoObjects = map[string]bool{
		"configmanagement.gke.io:importer":         true,
		"configmanagement.gke.io:monitor":          true,
		"clusterconfigs.configmanagement.gke.io":   true,
		"cluster-name":                             true,
		filesystem.GitImporterName:                 true,
		reconcilermanager.GitSync:                  true,
		"hierarchyconfigs.configmanagement.gke.io": true,
		importer.Name:                              true,
		state.MonitorName:                          true,
		"namespaceconfigs.configmanagement.gke.io": true,
		"repos.configmanagement.gke.io":            true,
		reconcilermanager.SourceFormat:             true,
		"syncs.configmanagement.gke.io":            true,
	}
	// multiObjects contains the names of all objects that are necessary to
	// install and run multi-repo Config Sync.
	multiObjects = map[string]bool{
		webhookconfig.ShortName:                      true,
		"admission-webhook-cert":                     true,
		"configsync.gke.io:admission-webhook":        true,
		"admission-webhook.configsync.gke.io":        true,
		"configsync.gke.io:reconciler-manager":       true,
		reconcilermanager.ManagerName:                true,
		"reconciler-manager-cm":                      true,
		"reposyncs.configsync.gke.io":                true,
		"rootsyncs.configsync.gke.io":                true,
		metrics.OtelAgentName:                        true,
		metrics.OtelCollectorName:                    true,
		"acm-psp":                                    true,
		"configmanagement.gke.io:otel-collector-psp": true,
		// ResourceGroup CRD
		"resourcegroups.kpt.dev": true,
	}
	// sharedObjects contains the names of all objects that are needed by both
	// mono-repo and multi-repo Config Sync.
	sharedObjects = map[string]bool{
		"clusters.clusterregistry.k8s.io":            true,
		"clusterselectors.configmanagement.gke.io":   true,
		"container-limits":                           true,
		"namespaceselectors.configmanagement.gke.io": true,
		"configsync.gke.io:admission-webhook":        true,
	}
	// ignoredObjects:
	// config-management-system, this namespace gets created elsewhere
	resourcegroupObjects = map[string]bool{
		"resource-group-system":                             true,
		"resource-group-sa":                                 true,
		"resource-group-leader-election-role":               true,
		"resource-group-manager-role":                       true,
		"resource-group-metrics-reader":                     true,
		"resource-group-proxy-role":                         true,
		"resource-group-leader-election-rolebinding":        true,
		"resource-group-manager-rolebinding":                true,
		"resource-group-proxy-rolebinding":                  true,
		"resource-group-otel-agent":                         true,
		"resource-group-controller-manager-metrics-service": true,
		"resource-group-controller-manager":                 true,
	}
)

var (
	// reconcilerPollingPeriod specifies reconciler polling period as time.Duration
	reconcilerPollingPeriod time.Duration
	// hydrationPollingPeriod specifies hydration-controller polling period as time.Duration
	hydrationPollingPeriod time.Duration
)

// IsReconcilerManagerConfigMap returns true if passed obj is the
// reconciler-manager ConfigMap reconciler-manager-cm in config-management namespace.
var IsReconcilerManagerConfigMap = func(obj client.Object) bool {
	return obj.GetName() == "reconciler-manager-cm" &&
		obj.GetNamespace() == "config-management-system" &&
		obj.GetObjectKind().GroupVersionKind() == kinds.ConfigMap()
}

func parseManifests(nt *NT, nomos ntopts.Nomos) []client.Object {
	nt.T.Helper()
	tmpManifestsDir := filepath.Join(nt.TmpDir, Manifests)

	objs := installationManifests(nt, tmpManifestsDir, nomos)
	objs = convertObjects(nt, objs)
	if nomos.MultiRepo {
		reconcilerPollingPeriod = nt.ReconcilerPollingPeriod
		hydrationPollingPeriod = nt.HydrationPollingPeriod
		objs = multiRepoObjects(nt.T, nt.MultiRepo, objs, setReconcilerDebugMode, setPollingPeriods)
	} else {
		objs = monoRepoObjects(objs)
	}
	return objs
}

// installConfigSync installs ConfigSync on the test cluster, and returns a
// callback for checking that the installation succeeded.
func installConfigSync(nt *NT, nomos ntopts.Nomos) {
	nt.T.Helper()
	objs := parseManifests(nt, nomos)
	for _, o := range objs {
		nt.T.Logf("installConfigSync obj: %v", core.GKNN(o))
		if o.GetObjectKind().GroupVersionKind().GroupKind() == kinds.ConfigMap().GroupKind() && o.GetName() == reconcilermanager.SourceFormat {
			cm := o.(*corev1.ConfigMap)
			cm.Data[filesystem.SourceFormatKey] = string(nomos.SourceFormat)
		}

		err := nt.Create(o)
		if err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			nt.T.Fatal(err)
		}
	}
}

// waitForConfigSync validates if the config sync deployment is ready.
func waitForConfigSync(nt *NT, nomos ntopts.Nomos) error {
	if nomos.MultiRepo {
		return validateMultiRepoDeployments(nt)
	}
	return validateMonoRepoDeployments(nt)
}

// convertObjects converts objects to their literal types. We can do this as
// we should have all required types in the scheme anyway. This keeps us from
// having to do ugly Unstructured operations.
func convertObjects(nt *NT, objs []client.Object) []client.Object {
	result := make([]client.Object, len(objs))
	for i, obj := range objs {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok {
			// Already converted when read from the disk, or added manually.
			// We don't need to convert, so insert and go to the next element.
			result[i] = obj
			continue
		}

		o, err := nt.scheme.New(u.GroupVersionKind())
		if err != nil {
			nt.T.Fatalf("installed type %v not in Scheme: %v", u.GroupVersionKind(), err)
		}

		jsn, err := u.MarshalJSON()
		if err != nil {
			nt.T.Fatalf("marshalling object into JSON: %v", err)
		}

		err = json.Unmarshal(jsn, o)
		if err != nil {
			nt.T.Fatalf("unmarshalling JSON into object: %v", err)
		}
		newObj, ok := o.(client.Object)
		if !ok {
			nt.T.Fatalf("trying to install non-object type %v", u.GroupVersionKind())
		}
		result[i] = newObj
	}
	return result
}

func copyFile(src, dst string) error {
	bytes, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dst, bytes, fileMode)
}

func copyDirContents(src, dest string) error {
	files, err := ioutil.ReadDir(src)
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
func installationManifests(nt *NT, tmpManifestsDir string, nomos ntopts.Nomos) []client.Object {
	nt.T.Helper()
	err := os.MkdirAll(tmpManifestsDir, fileMode)
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.DebugLog("copying test-only-resources")
	err = copyDirContents(testResourcesDir, tmpManifestsDir)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.DebugLog("copying manifests, not including manifests/templates/")
	err = copyDirContents(manifestsDir, tmpManifestsDir)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update 'GIT_REPO_URL' in mono-repo ConfigMaps.
	// 'GIT_REPO_URL' in the `git-sync` configmap for monorepo-mode needs to be reset dynamically.
	bytes, err := ioutil.ReadFile(monoConfigMaps)
	if err != nil {
		nt.T.Fatal(err)
	}

	var syncURL string
	if nt.GitProvider.Type() == e2e.Local {
		syncURL = nt.GitProvider.SyncURL(DefaultRootRepoNamespacedName.String())
	} else {
		if _, found := nt.RootRepos[configsync.RootSyncName]; !found {
			// Setting GIT_REPO_URL in the configmap requires a remote repo to be present, so we need to create one if not exists.
			// We can't call resetRepository() because it resets the existing repo to an initial state.
			// There are cases that we want to install config sync but keep using the current repo (e.g. switch_mode_test.go).
			nt.RootRepos[configsync.RootSyncName] = NewRepository(nt, RootRepo, DefaultRootRepoNamespacedName, nomos.UpstreamURL, nomos.SourceFormat)
		}
		syncURL = nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	}
	replaced := strings.ReplaceAll(string(bytes), "GIT_REPO_URL", syncURL)
	if err := ioutil.WriteFile(filepath.Join(tmpManifestsDir, monoConfigMapsName), []byte(replaced), fileMode); err != nil {
		nt.T.Fatal(err)
	}
	if err := copyFile(multiConfigMaps, filepath.Join(tmpManifestsDir, multiConfigMapsName)); err != nil {
		nt.T.Fatal(err)
	}

	// Generate the Deployment YAML.
	for _, template := range templates {
		// It isn't strictly necessary for us to have the YAML file
		// (we could apply directly), but it is very helpful for debugging.
		bytes, err := ioutil.ReadFile(filepath.Join(templateDir, template))
		if err != nil {
			nt.T.Fatal(err)
		}
		replaced := string(bytes)

		var imgName string
		switch template {
		case "reconciler-manager.yaml":
			// For the reconciler manager template, we want the latest image for the reconciler manager.
			imgName = fmt.Sprintf("%s/reconciler-manager:%s", *e2e.ImagePrefix, *e2e.ImageTag)
		case "reconciler-manager-configmap.yaml":
			// For the reconciler deployment template, we want the latest image for the reconciler and hydration-controller.
			reconcilerImgName := fmt.Sprintf("%s/reconciler:%s", *e2e.ImagePrefix, *e2e.ImageTag)
			replaced = strings.ReplaceAll(replaced, "RECONCILER_IMAGE_NAME", reconcilerImgName)
			hydrationControllerImgName := fmt.Sprintf("%s/hydration-controller:%s", *e2e.ImagePrefix, *e2e.ImageTag)
			replaced = strings.ReplaceAll(replaced, "HYDRATION_CONTROLLER_IMAGE_NAME", hydrationControllerImgName)
			ociSyncImgName := fmt.Sprintf("%s/oci-sync:%s", *e2e.ImagePrefix, *e2e.ImageTag)
			replaced = strings.ReplaceAll(replaced, "OCI_SYNC_IMAGE_NAME", ociSyncImgName)
		case "admission-webhook.yaml":
			imgName = fmt.Sprintf("%s/admission-webhook:%s", *e2e.ImagePrefix, *e2e.ImageTag)
		default:
			// For any other template, we want the latest image for the nomos binary (mono-repo).
			imgName = fmt.Sprintf("%s/nomos:%s", *e2e.ImagePrefix, *e2e.ImageTag)
		}

		if template != "reconciler-manager-configmap.yaml" {
			replaced = strings.ReplaceAll(replaced, "IMAGE_NAME", imgName)
		}

		err = ioutil.WriteFile(filepath.Join(tmpManifestsDir, template), []byte(replaced), fileMode)
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	// Create the list of paths for the File to read.
	readPath, err := cmpath.AbsoluteOS(tmpManifestsDir)
	if err != nil {
		nt.T.Fatal(err)
	}
	files, err := ioutil.ReadDir(tmpManifestsDir)
	if err != nil {
		nt.T.Fatal(err)
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
		nt.T.Fatal(err)
	}

	var objs []client.Object
	for _, o := range fos {
		objs = append(objs, o.Unstructured)
	}
	return objs
}

func monoRepoObjects(objects []client.Object) []client.Object {
	var filtered []client.Object
	for _, obj := range objects {
		if monoObjects[obj.GetName()] || sharedObjects[obj.GetName()] {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}

func multiRepoObjects(t testing.NTB, resourcegroup bool, objects []client.Object, opts ...func(t testing.NTB, obj client.Object)) []client.Object {
	var filtered []client.Object
	found := false
	for _, obj := range objects {
		if IsReconcilerManagerConfigMap(obj) {
			// Mark that we've found the ReconcilerManager ConfigMap.
			// This way we know we've enabled debug mode.
			found = true
		}
		for _, opt := range opts {
			opt(t, obj)
		}
		if multiObjects[obj.GetName()] || sharedObjects[obj.GetName()] {
			filtered = append(filtered, obj)
		}
		if resourcegroup && resourcegroupObjects[obj.GetName()] {
			filtered = append(filtered, obj)
		}
	}
	if !found {
		t.Fatal("Did not find Reconciler Manager ConfigMap")
	}
	return filtered
}

func validateMonoRepoDeployments(nt *NT) error {
	took, err := Retry(nt.DefaultWaitTimeout, func() error {
		err := nt.Validate("monitor", configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
		if err != nil {
			return err
		}
		return nt.Validate(filesystem.GitImporterName, configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
	})
	if err != nil {
		return err
	}
	nt.T.Logf("took %v to wait for monitor and git-importer", took)
	return nil
}

func validateMultiRepoDeployments(nt *NT) error {
	for name := range nt.RootRepos {
		// Create a RootSync to initialize the root reconciler.
		rs := RootSyncObjectV1Beta1(name, nt.GitProvider.SyncURL(nt.RootRepos[name].RemoteRepoName), nt.RootRepos[name].Format)
		if err := nt.Create(rs); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				nt.T.Fatal(err)
			}
		}
	}

	took, err := Retry(nt.DefaultWaitTimeout, func() error {
		err := nt.Validate(reconcilermanager.ManagerName, configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
		if err != nil {
			return err
		}
		err = nt.Validate(DefaultRootReconcilerName, configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
		if err != nil {
			return err
		}
		err = nt.Validate(webhookconfig.ShortName, configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
		if err != nil {
			return err
		}
		err = nt.Validate("admission-webhook.configsync.gke.io", "", &admissionv1.ValidatingWebhookConfiguration{})
		if err != nil {
			return err
		}
		return nt.Validate(metrics.OtelCollectorName, metrics.MonitoringNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
	})
	if err != nil {
		return err
	}
	nt.T.Logf("took %v to wait for %s, %s, %s, and %s", took, reconcilermanager.ManagerName, DefaultRootReconcilerName, webhookconfig.ShortName, metrics.OtelCollectorName)
	return nil
}

func setupRootSync(nt *NT, rsName string) {
	repo, exist := nt.RootRepos[rsName]
	if !exist {
		nt.T.Fatal("nonexistent root repo")
	}
	// create RootSync to initialize the root reconciler.
	rs := RootSyncObjectV1Beta1(rsName, nt.GitProvider.SyncURL(repo.RemoteRepoName), nt.RootRepos[rsName].Format)
	if err := nt.Create(rs); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			nt.T.Fatal(err)
		}
	}
}

func setupRepoSync(nt *NT, nn types.NamespacedName) {
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}
	// create RepoSync to initialize the Namespace reconciler.
	rs := RepoSyncObjectV1Beta1(nn.Namespace, nn.Name, nt.GitProvider.SyncURL(repo.RemoteRepoName))
	if err := nt.Create(rs); err != nil {
		nt.T.Fatal(err)
	}
}

func waitForReconciler(nt *NT, name string) error {
	took, err := Retry(60*time.Second, func() error {
		return nt.Validate(name, configmanagement.ControllerNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
	})
	if err != nil {
		return err
	}
	nt.T.Logf("took %v to wait for %s", took, name)

	return nil
}

func repoSyncClusterRole() *rbacv1.ClusterRole {
	cr := fake.ClusterRoleObject(core.Name(clusterRoleName))
	cr.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{rbacv1.APIGroupAll},
			Resources: []string{rbacv1.ResourceAll},
			Verbs:     []string{rbacv1.VerbAll},
		},
		{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{"acm-psp"},
			Verbs:         []string{"use"},
		},
	}
	return cr
}

// repoSyncRoleBinding returns rolebinding that grants service account
// permission to manage resources in the namespace.
func repoSyncRoleBinding(nn types.NamespacedName) *rbacv1.RoleBinding {
	rb := fake.RoleBindingObject(core.Name(nn.Name), core.Namespace(nn.Namespace))
	sb := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      reconciler.NsReconcilerName(nn.Namespace, nn.Name),
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

// repoSyncClusterRoleBinding returns clusterrolebinding that grants service account
// permission to manage resources in the namespace.
func repoSyncClusterRoleBinding(nn types.NamespacedName) *rbacv1.ClusterRoleBinding {
	rb := fake.ClusterRoleBindingObject(core.Name(nn.Name + "-" + nn.Namespace))
	sb := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      reconciler.NsReconcilerName(nn.Namespace, nn.Name),
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
	if err := nt.Create(repoSyncRoleBinding(nn)); err != nil {
		nt.T.Fatal(err)
	}

	// Validate rolebinding is present.
	return nt.Validate(nn.Name, nn.Namespace, &rbacv1.RoleBinding{})
}

func revokeRepoSyncClusterRoleBinding(nt *NT, nn types.NamespacedName) {
	if err := nt.Delete(repoSyncClusterRoleBinding(nn)); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		nt.T.Fatal(err)
	}
	WaitToTerminate(nt, kinds.ClusterRoleBinding(), nn.Name+"-"+nn.Namespace, "")
}

func revokeRepoSyncNamespace(nt *NT, ns string) {
	// TODO: Ideally we can delete the namespace directly and check if it is terminated.
	// Due to b/184680603, we have to check if the namespace is in a terminating state to avoid the error:
	//   Operation cannot be fulfilled on namespaces "bookstore": The system is ensuring all content is removed from this namespace.
	//   Upon completion, this namespace will automatically be purged by the system.
	namespace := &corev1.Namespace{}
	if err := nt.Get(ns, "", namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		nt.T.Fatal(err)
	}
	if namespace.Status.Phase != corev1.NamespaceTerminating {
		if err := nt.Delete(fake.NamespaceObject(ns)); err != nil {
			if apierrors.IsNotFound(err) {
				return
			}
			nt.T.Fatal(err)
		}
	}
	WaitToTerminate(nt, kinds.Namespace(), ns, "")
}

// setReconcilerDebugMode ensures the Reconciler deployments are run in debug mode.
func setReconcilerDebugMode(t testing.NTB, obj client.Object) {
	if !IsReconcilerManagerConfigMap(obj) {
		return
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		t.Fatalf("parsed Reconciler Manager ConfigMap was %T %v", obj, obj)
	}

	key := "deployment.yaml"
	deploymentYAML, found := cm.Data[key]
	if !found {
		t.Fatal("Reconciler Manager ConfigMap has no deployment.yaml entry")
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
		t.Fatal("Unable to set debug mode for reconciler")
	}

	cm.Data[key] = strings.Join(lines, "\n")
	t.Log("Set deployment.yaml")
}

// setPollingPeriods update Reconciler Manager configmap
// reconciler-manager-cm with reconciler and hydration-controller polling
// periods to override the default.
func setPollingPeriods(t testing.NTB, obj client.Object) {
	if !IsReconcilerManagerConfigMap(obj) {
		return
	}

	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		t.Fatalf("parsed Reconciler Manager ConfigMap was not ConfigMap %T %v", obj, obj)
	}

	cm.Data[reconcilermanager.ReconcilerPollingPeriod] = reconcilerPollingPeriod.String()
	cm.Data[reconcilermanager.HydrationPollingPeriod] = hydrationPollingPeriod.String()
	t.Log("Set filesystem polling period")
}

func setupDelegatedControl(nt *NT, opts *ntopts.New) {
	// Just create one RepoSync ClusterRole, even if there are no Namespace repos.
	if err := nt.Create(repoSyncClusterRole()); err != nil {
		nt.T.Fatal(err)
	}

	for rsName := range opts.RootRepos {
		// The default RootSync is created manually, no need to set up again.
		if rsName == configsync.RootSyncName {
			continue
		}
		setupRootSync(nt, rsName)
	}

	for nn := range opts.NamespaceRepos {
		// Add a ClusterRoleBinding so that the pods can be created
		// when the cluster has PodSecurityPolicy enabled.
		// Background: If a RoleBinding (not a ClusterRoleBinding) is used,
		// it will only grant usage for pods being run in the same namespace as the binding.
		// TODO: Remove the psp related change when Kubernetes 1.25 is
		// available on GKE.
		if strings.Contains(os.Getenv("GCP_CLUSTER"), "psp") {
			if err := nt.Create(repoSyncClusterRoleBinding(nn)); err != nil {
				nt.T.Fatal(err)
			}
		}

		// create namespace for namespace reconciler.
		err := nt.Create(fake.NamespaceObject(nn.Namespace))
		if err != nil {
			nt.T.Fatal(err)
		}

		// create secret for the namespace reconciler.
		CreateNamespaceSecret(nt, nn.Namespace)

		if err := setupRepoSyncRoleBinding(nt, nn); err != nil {
			nt.T.Fatal(err)
		}

		setupRepoSync(nt, nn)
	}

	// Validate multi-repo metrics in root reconcilers.
	for rsName := range opts.RootRepos {
		rootReconciler := reconciler.RootReconcilerName(rsName)
		if err := waitForReconciler(nt, rootReconciler); err != nil {
			nt.T.Fatal(err)
		}

		err := nt.ValidateMetrics(SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMultiRepoMetrics(rootReconciler, 1)
			if err != nil {
				return err
			}
			// Validate no error metrics are emitted.
			// TODO: internal_errors_total metric from diff.go
			//return nt.ValidateErrorMetricsNotFound()
			return nil
		})
		if err != nil {
			nt.T.Errorf("validating metrics: %v", err)
		}
	}

	for nn := range opts.NamespaceRepos {
		nsReconciler := reconciler.NsReconcilerName(nn.Namespace, nn.Name)
		if err := waitForReconciler(nt, nsReconciler); err != nil {
			nt.T.Fatal(err)
		}

		// Validate multi-repo metrics in namespace reconciler.
		err := nt.ValidateMetrics(SyncMetricsToLatestCommit(nt), func() error {
			return nt.ValidateMultiRepoMetrics(nsReconciler, 0)
		})
		if err != nil {
			nt.T.Errorf("validating metrics: %v", err)
		}
	}
}

// RootSyncObjectV1Alpha1 returns the default RootSync object.
func RootSyncObjectV1Alpha1(name, repoURL string, sourceFormat filesystem.SourceFormat) *v1alpha1.RootSync {
	rs := fake.RootSyncObjectV1Alpha1(name)
	rs.Spec.SourceFormat = string(sourceFormat)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1alpha1.Git{
		Repo:   repoURL,
		Branch: MainBranch,
		Dir:    AcmeDir,
		Auth:   "ssh",
		SecretRef: v1alpha1.SecretReference{
			Name: controllers.GitCredentialVolume,
		},
	}
	return rs
}

// RootSyncObjectV1Beta1 returns the default RootSync object with version v1beta1.
func RootSyncObjectV1Beta1(name, repoURL string, sourceFormat filesystem.SourceFormat) *v1beta1.RootSync {
	rs := fake.RootSyncObjectV1Beta1(name)
	rs.Spec.SourceFormat = string(sourceFormat)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1beta1.Git{
		Repo:   repoURL,
		Branch: MainBranch,
		Dir:    AcmeDir,
		Auth:   "ssh",
		SecretRef: v1beta1.SecretReference{
			Name: controllers.GitCredentialVolume,
		},
	}
	return rs
}

// StructuredNSPath returns structured path with namespace and resourcename in repo.
func StructuredNSPath(namespace, resourceName string) string {
	return fmt.Sprintf("acme/namespaces/%s/%s.yaml", namespace, resourceName)
}

// RepoSyncObjectV1Alpha1 returns the default RepoSync object in the given namespace.
func RepoSyncObjectV1Alpha1(ns, name, repoURL string) *v1alpha1.RepoSync {
	rs := fake.RepoSyncObjectV1Alpha1(ns, name)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1alpha1.Git{
		Repo:   repoURL,
		Branch: MainBranch,
		Dir:    AcmeDir,
		Auth:   "ssh",
		SecretRef: v1alpha1.SecretReference{
			Name: "ssh-key",
		},
	}
	return rs
}

// RepoSyncObjectV1Beta1 returns the default RepoSync object
// with version v1beta1 in the given namespace.
func RepoSyncObjectV1Beta1(ns, name, repoURL string) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1(ns, name)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1beta1.Git{
		Repo:   repoURL,
		Branch: MainBranch,
		Dir:    AcmeDir,
		Auth:   "ssh",
		SecretRef: v1beta1.SecretReference{
			Name: "ssh-key",
		},
	}
	return rs
}

// setupCentralizedControl is a pure central-control mode.
// A default root repo (root-sync) manages all other root repos and namespace repos.
func setupCentralizedControl(nt *NT, opts *ntopts.New) {
	rsCount := 0
	cluster := os.Getenv("GCP_CLUSTER")
	for rsName := range opts.RootRepos {
		// The default RootSync is created manually, don't check it in the repo.
		if rsName == configsync.RootSyncName {
			continue
		}
		repo, exist := nt.RootRepos[rsName]
		if !exist {
			nt.T.Fatal("nonexistent root repo")
		}
		rs := RootSyncObjectV1Beta1(rsName, nt.GitProvider.SyncURL(repo.RemoteRepoName), nt.RootRepos[rsName].Format)
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/%s.yaml", configsync.ControllerNamespace, rsName), rs)
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding RootSync: " + rsName)
	}

	if len(opts.NamespaceRepos) > 0 {
		nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", repoSyncClusterRole())
	}
	// Use a map to record the number of RepoSync namespaces
	rsNamespaces := map[string]struct{}{}
	for nn := range opts.NamespaceRepos {
		ns := nn.Namespace
		rsCount++
		rsNamespaces[ns] = struct{}{}
		nt.RootRepos[configsync.RootSyncName].Add(StructuredNSPath(ns, ns), fake.NamespaceObject(ns))
		nt.RootRepos[configsync.RootSyncName].Add(StructuredNSPath(ns, fmt.Sprintf("rb-%s", nn.Name)), repoSyncRoleBinding(nn))
		if strings.Contains(cluster, "psp") {
			// Add a ClusterRoleBinding so that the pods can be created
			// when the cluster has PodSecurityPolicy enabled.
			// Background: If a RoleBinding (not a ClusterRoleBinding) is used,
			// it will only grant usage for pods being run in the same namespace as the binding.
			// TODO: Remove the psp related change when Kubernetes 1.25 is
			// available on GKE.
			crb := repoSyncClusterRoleBinding(nn)
			nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/cluster/crb-%s-%s.yaml", ns, nn.Name), crb)
		}

		repo, exist := nt.NonRootRepos[nn]
		if !exist {
			nt.T.Fatal("nonexistent repo")
		}
		rs := RepoSyncObjectV1Beta1(ns, nn.Name, nt.GitProvider.SyncURL(repo.RemoteRepoName))
		nt.RootRepos[configsync.RootSyncName].Add(StructuredNSPath(ns, nn.Name), rs)

		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding namespace, clusterrole, rolebinding, clusterrolebinding and RepoSync")
	}
	// This waits for the Namespace to be created.
	nt.WaitForRepoSyncs(RootSyncOnly())

	// Now that the Namespace exists, create the secret inside it, and ensure
	// its RepoSync reports everything is synced.
	for nn := range opts.NamespaceRepos {
		CreateNamespaceSecret(nt, nn.Namespace)
	}
	nt.WaitForRepoSyncs()
	for nn := range opts.NamespaceRepos {
		err := nt.Validate(nn.Name, nn.Namespace, &v1beta1.RepoSync{})
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	// Validate multi-repo metrics.
	if len(opts.NamespaceRepos) > 0 {
		err := nt.ValidateMetrics(SyncMetricsToLatestCommit(nt), func() error {
			var err error
			if strings.Contains(cluster, "psp") {
				err = nt.ValidateMultiRepoMetrics(DefaultRootReconcilerName,
					// 2 is for the `safety` namespace and the `configsync.gke.io:ns-reconciler` clusterrole, and
					// 3 is for the resources created for every namespace: RepoSync, RoleBinding, ClusterRoleBinding
					2+len(rsNamespaces)+rsCount*3,
					testmetrics.ResourceCreated("Namespace"), testmetrics.ResourceCreated("ClusterRole"),
					testmetrics.ResourceCreated("RoleBinding"), testmetrics.ResourceCreated("RepoSync"),
					testmetrics.ResourceCreated("ClusterRoleBinding"))
			} else {
				err = nt.ValidateMultiRepoMetrics(DefaultRootReconcilerName,
					// 2 is for the `safety` namespace and the `configsync.gke.io:ns-reconciler` clusterrole,
					// and 2 is for the resources created for every namespace: RepoSync and RoleBinding
					2+len(rsNamespaces)+rsCount*2,
					testmetrics.ResourceCreated("Namespace"), testmetrics.ResourceCreated("ClusterRole"),
					testmetrics.ResourceCreated("RoleBinding"), testmetrics.ResourceCreated("RepoSync"))
			}
			if err != nil {
				return err
			}
			// Validate no error metrics are emitted.
			// TODO: unexpected resource_conflicts_total metric from remediator
			//return nt.ValidateErrorMetricsNotFound()
			return nil
		})
		if err != nil {
			nt.T.Errorf("validating metrics: %v", err)
		}
	}
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
// It also checks the reconcilers if the pod is a reconcielr-manager with multi-repo support.
func NewPodReady(nt *NT, labelName, currentLabel, childLabel string, oldCurrentPods, oldChildPods []corev1.Pod) error {
	if len(oldCurrentPods) == 0 {
		return nil
	}
	newPods := &corev1.PodList{}
	if err := nt.List(newPods, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{labelName: currentLabel}); err != nil {
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
func DeletePodByLabel(nt *NT, label, value string) {
	oldPods := &corev1.PodList{}
	if err := nt.List(oldPods, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: value}); err != nil {
		nt.T.Fatal(err)
	}
	oldReconcilers := &corev1.PodList{}
	if value == reconcilermanager.ManagerName {
		if err := nt.List(oldReconcilers, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: reconcilermanager.Reconciler}); err != nil {
			nt.T.Fatal(err)
		}
	}
	if err := nt.DeleteAllOf(&corev1.Pod{}, client.InNamespace(configmanagement.ControllerNamespace), client.MatchingLabels{label: value}); err != nil {
		nt.T.Fatalf("Pod delete failed: %v", err)
	}
	Wait(nt.T, "new pods come up", nt.DefaultWaitTimeout, func() error {
		if value == reconcilermanager.ManagerName {
			return NewPodReady(nt, label, value, reconcilermanager.Reconciler, oldPods.Items, oldReconcilers.Items)
		}
		return NewPodReady(nt, label, value, "", oldPods.Items, nil)
	}, WaitTimeout(nt.DefaultWaitTimeout))
}

// ResetMonoRepoSpec sets the mono repo's SOURCE_FORMAT and POLICY_DIR. It might cause the git-importer to restart.
func ResetMonoRepoSpec(nt *NT, sourceFormat filesystem.SourceFormat, policyDir string) {
	restartPod := false

	importerCM := &corev1.ConfigMap{}
	if err := nt.Get("importer", configmanagement.ControllerNamespace, importerCM); err != nil {
		nt.T.Fatal(err)
	}
	if importerCM.Data["POLICY_DIR"] != policyDir {
		restartPod = true
		nt.MustMergePatch(importerCM, fmt.Sprintf(`{"data":{"POLICY_DIR":"%s"}}`, policyDir))
	}

	sourceFormatCM := &corev1.ConfigMap{}
	if err := nt.Get("source-format", configmanagement.ControllerNamespace, sourceFormatCM); err != nil {
		nt.T.Fatal(err)
	}
	if sourceFormatCM.Data["SOURCE_FORMAT"] != string(sourceFormat) {
		restartPod = true
		nt.MustMergePatch(sourceFormatCM, fmt.Sprintf(`{"data":{"SOURCE_FORMAT":"%s"}}`, sourceFormat))
	}

	if restartPod {
		DeletePodByLabel(nt, "app", filesystem.GitImporterName)
		DeletePodByLabel(nt, "app", "monitor")
	}
}

// resetRepository re-initializes an existing remote repository or creates a new remote repository.
func resetRepository(nt *NT, repoType RepoType, nn types.NamespacedName, upstream string, sourceFormat filesystem.SourceFormat) *Repository {
	if repo, found := nt.RemoteRepositories[nn]; found {
		repo.ReInit(nt, sourceFormat)
		return repo
	}
	repo := NewRepository(nt, repoType, nn, upstream, sourceFormat)
	return repo
}

func resetRootRepo(nt *NT, rsName, upstream string, sourceFormat filesystem.SourceFormat) {
	rs := fake.RootSyncObjectV1Beta1(rsName)
	if err := nt.Get(rs.Name, rs.Namespace, rs); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Fatal(err)
	} else {
		nt.RootRepos[rsName] = resetRepository(nt, RootRepo, RootSyncNN(rsName), upstream, sourceFormat)
		if err == nil {
			nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "%s", "git": {"dir": "%s"}}}`, sourceFormat, AcmeDir))
		} else {
			rs.Spec.SourceFormat = string(sourceFormat)
			rs.Spec.Git = &v1beta1.Git{
				Repo:      nt.GitProvider.SyncURL(nt.RootRepos[rsName].RemoteRepoName),
				Branch:    MainBranch,
				Dir:       AcmeDir,
				Auth:      "ssh",
				SecretRef: v1beta1.SecretReference{Name: controllers.GitCredentialVolume},
			}
			if err = nt.Create(rs); err != nil {
				nt.T.Fatal(err)
			}
		}
	}
}

// resetRootRepos resets all RootSync objects to the initial commit to delete managed testing resources.
// For the default RootSync, it sets its SOURCE_FORMAT and POLICY_DIR. It might cause the root-reconciler to restart.
// It sets POLICY_DIR to always be `acme` because the initial root-repo's sync directory is configured to be `acme`.
func resetRootRepos(nt *NT, upstream string, sourceFormat filesystem.SourceFormat) {
	// Reset the default root repo first so that other managed RootSyncs can be re-created and initialized.
	rootRepo := nt.RootRepos[configsync.RootSyncName]
	// Update the sourceFormat as the test case might use a different sourceFormat
	// than the one that is created initially.
	rootRepo.Format = sourceFormat
	resetRootRepo(nt, configsync.RootSyncName, upstream, sourceFormat)
	nt.WaitForSync(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		nt.DefaultWaitTimeout, DefaultRootSha1Fn, RootSyncHasStatusSyncCommit, nil)

	for name := range nt.RootRepos {
		if name != configsync.RootSyncName {
			resetRootRepo(nt, name, nt.RootRepos[name].UpstreamRepoURL, nt.RootRepos[name].Format)
			nt.WaitForSync(kinds.RootSyncV1Beta1(), name, configsync.ControllerNamespace,
				nt.DefaultWaitTimeout, DefaultRootSha1Fn, RootSyncHasStatusSyncCommit, nil)
			// Now the repo is back to the initial commit, we can delete the safety check namespace
			nt.RootRepos[name].Remove(nt.RootRepos[name].SafetyNSPath)
			nt.RootRepos[name].CommitAndPush("delete the safety check namespace")
			nt.WaitForSync(kinds.RootSyncV1Beta1(), name, configsync.ControllerNamespace,
				nt.DefaultWaitTimeout, DefaultRootSha1Fn, RootSyncHasStatusSyncCommit, nil)
		}
	}
}

// resetNamespaceRepos sets the namespace repo to the initial state. That should delete all resources in the namespace.
func resetNamespaceRepos(nt *NT) {
	namespaceRepos := &v1beta1.RepoSyncList{}
	if err := nt.List(namespaceRepos); err != nil {
		nt.T.Fatal(err)
	}
	for _, nr := range namespaceRepos.Items {
		// reset the namespace repo only when it is in 'nt.NonRootRepos' (created by test).
		// This prevents from resetting an existing namespace repo from a remote git provider.
		nn := RepoSyncNN(nr.Namespace, nr.Name)
		if r, found := nt.NonRootRepos[nn]; found {
			nt.NonRootRepos[nn] = resetRepository(nt, NamespaceRepo, nn, r.UpstreamRepoURL, filesystem.SourceFormatUnstructured)
			rs := &v1beta1.RepoSync{}
			if err := nt.Get(nn.Name, nn.Namespace, rs); err != nil {
				if apierrors.IsNotFound(err) {
					// The RepoSync might be declared in other namespace repos and get pruned in the reset process.
					// If that happens, re-create the object to clean up the managed testing resources.
					rs := RepoSyncObjectV1Beta1(nn.Namespace, nn.Name, nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName))
					if err := nt.Create(rs); err != nil {
						nt.T.Fatal(err)
					}
				} else {
					nt.T.Fatal(err)
				}
			}
			nt.WaitForSync(kinds.RepoSyncV1Beta1(), nr.Name, nr.Namespace,
				nt.DefaultWaitTimeout, DefaultRepoSha1Fn(), RepoSyncHasStatusSyncCommit, nil)
		}
	}
}

// deleteRootRepos deletes RootSync objects except for the default one (root-sync).
// It also deletes the safety check namespace managed by the root repo.
func deleteRootRepos(nt *NT) {
	rootSyncs := &v1beta1.RootSyncList{}
	if err := nt.List(rootSyncs); err != nil {
		nt.T.Fatal(err)
	}
	for _, rs := range rootSyncs.Items {
		// Keep the default RootSync object
		if rs.Name == configsync.RootSyncName {
			continue
		}
		if err := nt.Delete(&rs); err != nil {
			nt.T.Fatal(err)
		}
		WaitToTerminate(nt, kinds.Deployment(), reconciler.RootReconcilerName(rs.Name), rs.Namespace)
		WaitToTerminate(nt, kinds.RootSyncV1Beta1(), rs.Name, rs.Namespace)
	}
}

// deleteNamespaceRepos deletes the repo-sync and the namespace.
func deleteNamespaceRepos(nt *NT) {
	repoSyncs := &v1beta1.RepoSyncList{}
	if err := nt.List(repoSyncs); err != nil {
		nt.T.Fatal(err)
	}

	for _, rs := range repoSyncs.Items {
		// revokeRepoSyncNamespace will delete the namespace of RepoSync, which
		// auto-deletes the resources, including RepoSync, Deployment, RoleBinding, Secret, and etc.
		revokeRepoSyncNamespace(nt, rs.Namespace)
		nn := RepoSyncNN(rs.Namespace, rs.Name)
		if strings.Contains(os.Getenv("GCP_CLUSTER"), "psp") {
			revokeRepoSyncClusterRoleBinding(nt, nn)
		}
	}

	rsClusterRole := repoSyncClusterRole()
	if err := nt.Delete(rsClusterRole); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Fatal(err)
	}
	WaitToTerminate(nt, kinds.ClusterRole(), rsClusterRole.Name, "")
}

// SetPolicyDir updates the root-sync object with the provided policyDir.
func SetPolicyDir(nt *NT, name, policyDir string) {
	nt.T.Logf("Set policyDir to %q", policyDir)
	if nt.MultiRepo {
		rs := fake.RootSyncObjectV1Beta1(name)
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"dir": "%s"}}}`, policyDir))
	} else {
		ResetMonoRepoSpec(nt, filesystem.SourceFormatHierarchy, policyDir)
	}
}
