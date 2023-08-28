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

package bugreport

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/policycontroller"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corev1Client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/cmd/nomos/status"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/cmd/nomos/version"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/client/restconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

type coreClient interface {
	CoreV1() corev1Client.CoreV1Interface
}

func operatorLabelSelectorOrDie() labels.Requirement {
	ret, err := labels.NewRequirement("k8s-app", selection.Equals, []string{"config-management-operator"})
	if err != nil {
		panic(err)
	}
	return *ret
}

// Filepath for bugreport directory
const (
	Namespace    = "namespaces"
	ClusterScope = "cluster"
	Raw          = "raw"
	Processed    = "processed"
)

// BugReporter handles basic data gathering tasks for generating a
// bug report
type BugReporter struct {
	client    client.Reader
	clientSet *kubernetes.Clientset
	cm        *unstructured.Unstructured
	enabled   map[Product]bool
	util.ConfigManagementClient
	k8sContext string
	// report file
	outFile       *os.File
	writer        *zip.Writer
	name          string
	ErrorList     []error
	WritingErrors []error
}

// New creates a new BugReport
func New(ctx context.Context, cfg *rest.Config) (*BugReporter, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{})
	if err != nil {
		return nil, err
	}
	cm := &unstructured.Unstructured{}
	cm.SetGroupVersionKind(schema.GroupVersionKind{
		Group: configmanagement.GroupName,
		// we rely on a specific version implicitly later in the code, so this should
		// be hardcoded
		Version: "v1",
		Kind:    configmanagement.OperatorKind,
	})
	var errorList []error
	currentk8sContext, err := restconfig.CurrentContextName()
	if err != nil {
		errorList = append(errorList, err)
	}

	if err := c.Get(ctx, types.NamespacedName{Name: util.ConfigManagementName}, cm); err != nil {
		if meta.IsNoMatchError(err) {
			fmt.Println("kind <<" + configmanagement.OperatorKind + ">> is not registered with the cluster")
		} else if errors.IsNotFound(err) {
			fmt.Println("ConfigManagement object not found")
		} else {
			errorList = append(errorList, err)
		}
	}

	isMulti, _, err := unstructured.NestedBool(cm.UnstructuredContent(), "spec", "enableMultiRepo")
	if err != nil {
		fmt.Println("ConfigManagement parsing error", err)
	}
	if !isMulti {
		util.MonoRepoNotice(os.Stdout, currentk8sContext)
	}

	return &BugReporter{
		client:        c,
		clientSet:     cs,
		cm:            cm,
		k8sContext:    currentk8sContext,
		ErrorList:     errorList,
		WritingErrors: []error{},
	}, nil
}

// EnabledServices returns the set of services that are enabled
func (b *BugReporter) EnabledServices() map[Product]bool {
	if b.enabled == nil {
		enabled := make(map[Product]bool)

		// We can safely ignore errors here, because if this request doesn't succeed,
		// Policy Controller is not enabled
		enabled[PolicyController], _, _ = unstructured.NestedBool(b.cm.Object, "spec", "policyController", "enabled")
		// Same for Config Sync, though here the "disabled" condition is if enableMultiRepo is true or if the git
		// config is "empty", which involves looking for an empty proxy config
		configSyncEnabled := false
		enableMultiRepo, _, _ := unstructured.NestedBool(b.cm.Object, "spec", "enableMultiRepo")
		if enableMultiRepo {
			configSyncEnabled = true
		} else {
			syncGitCfg, _, _ := unstructured.NestedMap(b.cm.Object, "spec", "git")
			for k := range syncGitCfg {
				if k != "proxy" {
					configSyncEnabled = true
				}
			}
			proxy, _, _ := unstructured.NestedMap(syncGitCfg, "proxy")
			if len(proxy) > 0 {
				configSyncEnabled = true
			}
		}

		enabled[ConfigSync] = configSyncEnabled
		enabled[ResourceGroup] = enableMultiRepo
		enabled[ConfigSyncMonitoring] = true
		b.enabled = enabled
	}

	return b.enabled
}

