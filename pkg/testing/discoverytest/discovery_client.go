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

// Package discoverytest contains a fake implementation of the API discovery
// mechanism seeded with the types used in Config Sync.
package discoverytest

import (
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/restmapper"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/kinds"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
)

// Client returns a new test DiscoveryClient that has mappings for test and provided resources.
func Client(extraResources []*restmapper.APIGroupResources) utildiscovery.ServerResourcer {
	return newFakeDiscoveryClient(testAPIResourceList(testDynamicResources(extraResources...)))
}

// fakeDiscoveryClient is a DiscoveryClient with stubbed API Resources.
type fakeDiscoveryClient struct {
	APIGroupResources []*metav1.APIResourceList
}

// newFakeDiscoveryClient returns a DiscoveryClient with stubbed API Resources.
func newFakeDiscoveryClient(res []*metav1.APIResourceList) utildiscovery.ServerResourcer {
	return &fakeDiscoveryClient{APIGroupResources: res}
}

// ServerGroupsAndResources returns the stubbed list of available API groups
// and resources.
func (d *fakeDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, d.APIGroupResources, nil
}

// testAPIResourceList returns the API ResourceList as would be returned by the DiscoveryClient ServerResources
// call which represents resources that are returned by the API server during discovery.
func testAPIResourceList(rs []*restmapper.APIGroupResources) []*metav1.APIResourceList {
	var apiResources []*metav1.APIResourceList
	for _, item := range rs {
		for version, resources := range item.VersionedResources {
			apiResources = append(apiResources, &metav1.APIResourceList{
				TypeMeta: metav1.TypeMeta{
					APIVersion: metav1.SchemeGroupVersion.String(),
					Kind:       "APIResourceList",
				},
				GroupVersion: schema.GroupVersion{Group: item.Group.Name, Version: version}.String(),
				APIResources: resources,
			})
		}
	}
	return apiResources
}

func testK8SResources() []*restmapper.APIGroupResources {
	return []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "pods", Namespaced: true, Kind: "Pod"},
					{Name: "services", Namespaced: true, Kind: "Service"},
					{Name: "replicationcontrollers", Namespaced: true, Kind: "ReplicationController"},
					{Name: "componentstatuses", Namespaced: false, Kind: "ComponentStatus"},
					{Name: "nodes", Namespaced: false, Kind: "Node"},
					{Name: "secrets", Namespaced: true, Kind: "Secret"},
					{Name: "configmaps", Namespaced: true, Kind: "ConfigMap"},
					{Name: "namespaces", Namespaced: false, Kind: "Namespace"},
					{Name: "resourcequotas", Namespaced: true, Kind: "ResourceQuota"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apiextensions.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "customresourcedefinitions", Namespaced: false, Kind: kinds.CustomResourceDefinitionKind},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "extensions",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "customresourcedefinitions", Namespaced: false, Kind: kinds.CustomResourceDefinitionKind},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "policy",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "podsecuritypolicyies", Namespaced: false, Kind: "PodSecurityPolicy"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta2"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta2"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta2": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v1beta2"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
				"v1beta2": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				},
				"v1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "autoscaling",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
					{Version: "v2beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v2beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "horizontalpodautoscalers", Namespaced: true, Kind: "HorizontalPodAutoscaler"},
				},
				"v2beta1": {
					{Name: "horizontalpodautoscalers", Namespaced: true, Kind: "HorizontalPodAutoscaler"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "storage.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v0"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "storageclasses", Namespaced: false, Kind: "StorageClass"},
				},
				// bogus version of a known group/version/resource to make sure kubectl falls back to generic object mode
				"v0": {
					{Name: "storageclasses", Namespaced: false, Kind: "StorageClass"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "rbac.authorization.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "roles", Namespaced: true, Kind: "Role"},
					{Name: "rolebindings", Namespaced: true, Kind: "RoleBinding"},
					{Name: "clusterroles", Namespaced: false, Kind: "ClusterRole"},
					{Name: "clusterrolebindings", Namespaced: false, Kind: "ClusterRoleBinding"},
				},
				"v1beta1": {
					{Name: "clusterrolebindings", Namespaced: false, Kind: "ClusterRoleBinding"},
				},
			},
		},
	}
}

