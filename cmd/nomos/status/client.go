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

// Package status contains logic for the nomos status CLI command.
package status

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/generated/clientset/versioned"
	typedv1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	// ACMOperatorLabelSelector is the label selector for the ACM operator Pod.
	ACMOperatorLabelSelector = "k8s-app=config-management-operator"
	// ACMOperatorDeployment is the name of the ACM operator Deployment.

	syncingConditionSupportedVersion = "v1.10.0-rc.1"
)

// ClusterClient is the client that talks to the cluster.
type ClusterClient struct {
	// Client performs CRUD operations on Kubernetes objects.
	Client client.Client
	repos  typedv1.RepoInterface
	// K8sClient contains the clients for groups.
	K8sClient        *kubernetes.Clientset
	ConfigManagement *util.ConfigManagementClient
}

func (c *ClusterClient) rootSyncs(ctx context.Context) ([]*v1beta1.RootSync, []types.NamespacedName, error) {
	rsl := &v1beta1.RootSyncList{}
	if err := c.Client.List(ctx, rsl); err != nil {
		return nil, nil, err
	}
	var rootSyncs []*v1beta1.RootSync
	var rootSyncNsAndNames []types.NamespacedName
	for _, rs := range rsl.Items {
		// Use local copy of the iteration variable to correctly get the value in
		// each iteration and avoid the last value getting overwritten.
		localRS := rs
		rootSyncs = append(rootSyncs, &localRS)
		rootSyncNsAndNames = append(rootSyncNsAndNames, types.NamespacedName{
			Namespace: rs.Namespace,
			Name:      rs.Name,
		})
	}
	return rootSyncs, rootSyncNsAndNames, nil
}

func (c *ClusterClient) repoSyncs(ctx context.Context, ns string) ([]*v1beta1.RepoSync, []types.NamespacedName, error) {
	rsl := &v1beta1.RepoSyncList{}
	if ns == "" {
		if err := c.Client.List(ctx, rsl); err != nil {
			return nil, nil, err
		}
	} else {
		if err := c.Client.List(ctx, rsl, client.InNamespace(ns)); err != nil {
			return nil, nil, err
		}
	}

	var repoSyncs []*v1beta1.RepoSync
	var repoSyncNsAndNames []types.NamespacedName
	for _, rs := range rsl.Items {
		// Use local copy of the iteration variable to correctly get the value in
		// each iteration and avoid the last value getting overwritten.
		localRS := rs
		repoSyncs = append(repoSyncs, &localRS)
		repoSyncNsAndNames = append(repoSyncNsAndNames, types.NamespacedName{
			Namespace: rs.Namespace,
			Name:      rs.Name,
		})
	}
	return repoSyncs, repoSyncNsAndNames, nil
}

func (c *ClusterClient) resourceGroups(ctx context.Context, ns string, nsAndNames []types.NamespacedName) ([]*unstructured.Unstructured, error) {
	rgl := kinds.NewUnstructuredListForItemGVK(v1alpha1.SchemeGroupVersionKind())
	if ns == "" {
		if err := c.Client.List(ctx, rgl); err != nil {
			return nil, err
		}
	} else {
		if err := c.Client.List(ctx, rgl, client.InNamespace(ns)); err != nil {
			return nil, err
		}
	}

	var resourceGroups []*unstructured.Unstructured
	for _, rg := range rgl.Items {
		localRG := rg
		resourceGroups = append(resourceGroups, &localRG)
	}
	return consistentOrder(nsAndNames, resourceGroups), nil
}

// clusterStatus returns the ClusterState for the cluster this client is connected to.
func (c *ClusterClient) clusterStatus(ctx context.Context, cluster, namespace string) *ClusterState {
	cs := &ClusterState{Ref: cluster}
	isOss, err := util.IsOssInstallation(ctx, c.ConfigManagement, c.Client, c.K8sClient)
	if err != nil {
		cs.Error = err.Error()
		return cs
	}

	if !isOss {
		cs.isMulti, err = c.ConfigManagement.IsMultiRepo(ctx)

		if !c.IsInstalled(ctx, cs) {
			return cs
		}
		if !c.IsConfigured(ctx, cs) {
			return cs
		}

		if err != nil {
			cs.status = util.ErrorMsg
			cs.Error = err.Error()
			return cs
		}
	}

	if namespace == configsync.ControllerNamespace {
		cs.Error = c.rootRepoClusterStatus(ctx, cs)
	} else if namespace != "" {
		cs.Error = c.namespaceRepoClusterStatus(ctx, cs, namespace)
	} else if isOss || (cs.isMulti != nil && *cs.isMulti) {
		c.multiRepoClusterStatus(ctx, cs)
	}
	return cs
}