// FetchLogSources provides a set of Readables for all of nomos' container logs
// TODO: Still need to figure out a good way to test this
func (b *BugReporter) FetchLogSources(ctx context.Context) []Readable {
	var toBeLogged logSources

	// for each namespace, generate a list of logSources
	listOps := client.ListOptions{LabelSelector: labels.NewSelector().Add(operatorLabelSelectorOrDie())}
	sources, err := b.logSourcesForNamespace(ctx, metav1.NamespaceSystem, listOps, nil)
	if err != nil {
		b.ErrorList = append(b.ErrorList, err)
	} else {
		toBeLogged = append(toBeLogged, sources...)
	}

	listOps = client.ListOptions{}
	nsLabels := map[string]string{"configmanagement.gke.io/configmanagement": "config-management"}
	productAndLabels := map[Product]map[string]string{
		PolicyController:     nsLabels,
		ResourceGroup:        nsLabels,
		ConfigSyncMonitoring: nil,
		ConfigSync:           nil,
	}
	for product, ls := range productAndLabels {
		sources, err = b.logSourcesForProduct(ctx, product, listOps, ls)
		if err != nil {
			b.ErrorList = append(b.ErrorList, err)
		} else {
			toBeLogged = append(toBeLogged, sources...)
		}
	}

	// If we don't have any logs to pull down, report errors and exit
	if len(toBeLogged) == 0 {
		return nil
	}

	// Convert logSources to Readables
	toBeRead, errs := toBeLogged.convertLogSourcesToReadables(ctx, b.clientSet)
	b.ErrorList = append(b.ErrorList, errs...)

	return toBeRead
}

func (b *BugReporter) logSourcesForProduct(ctx context.Context, product Product, listOps client.ListOptions, nsLabels map[string]string) (logSources, error) {
	enabled := b.EnabledServices()

	ls, err := b.logSourcesForNamespace(ctx, productNamespaces[product], listOps, nsLabels)
	if err != nil {
		switch {
		case errorIs(err, missingNamespace) && !enabled[product]:
			klog.Infof("%s is not enabled", string(product))
			return nil, nil
		case errorIs(err, notManagedByACM) && !enabled[product]:
			klog.Infof("%s is not managed by ACM", string(product))
			return nil, nil
		case errorIs(err, notManagedByACM) && enabled[product]:
			klog.Errorf("%s is not managed by ACM, but it should be", string(product))
			return nil, err
		default:
			return nil, err
		}
	}
	if !enabled[product] {
		if len(ls) == 0 {
			klog.Infof("%s is not enabled", string(product))
		} else {
			klog.Infof("%s is not enabled but log sources found. It may be in the process of uninstalling. Adding logs to report.", string(product))
		}
	}
	return ls, err
}

func (b *BugReporter) logSourcesForNamespace(ctx context.Context, name string, listOps client.ListOptions, nsLabels map[string]string) (logSources, error) {
	fmt.Println("Retrieving " + name + " logs")
	ns, err := b.fetchNamespace(ctx, name, nsLabels)
	if err != nil {
		return nil, wrap(err, "failed to retrieve namespace %v", name)
	}

	pods, err := b.listPods(ctx, *ns, listOps)
	if err != nil {
		return nil, wrap(err, "failed to list pods for namespace %v", name)
	}

	return assembleLogSources(*ns, *pods), nil
}

func (b *BugReporter) fetchNamespace(ctx context.Context, name string, nsLabels map[string]string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := b.client.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, newMissingNamespaceError(name)
		}
		return nil, fmt.Errorf("failed to get namespace with name=%v", name)
	}
	for k, v := range nsLabels {
		val, ok := ns.GetLabels()[k]
		if !ok || val != v {
			return nil, newNotManagedNamespaceError(name)
		}
	}
	return ns, nil
}

func (b *BugReporter) listPods(ctx context.Context, ns corev1.Namespace, lOps client.ListOptions) (*corev1.PodList, error) {
	pods := &corev1.PodList{}
	lOps.Namespace = ns.Name
	err := b.client.List(ctx, pods, &lOps)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve pods for namespace %v", ns.Name)
	}

	return pods, nil
}