// testDynamicResources returns API Resources for both standard K8S resources
// and Nomos resources.
func testDynamicResources(extraResources ...*restmapper.APIGroupResources) []*restmapper.APIGroupResources {
	r := testK8SResources()
	r = append(r, []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Name: configmanagement.GroupName,
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1alpha1"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1alpha1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1alpha1": {
					{Name: "clusterselectors", Namespaced: false, Kind: configmanagement.ClusterSelectorKind},
					{Name: "namespaceselectors", Namespaced: false, Kind: configmanagement.NamespaceSelectorKind},
					{Name: "repos", Namespaced: false, Kind: configmanagement.RepoKind},
					{Name: "syncs", Namespaced: false, Kind: configmanagement.SyncKind},
					{Name: "hierarchyconfigs", Namespaced: false, Kind: configmanagement.HierarchyConfigKind},
					{Name: "namespaceconfigs", Namespaced: false, Kind: configmanagement.NamespaceConfigKind},
				},
				"v1": {
					{Name: "clusterselectors", Namespaced: false, Kind: configmanagement.ClusterSelectorKind},
					{Name: "namespaceselectors", Namespaced: false, Kind: configmanagement.NamespaceSelectorKind},
					{Name: "repos", Namespaced: false, Kind: configmanagement.RepoKind},
					{Name: "syncs", Namespaced: false, Kind: configmanagement.SyncKind},
					{Name: "hierarchyconfigs", Namespaced: false, Kind: configmanagement.HierarchyConfigKind},
					{Name: "namespaceconfigs", Namespaced: false, Kind: configmanagement.NamespaceConfigKind},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "clusterregistry.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1alpha1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1alpha1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1alpha1": {
					{Name: "clusters", Namespaced: false, Kind: "Cluster"},
				},
			},
		},
	}...,
	)
	r = append(r, extraResources...)
	return r
}

// CRDsToAPIGroupResources converts a list of CRDs to the corresponding APIGroupResources the
// server would return.
//
// As-is assumes each CRD is a different APIGroup. Don't bother fixing unless you need to test
// a case where you need to sync multiple CRDs for the same APIGroup.
func CRDsToAPIGroupResources(crds []*v1beta1.CustomResourceDefinition) []*restmapper.APIGroupResources {
	var result []*restmapper.APIGroupResources
	for _, syncedCRD := range crds {
		extraResource := &restmapper.APIGroupResources{
			Group: metav1.APIGroup{
				Name: syncedCRD.Spec.Group,
				PreferredVersion: metav1.GroupVersionForDiscovery{
					GroupVersion: syncedCRD.Spec.Group + "/" + syncedCRD.Spec.Versions[0].Name,
				},
			},
		}

		extraResource.VersionedResources = make(map[string][]metav1.APIResource)
		for _, version := range syncedCRD.Spec.Versions {
			extraResource.Group.Versions = append(extraResource.Group.Versions,
				metav1.GroupVersionForDiscovery{
					GroupVersion: syncedCRD.Spec.Group + "/" + version.Name,
					Version:      version.Name,
				},
			)
			extraResource.VersionedResources[version.Name] = []metav1.APIResource{
				{
					Name:         syncedCRD.Spec.Names.Plural,
					SingularName: syncedCRD.Spec.Names.Singular,
					Namespaced:   !(syncedCRD.Spec.Scope == v1beta1.ClusterScoped),
					Group:        syncedCRD.Spec.Group,
					Version:      version.Name,
					Kind:         syncedCRD.Spec.Names.Kind,
				},
			}
		}

		result = append(result, extraResource)
	}
	return result
}