// syncingConditionSupported checks if the ACM version is v1.9.2 or later, which
// has the high-level syncing condition.
func (c *ClusterClient) syncingConditionSupported(ctx context.Context) bool {
	v, err := c.ConfigManagement.Version(ctx)
	if err != nil {
		return false
	}
	supportedVersion := semver.MustParse(syncingConditionSupportedVersion)
	version, err := semver.NewVersion(v)
	if err != nil {
		return false
	}
	return !version.LessThan(supportedVersion)
}

// multiRepoClusterStatus populates the given ClusterState with the sync status of
// the multi repos on the ClusterClient's cluster.
func (c *ClusterClient) multiRepoClusterStatus(ctx context.Context, cs *ClusterState) {
	// Get the status of all RootSyncs
	rootErr := c.rootRepoClusterStatus(ctx, cs)

	// Get the status of all RepoSyncs
	repoErr := c.namespaceRepoClusterStatus(ctx, cs, "")
	if len(rootErr) > 0 {
		cs.Error = fmt.Sprintf("Root repo error: %s", rootErr)
	}
	if len(repoErr) > 0 {
		if len(cs.Error) > 0 {
			cs.Error += ", "
		}
		cs.Error += fmt.Sprintf("Namespace repo error: %s", repoErr)
	}
}

// rootRepoClusterStatus populates the given ClusterState with the sync status of
// config-management-system namespace
func (c *ClusterClient) rootRepoClusterStatus(ctx context.Context, cs *ClusterState) (errorMsg string) {
	var errs []string
	var rootErr string
	syncingConditionSupported := c.syncingConditionSupported(ctx)

	// Get the status of all RootSyncs
	var rootRGs []*unstructured.Unstructured
	rootSyncs, rootSyncNsAndNames, err := c.rootSyncs(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		rootRGs, err = c.resourceGroups(ctx, configsync.ControllerNamespace, rootSyncNsAndNames)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(rootSyncs) != len(rootRGs) {
		errs = append(errs, fmt.Sprintf("expected the number of RootSyncs and ResourceGroups to be equal, but found %d RootSyncs and %d ResourceGroups", len(rootSyncs), len(rootRGs)))
	} else if len(rootSyncs) != 0 {
		var repos []*RepoState
		for i, rs := range rootSyncs {
			rg := rootRGs[i]
			if rg == nil {
				// We always expect a ResourceGroup, even if we have no managed resources.
				errs = append(errs, rgNotFoundErrMsg(rootSyncNsAndNames[i].Name, rootSyncNsAndNames[i].Namespace))
			}
			repos = append(repos, RootRepoStatus(rs, rg, syncingConditionSupported))
		}
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].scope < repos[j].scope || (repos[i].scope == repos[j].scope && repos[i].syncName < repos[j].syncName)
		})
		cs.repos = append(cs.repos, repos...)
	}

	if len(errs) > 0 {
		cs.status = util.ErrorMsg
		rootErr = strings.Join(errs, ", ")
	} else if len(cs.repos) == 0 {
		cs.status = util.UnknownMsg
		rootErr = "No RootSync resources found"
	}

	return rootErr
}