func assembleLogSources(ns corev1.Namespace, pods corev1.PodList) logSources {
	var ls logSources
	for _, p := range pods.Items {
		for _, c := range p.Spec.Containers {
			ls = append(ls, &logSource{
				ns:   ns,
				pod:  p,
				cont: c,
			})
		}
	}

	return ls
}

// resourcesToReadables is a type of function that converts the resources to readables.
type resourcesToReadables func(*unstructured.UnstructuredList, string) []Readable

// fetchResources provides a set of Readables for resources with a given group and version
// toReadables: the function that converts the resources to readables.
func (b *BugReporter) fetchResources(ctx context.Context, gv schema.GroupVersion, toReadables resourcesToReadables) (rd []Readable) {
	rl, err := b.clientSet.ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("No %s resources found on cluster\n", gv.Group)
			return rd
		}
		if meta.IsNoMatchError(err) {
			fmt.Println("No match for " + gv.String())
			return rd
		}
		b.ErrorList = append(b.ErrorList, fmt.Errorf("failed to list server %s resources: %v", gv.Group, err))
		return rd
	}
	for _, apiResource := range rl.APIResources {
		// Check for empty singular name to skip subresources
		if apiResource.SingularName != "" {
			u := &unstructured.UnstructuredList{}
			u.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   gv.Group,
				Kind:    apiResource.SingularName,
				Version: gv.Version,
			})
			if err := b.client.List(ctx, u); err != nil {
				b.ErrorList = append(b.ErrorList, fmt.Errorf("failed to list %s resources: %v", apiResource.SingularName, err))
			} else {
				rd = append(rd, toReadables(u, apiResource.Name)...)
			}
		}
	}
	return rd
}

// fetchConfigMaps provides a set of Readables for the ConfigMap objects under the config-management-system namespace
func (b *BugReporter) fetchConfigMaps(ctx context.Context) (rd []Readable) {
	redactKeys := []string{"HTTPS_PROXY", "HTTP_PROXY"}
	ns := configmanagement.ControllerNamespace
	configMapList, err := b.clientSet.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		b.ErrorList = append(b.ErrorList, fmt.Errorf("failed to list %s configmaps: %v", ns, err))
	} else {
		for _, cm := range configMapList.Items {
			if strings.HasSuffix(cm.Name, "git-sync") {
				for _, k := range redactKeys {
					if _, ok := cm.Data[k]; ok {
						cm.Data[k] = "<REDACTED>"
					}
				}
			}
		}
		filePath := path.Join(Namespace, ns, "ConfigMaps")
		rd = b.appendPrettyJSON(rd, filePath, configMapList)
	}
	return rd
}

// fetchConfigSyncWebhookConfig provides a set of Readables for the ValidatingWebhookConfiguration object with the name of admission-webhook.configsync.gke.io
func (b *BugReporter) fetchConfigSyncWebhookConfig(ctx context.Context) (rd []Readable) {
	name := "admission-webhook.configsync.gke.io"
	webhook, err := b.clientSet.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			b.ErrorList = append(b.ErrorList, fmt.Errorf("failed to get the %q ValidatingWebhookConfiguration object: %v", name, err))
		}
	} else {
		filePath := pathToClusterCmList("config-sync-validating-webhhook-configuration")
		rd = b.appendPrettyJSON(rd, filePath, webhook)
	}
	return rd
}

