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

package kinds

import (
	kptfilev1 "github.com/GoogleContainerTools/kpt/pkg/api/kptfile/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	configsyncv1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// Anvil returns the GroupVersionKind for Anvil Custom Resource used in tests.
func Anvil() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "acme.com",
		Version: "v1",
		Kind:    "Anvil",
	}
}

// Sync returns the canonical Sync GroupVersionKind.
func Sync() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.SyncKind)
}

// RoleBinding returns the canonical RoleBinding GroupVersionKind.
func RoleBinding() schema.GroupVersionKind {
	return rbacv1.SchemeGroupVersion.WithKind("RoleBinding")
}

// RoleBindingV1Beta1 returns the canonical v1beta1 RoleBinding GroupVersionKind.
func RoleBindingV1Beta1() schema.GroupVersionKind {
	return rbacv1beta1.SchemeGroupVersion.WithKind("RoleBinding")
}

// Role returns the canonical Role GroupVersionKind.
func Role() schema.GroupVersionKind {
	return rbacv1.SchemeGroupVersion.WithKind("Role")
}

// ResourceQuota returns the canonical ResourceQuota GroupVersionKind.
func ResourceQuota() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("ResourceQuota")
}

// Repo returns the canonical Repo GroupVersionKind.
func Repo() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.RepoKind)
}

// PersistentVolume returns the canonical PersistentVolume GroupVersionKind.
func PersistentVolume() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("PersistentVolume")
}

// NamespaceConfig returns the canonical NamespaceConfig GroupVersionKind.
func NamespaceConfig() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.NamespaceConfigKind)
}

// PodSecurityPolicy returns the canonical PodSecurityPolicy GroupVersionKind.
func PodSecurityPolicy() schema.GroupVersionKind {
	return policyv1beta1.SchemeGroupVersion.WithKind("PodSecurityPolicy")
}

// NamespaceSelector returns the canonical NamespaceSelector GroupVersionKind.
func NamespaceSelector() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.NamespaceSelectorKind)
}

// Namespace returns the canonical Namespace GroupVersionKind.
func Namespace() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Namespace")
}

// CustomResourceDefinitionKind is the Kind for CustomResourceDefinitions
const CustomResourceDefinitionKind = "CustomResourceDefinition"

// CustomResourceDefinitionV1Beta1 returns the v1beta1 CustomResourceDefinition GroupVersionKind.
func CustomResourceDefinitionV1Beta1() schema.GroupVersionKind {
	return CustomResourceDefinition().WithVersion(v1beta1.SchemeGroupVersion.Version)
}

// CustomResourceDefinitionV1 returns the v1 CustomResourceDefinition GroupVersionKind.
func CustomResourceDefinitionV1() schema.GroupVersionKind {
	return CustomResourceDefinition().WithVersion("v1")
}

// CustomResourceDefinition returns the canonical CustomResourceDefinition GroupKind
func CustomResourceDefinition() schema.GroupKind {
	return schema.GroupKind{
		Group: v1beta1.GroupName,
		Kind:  CustomResourceDefinitionKind,
	}
}

// ClusterSelector returns the canonical ClusterSelector GroupVersionKind.
func ClusterSelector() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.ClusterSelectorKind)
}

// ClusterRoleBinding returns the canonical ClusterRoleBinding GroupVersionKind.
func ClusterRoleBinding() schema.GroupVersionKind {
	return rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding")
}

// ClusterRoleBindingV1Beta1 returns the canonical ClusterRoleBinding GroupVersionKind.
func ClusterRoleBindingV1Beta1() schema.GroupVersionKind {
	return rbacv1beta1.SchemeGroupVersion.WithKind("ClusterRoleBinding")
}

// ClusterRole returns the canonical ClusterRole GroupVersionKind.
func ClusterRole() schema.GroupVersionKind {
	return rbacv1.SchemeGroupVersion.WithKind("ClusterRole")
}

// ClusterConfig returns the canonical ClusterConfig GroupVersionKind.
func ClusterConfig() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.ClusterConfigKind)
}