// namespaceRepoClusterStatus populates the given ClusterState with the sync status of
// the specified namespace repo on the ClusterClient's cluster.
func (c *ClusterClient) namespaceRepoClusterStatus(ctx context.Context, cs *ClusterState, ns string) (errorMsg string) {
	var errs []string
	var repoErr string
	syncingConditionSupported := c.syncingConditionSupported(ctx)

	var rgs []*unstructured.Unstructured
	syncs, nsAndNames, err := c.repoSyncs(ctx, ns)
	if err != nil {
		errs = append(errs, err.Error())
	} else {
		rgs, err = c.resourceGroups(ctx, ns, nsAndNames)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(syncs) != len(rgs) {
		errs = append(errs, fmt.Sprintf("expected the number of RepoSyncs and ResourceGroups to be equal, but found %d RepoSyncs and %d ResourceGroups", len(syncs), len(rgs)))
	} else if len(syncs) != 0 {
		var repos []*RepoState
		for i, rs := range syncs {
			rg := rgs[i]
			if rg == nil {
				// We always expect a ResourceGroup, even if we have no managed resources.
				errs = append(errs, rgNotFoundErrMsg(nsAndNames[i].Name, nsAndNames[i].Namespace))
			}
			repos = append(repos, namespaceRepoStatus(rs, rg, syncingConditionSupported))
		}
		sort.Slice(repos, func(i, j int) bool {
			return repos[i].scope < repos[j].scope || (repos[i].scope == repos[j].scope && repos[i].syncName < repos[j].syncName)
		})
		cs.repos = append(cs.repos, repos...)
	}

	if len(errs) > 0 {
		cs.status = util.ErrorMsg
		repoErr = strings.Join(errs, ", ")
	} else if len(cs.repos) == 0 {
		cs.status = util.UnknownMsg
		repoErr = "No RepoSync resources found"
	}

	return repoErr
}

// IsInstalled returns true if the ClusterClient is connected to a cluster where
// Config Sync is installed (ACM operator Pod is running). Updates the given ClusterState with status info if
// Config Sync is not installed.
func (c *ClusterClient) IsInstalled(ctx context.Context, cs *ClusterState) bool {
	if _, err := c.K8sClient.CoreV1().Namespaces().Get(ctx, configmanagement.ControllerNamespace, metav1.GetOptions{}); err != nil && apierrors.IsNotFound(err) {
		cs.status = util.NotInstalledMsg
		cs.Error = fmt.Sprintf("The %q namespace is not found", configmanagement.ControllerNamespace)
		return false
	}
	_, errDeploymentKubeSystem := c.K8sClient.AppsV1().Deployments(metav1.NamespaceSystem).Get(ctx, util.ACMOperatorDeployment, metav1.GetOptions{})
	_, errDeploymentCMSystem := c.K8sClient.AppsV1().Deployments(configmanagement.ControllerNamespace).Get(ctx, util.ACMOperatorDeployment, metav1.GetOptions{})
	podListKubeSystem, errPodsKubeSystem := c.K8sClient.CoreV1().Pods(metav1.NamespaceSystem).List(ctx, metav1.ListOptions{LabelSelector: ACMOperatorLabelSelector})
	podListCMSystem, errPodsCMSystem := c.K8sClient.CoreV1().Pods(configmanagement.ControllerNamespace).List(ctx, metav1.ListOptions{LabelSelector: ACMOperatorLabelSelector})

	switch {
	case errDeploymentKubeSystem != nil && apierrors.IsNotFound(errDeploymentKubeSystem) && errDeploymentCMSystem != nil && apierrors.IsNotFound(errDeploymentCMSystem):
		cs.status = util.NotInstalledMsg
		cs.Error = fmt.Sprintf("The ACM operator is neither installed in the %q namespace nor the %q namespace", metav1.NamespaceSystem, configmanagement.ControllerNamespace)
		return false
	case errDeploymentKubeSystem != nil && apierrors.IsNotFound(errDeploymentKubeSystem) && errDeploymentCMSystem != nil && !apierrors.IsNotFound(errDeploymentCMSystem):
		cs.status = util.ErrorMsg
		cs.Error = fmt.Sprintf("The ACM operator is not installed in the %q namespace, and failed to get the ACM operator Deployment in the %q namespace: %v", metav1.NamespaceSystem, configmanagement.ControllerNamespace, errDeploymentCMSystem)
		return false
	case errDeploymentKubeSystem != nil && !apierrors.IsNotFound(errDeploymentKubeSystem) && errDeploymentCMSystem != nil && apierrors.IsNotFound(errDeploymentCMSystem):
		cs.status = util.ErrorMsg
		cs.Error = fmt.Sprintf("The ACM operator is not installed in the %q namespace, and failed to get the ACM operator Deployment in the %q namespace: %v", configmanagement.ControllerNamespace, metav1.NamespaceSystem, errDeploymentKubeSystem)
		return false
	case errDeploymentKubeSystem != nil && !apierrors.IsNotFound(errDeploymentKubeSystem) && errDeploymentCMSystem != nil && !apierrors.IsNotFound(errDeploymentCMSystem):
		cs.status = util.ErrorMsg
		cs.Error = fmt.Sprintf("Failed to get the ACM operator Deployment in the %q namespace (error: %v), and in the %q namespace (error: %v)", configmanagement.ControllerNamespace, errDeploymentCMSystem, metav1.NamespaceSystem, errDeploymentKubeSystem)
		return false
	case errDeploymentKubeSystem == nil && errDeploymentCMSystem == nil:
		cs.status = util.ErrorMsg
		cmd := fmt.Sprintf("kubectl delete -n %s serviceaccounts config-management-operator && kubectl delete -n %s deployments config-management-operator", metav1.NamespaceSystem, metav1.NamespaceSystem)
		cs.Error = fmt.Sprintf("Found two ACM operators: one from the %q namespace, and the other from the %q namespace. Please remove the one from the %q namespace: %s", metav1.NamespaceSystem, configmanagement.ControllerNamespace, metav1.NamespaceSystem, cmd)
		return false
	case errDeploymentCMSystem == nil && errPodsCMSystem != nil:
		cs.status = util.ErrorMsg
		cs.Error = fmt.Sprintf("Failed to find the ACM operator Pods in the %q namespace: %v", configmanagement.ControllerNamespace, errPodsCMSystem)
		return false
	case errDeploymentCMSystem == nil && !HasRunningPod(podListCMSystem.Items):
		cs.status = util.NotRunningMsg
		cs.Error = fmt.Sprintf("The ACM operator Pod is not running in the %q namespace", configmanagement.ControllerNamespace)
		return false
	case errDeploymentKubeSystem == nil && errPodsKubeSystem != nil:
		cs.status = util.ErrorMsg
		cs.Error = fmt.Sprintf("Failed to find the ACM operator Pods in the %q namespace: %v", metav1.NamespaceSystem, errPodsKubeSystem)
		return false
	case errDeploymentKubeSystem == nil && !HasRunningPod(podListKubeSystem.Items):
		cs.status = util.NotRunningMsg
		cs.Error = fmt.Sprintf("The ACM operator Pod is not running in the %q namespace", metav1.NamespaceSystem)
		return false
	default:
		return true
	}
}

// HasRunningPod returns true if there is a Pod whose phase is running.
func HasRunningPod(pods []corev1.Pod) bool {
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning {
			return true
		}
	}
	return false
}