// FetchResources provides a set of Readables for configsync, configmanagement and resourcegroup resources.
func (b *BugReporter) FetchResources(ctx context.Context) []Readable {
	var rd []Readable
	var clusterResourceToReadables = func(u *unstructured.UnstructuredList, resourceName string) (r []Readable) {
		r = b.appendPrettyJSON(r, pathToClusterCmList(resourceName), u)
		return r
	}

	var namespacedResourceToReadables = func(u *unstructured.UnstructuredList, _ string) (r []Readable) {
		for _, o := range u.Items {
			r = b.appendPrettyJSON(r, pathToNamespacedResource(o.GetNamespace(), o.GetKind(), o.GetName()), o)
		}
		return r
	}

	// fetch cluster-scoped configmanagement resources
	cmReadables := b.fetchResources(ctx, v1.SchemeGroupVersion, clusterResourceToReadables)
	rd = append(rd, cmReadables...)

	namespaceGVs := []schema.GroupVersion{
		v1beta1.SchemeGroupVersion,           // namespace-scoped configsync resources
		live.ResourceGroupGVK.GroupVersion(), // namespace-scoped resourcegroup resources
	}
	for _, gv := range namespaceGVs {
		readables := b.fetchResources(ctx, gv, namespacedResourceToReadables)
		rd = append(rd, readables...)
	}

	readables := b.fetchConfigMaps(ctx)
	rd = append(rd, readables...)

	readables = b.fetchConfigSyncWebhookConfig(ctx)
	rd = append(rd, readables...)
	return rd
}

func pathToNamespacedResource(namespace, kind, name string) string {
	return path.Join(Namespace, namespace, kind+"-"+name)
}

func pathToClusterCmList(name string) string {
	return path.Join(ClusterScope, "configmanagement", name)
}

// FetchCMSystemPods provides a Readable for pods in the config-management-system,
// kube-system, resource-group-system and config-management-monitoring namespaces.
func (b *BugReporter) FetchCMSystemPods(ctx context.Context) (rd []Readable) {
	var namespaces = []string{
		configmanagement.ControllerNamespace,
		metav1.NamespaceSystem,
		configmanagement.RGControllerNamespace,
		metrics.MonitoringNamespace,
		policycontroller.NamespaceSystem,
	}

	for _, ns := range namespaces {
		podList, err := b.clientSet.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			b.ErrorList = append(b.ErrorList, fmt.Errorf("failed to list %s pods: %v", ns, err))
		} else {
			rd = b.appendPrettyJSON(rd, pathToNamespacePodList(ns), podList)
		}
	}

	return rd
}

func pathToNamespacePodList(ns string) string {
	return path.Join(Namespace, ns, "pods")
}

// AddNomosStatusToZip writes `nomos status` to bugreport zip file
func (b *BugReporter) AddNomosStatusToZip(ctx context.Context) {
	tmpFile, err := status.SaveToTempFile(ctx, []string{b.k8sContext})
	defer func() {
		if tmpFile != nil {
			if err = os.Remove(tmpFile.Name()); err != nil && !os.IsNotExist(err) {
				klog.Errorf("failed to remove the temporary file %q: %v", tmpFile.Name(), err)
			}
		}
	}()
	if err != nil {
		b.ErrorList = append(b.ErrorList, err)
	} else if err = b.copyToZip(tmpFile, path.Join(Processed, b.k8sContext, "status.txt")); err != nil {
		b.WritingErrors = append(b.WritingErrors, err)
	}
}

// copyToZip copies the content from the input file to the zip file.
func (b *BugReporter) copyToZip(inputFile *os.File, zipFileName string) error {
	baseName := filepath.Base(b.name)
	dirName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	fileName := filepath.FromSlash(filepath.Join(dirName, zipFileName))
	zipFile, err := b.writer.Create(fileName)
	if err != nil {
		e := fmt.Errorf("failed to create file %v inside zip: %v", fileName, err)
		return e
	}
	if _, err = io.Copy(zipFile, inputFile); err != nil {
		return fmt.Errorf("failed to copy file %s to zip %s: %v", inputFile.Name(), fileName, err)
	}
	if err = inputFile.Close(); err != nil {
		klog.Errorf("failed to close file %s: %v", inputFile.Name(), err)
	}

	fmt.Println("Wrote file " + fileName)
	return nil
}

// AddNomosVersionToZip writes `nomos version` to bugreport zip file
func (b *BugReporter) AddNomosVersionToZip(ctx context.Context) {
	if versionRc, err := version.GetVersionReadCloser(ctx, []string{b.k8sContext}); err != nil {
		b.ErrorList = append(b.ErrorList, err)
	} else if err = b.writeReadableToZip(Readable{
		Name:       path.Join(Processed, b.k8sContext, "version.txt"),
		ReadCloser: versionRc,
	}); err != nil {
		b.WritingErrors = append(b.WritingErrors, err)
	}
}