// Cluster returns the canonical Cluster GroupVersionKind.
func Cluster() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "clusterregistry.k8s.io", Version: "v1alpha1", Kind: "Cluster"}
}

// Deployment returns the canonical Deployment GroupVersionKind.
func Deployment() schema.GroupVersionKind {
	return appsv1.SchemeGroupVersion.WithKind("Deployment")
}

// Pod returns the canonical Pod GroupVersionKind.
func Pod() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Pod")
}

// DaemonSet returns the canonical DaemonSet GroupVersionKind.
func DaemonSet() schema.GroupVersionKind {
	return appsv1.SchemeGroupVersion.WithKind("DaemonSet")
}

// Ingress returns the canonical Ingress GroupVersionKind.
func Ingress() schema.GroupVersionKind {
	return networkingv1.SchemeGroupVersion.WithKind("Ingress")
}

// ReplicaSet returns the canonical ReplicaSet GroupVersionKind.
func ReplicaSet() schema.GroupVersionKind {
	return appsv1.SchemeGroupVersion.WithKind("ReplicaSet")
}

// HierarchyConfig returns the canonical HierarchyConfig GroupVersionKind.
func HierarchyConfig() schema.GroupVersionKind {
	return v1.SchemeGroupVersion.WithKind(configmanagement.HierarchyConfigKind)
}

// NetworkPolicy returns the canonical NetworkPolicy GroupVersionKind.
func NetworkPolicy() schema.GroupVersionKind {
	return networkingv1.SchemeGroupVersion.WithKind("NetworkPolicy")
}

// ConfigMap returns the canonical ConfigMap GroupVersionKind.
func ConfigMap() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("ConfigMap")
}

// Job returns the canonical Job GroupVersionKind.
func Job() schema.GroupVersionKind {
	return batchv1.SchemeGroupVersion.WithKind("Job")
}

// CronJob returns the canonical CronJob GroupVersionKind.
func CronJob() schema.GroupVersionKind {
	return batchv1.SchemeGroupVersion.WithKind("CronJob")
}

// ReplicationController returns the canonical ReplicationController GroupVersionKind.
func ReplicationController() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("ReplicationController")
}

// StatefulSet returns the canonical StatefulSet GroupVersionKind.
func StatefulSet() schema.GroupVersionKind {
	return appsv1.SchemeGroupVersion.WithKind("StatefulSet")
}

// RepoSyncV1Alpha1 returns the canonical RepoSync GroupVersionKind.
func RepoSyncV1Alpha1() schema.GroupVersionKind {
	return v1alpha1.SchemeGroupVersion.WithKind("RepoSync")
}

// RepoSyncV1Beta1 returns the v1beta1 RepoSync GroupVersionKind.
func RepoSyncV1Beta1() schema.GroupVersionKind {
	return configsyncv1beta1.SchemeGroupVersion.WithKind("RepoSync")
}

// RootSyncV1Alpha1 returns the canonical RootSync GroupVersionKind.
func RootSyncV1Alpha1() schema.GroupVersionKind {
	return v1alpha1.SchemeGroupVersion.WithKind("RootSync")
}

// RootSyncV1Beta1 returns the v1beta1 RootSync GroupVersionKind.
func RootSyncV1Beta1() schema.GroupVersionKind {
	return configsyncv1beta1.SchemeGroupVersion.WithKind("RootSync")
}

// Service returns the canonical Service GroupVersionKind.
func Service() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Service")
}

// Secret returns the canonical Secret GroupVersionKind.
func Secret() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("Secret")
}

// ServiceAccount returns the canonical ServiceAccount GroupVersionKind.
func ServiceAccount() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("ServiceAccount")
}

// KptFile returns the canonical Kptfile GroupVersionKind.
func KptFile() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: kptfilev1.KptFileGroup, Version: kptfilev1.KptFileVersion, Kind: kptfilev1.KptFileKind}
}

// APIService returns the APIService kind.
func APIService() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "apiregistration.k8s.io", Version: "v1", Kind: "APIService"}
}