// IsConfigured returns true if the ClusterClient is connected to a cluster where
// Config Sync is configured. Updates the given ClusterState with status info if
// Config Sync is not configured.
func (c *ClusterClient) IsConfigured(ctx context.Context, cs *ClusterState) bool {
	errs, err := c.ConfigManagement.NestedStringSlice(ctx, "status", "errors")

	if err != nil {
		if apierrors.IsNotFound(err) {
			cs.status = util.NotConfiguredMsg
			cs.Error = "ConfigManagement resource is missing"
		} else {
			cs.status = util.ErrorMsg
			cs.Error = err.Error()
		}
		return false
	}

	if len(errs) > 0 {
		cs.status = util.NotConfiguredMsg
		cs.Error = strings.Join(errs, ", ")
		return false
	}

	return true
}

// ClusterClients returns a map of of typed clients keyed by the name of the kubeconfig context they
// are initialized from.
func ClusterClients(ctx context.Context, contexts []string) (map[string]*ClusterClient, error) {
	configs, err := restconfig.AllKubectlConfigs(flags.ClientTimeout, contexts)
	if configs == nil {
		return nil, fmt.Errorf("failed to create client configs: %w", err)
	}
	if err != nil {
		fmt.Println(err)
	}

	if klog.V(4).Enabled() {
		// Sort contexts for consistent ordering in the log
		var contexts []string
		for ctxName := range configs {
			contexts = append(contexts, ctxName)
		}
		sort.Strings(contexts)
		klog.V(4).Infof("Config contexts after filtering: %s", strings.Join(contexts, ", "))
	}

	var mapMutex sync.Mutex
	var wg sync.WaitGroup
	clientMap := make(map[string]*ClusterClient)
	unreachableClusters := false

	for name, cfg := range configs {
		httpClient, err := rest.HTTPClientFor(cfg)
		if err != nil {
			fmt.Printf("Failed to create HTTPClient for %q: %v\n", name, err)
			continue
		}
		mapper, err := apiutil.NewDynamicRESTMapper(cfg, httpClient)
		if err != nil {
			fmt.Printf("Failed to create mapper for %q: %v\n", name, err)
			continue
		}

		cl, err := client.New(cfg, client.Options{
			Scheme: core.Scheme,
			Mapper: mapper,
		})
		if err != nil {
			fmt.Printf("Failed to generate runtime client for %q: %v\n", name, err)
			continue
		}

		policyHierarchyClientSet, err := versioned.NewForConfig(cfg)
		if err != nil {
			fmt.Printf("Failed to generate Repo client for %q: %v\n", name, err)
			continue
		}

		k8sClientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			fmt.Printf("Failed to generate Kubernetes client for %q: %v\n", name, err)
			continue
		}

		cmClient, err := util.NewConfigManagementClient(cfg)
		if err != nil {
			fmt.Printf("Failed to generate ConfigManagement client for %q: %v\n", name, err)
			continue
		}

		wg.Add(1)

		go func(pcs *versioned.Clientset, kcs *kubernetes.Clientset, cmc *util.ConfigManagementClient, cfgName string) {
			if isReachable(ctx, pcs, cfgName) {
				mapMutex.Lock()
				clientMap[cfgName] = &ClusterClient{
					cl,
					pcs.ConfigmanagementV1().Repos(),
					kcs,
					cmc,
				}
				mapMutex.Unlock()
			} else {
				mapMutex.Lock()
				clientMap[cfgName] = nil
				unreachableClusters = true
				mapMutex.Unlock()
			}
			wg.Done()
		}(policyHierarchyClientSet, k8sClientset, cmClient, name)
	}

	wg.Wait()

	if unreachableClusters {
		// We can't stop the underlying libraries from spamming to klog when a cluster is unreachable,
		// so just flush it out and print a blank line to at least make a clean separation.
		klog.Flush()
		fmt.Println()
	}
	return clientMap, nil
}