func getReportName() string {
	now := time.Now()
	baseName := fmt.Sprintf("bug_report_%v.zip", now.Unix())
	nameWithPath, err := filepath.Abs(baseName)
	if err != nil {
		nameWithPath = baseName
	}

	return nameWithPath
}

func (b *BugReporter) writeReadableToZip(readable Readable) error {
	baseName := filepath.Base(b.name)
	dirName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	fileName := filepath.FromSlash(filepath.Join(dirName, readable.Name))
	f, err := b.writer.Create(fileName)
	if err != nil {
		e := fmt.Errorf("failed to create file %v inside zip: %v", fileName, err)
		return e
	}

	w := bufio.NewWriter(f)
	_, err = w.ReadFrom(readable.ReadCloser)
	if err != nil {
		e := fmt.Errorf("failed to write file %v to zip: %v", fileName, err)
		return e
	}

	err = w.Flush()
	if err != nil {
		e := fmt.Errorf("failed to flush writer to zip for file %v:i %v", fileName, err)
		return e
	}

	fmt.Println("Wrote file " + fileName)

	return nil
}

// WriteRawInZip writes raw kubernetes resource to bugreport zip file
func (b *BugReporter) WriteRawInZip(toBeRead []Readable) {

	for _, readable := range toBeRead {
		readable.Name = path.Join(Raw, b.k8sContext, readable.Name)
		err := b.writeReadableToZip(readable)
		if err != nil {
			b.WritingErrors = append(b.WritingErrors, err)
		}
	}

}

// Close closes all file streams
func (b *BugReporter) Close() {

	err := b.writer.Close()
	if err != nil {
		e := fmt.Errorf("failed to close zip writer: %v", err)
		b.ErrorList = append(b.ErrorList, e)
	}

	err = b.outFile.Close()
	if err != nil {
		e := fmt.Errorf("failed to close zip file: %v", err)
		b.ErrorList = append(b.ErrorList, e)
	}

	if len(b.WritingErrors) == 0 {
		klog.Infof("Bug report written to zip file: %v\n", b.name)
	} else {
		klog.Warningf("Some errors returned while writing zip file.  May exist at: %v\n", b.name)
	}
	b.ErrorList = append(b.ErrorList, b.WritingErrors...)

	if len(b.ErrorList) > 0 {
		for _, e := range b.ErrorList {
			klog.Errorf("Error: %v\n", e)
		}

		klog.Errorf("Partial bug report may have succeeded.  Look for file: %s\n", b.name)
	} else {
		fmt.Println("Created file " + b.name)
	}
}

// Open initializes bugreport zip files
func (b *BugReporter) Open() (err error) {
	b.name = getReportName()
	if b.outFile, err = os.Create(b.name); err != nil {
		return fmt.Errorf("failed to create file %v: %v", b.name, err)
	}
	b.writer = zip.NewWriter(b.outFile)
	return nil
}

func (b *BugReporter) appendPrettyJSON(rd []Readable, pathName string, object interface{}) []Readable {
	if data, err := json.MarshalIndent(object, "", "  "); err != nil {
		b.ErrorList = append(b.ErrorList, fmt.Errorf("invalid json response from resources %s: %v", pathName, err))
	} else {
		rd = append(rd, Readable{
			ReadCloser: ioutil.NopCloser(bytes.NewReader(data)),
			Name:       fmt.Sprintf("%s.json", pathName),
		})
	}
	// also write to yaml for easier manual readability
	if data, err := yaml.Marshal(object); err != nil {
		b.ErrorList = append(b.ErrorList, fmt.Errorf("invalid yaml response from resources %s: %v", pathName, err))
	} else {
		rd = append(rd, Readable{
			ReadCloser: ioutil.NopCloser(bytes.NewReader(data)),
			Name:       fmt.Sprintf("%s.yaml", pathName),
		})
	}
	return rd
}