// isReachable returns true if the given ClientSet points to a reachable cluster.
func isReachable(ctx context.Context, clientset *versioned.Clientset, cluster string) bool {
	_, err := clientset.RESTClient().Get().DoRaw(ctx)
	if err == nil {
		return true
	}
	if nErr, ok := err.(net.Error); ok && nErr.Timeout() {
		fmt.Printf("%q is an invalid cluster\n", cluster)
	} else {
		fmt.Printf("Failed to connect to cluster %q: %v\n", cluster, err)
	}
	return false
}

// consistentOrder sort the resourcegroups in the same order as the namespace
// and name pairs of RootSyncs or RepoSyncs.
// The resourcegroup list contains ResourceGroup CRs in a specific namespace, or
// from all namespaces, which will include the one from config-management-system.
// The nsAndNames might be the namespace and name pairs of RootSyncs, RepoSyncs
// in a specific namespace, or RepoSyncs in all namespaces.
// For a RepoSync CR, the corresponding ResourceGroup CR may not exist in the cluster.
// We assign it to nil in this case.
func consistentOrder(nsAndNames []types.NamespacedName, resourcegroups []*unstructured.Unstructured) []*unstructured.Unstructured {
	indexMap := map[types.NamespacedName]int{}
	for i, r := range resourcegroups {
		nn := types.NamespacedName{
			Namespace: r.GetNamespace(),
			Name:      r.GetName(),
		}
		indexMap[nn] = i
	}
	rgs := make([]*unstructured.Unstructured, len(nsAndNames))
	for i, nn := range nsAndNames {
		idx, found := indexMap[nn]
		if found {
			rgs[i] = resourcegroups[idx]
		}
	}
	return rgs
}

func rgNotFoundErrMsg(name, ns string) string {
	return fmt.Sprintf("resourcegroups.kpt.dev %q not found in namespace %q", name, ns)
}
