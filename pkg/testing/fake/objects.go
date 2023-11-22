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

package fake

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
)

// NamespaceSelectorObject returns an initialized NamespaceSelector.
func NamespaceSelectorObject(opts ...core.MetaMutator) *v1.NamespaceSelector {
	result := &v1.NamespaceSelector{}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// NamespaceSelector returns a Nomos NamespaceSelector at a default path.
func NamespaceSelector(opts ...core.MetaMutator) ast.FileObject {
	return NamespaceSelectorAtPath("namespaces/ns.yaml", opts...)
}

// NamespaceSelectorAtPath returns a NamespaceSelector at the specified path.
func NamespaceSelectorAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(NamespaceSelectorObject(opts...), path)
}

// NamespaceSelectorAtPathWithName returns a NamespaceSelector at the specified
// path with the specified name.
func NamespaceSelectorAtPathWithName(path string, name string, opts ...core.MetaMutator) ast.FileObject {
	opts = append(opts, core.Name(name))
	return FileObject(NamespaceSelectorObject(opts...), path)
}

// ResourceQuotaObject initializes a ResouceQuota.
func ResourceQuotaObject(opts ...core.MetaMutator) *corev1.ResourceQuota {
	obj := &corev1.ResourceQuota{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// RoleBindingObject initializes a RoleBinding.
func RoleBindingObject(opts ...core.MetaMutator) *rbacv1.RoleBinding {
	obj := &rbacv1.RoleBinding{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// RoleBindingAtPath returns a RoleBinding at the specified path.
func RoleBindingAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(RoleBindingObject(opts...), path)
}

// RoleBinding returns an rbacv1 RoleBinding.
func RoleBinding(opts ...core.MetaMutator) ast.FileObject {
	return RoleBindingAtPath("namespaces/foo/rolebinding.yaml", opts...)
}

// RoleBindingV1Beta1Object initializes a v1beta1 RoleBinding.
func RoleBindingV1Beta1Object(opts ...core.MetaMutator) *rbacv1beta1.RoleBinding {
	obj := &rbacv1beta1.RoleBinding{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// RoleBindingV1Beta1AtPath returns a RoleBinding at the specified path.
func RoleBindingV1Beta1AtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(RoleBindingV1Beta1Object(opts...), path)
}

// RoleBindingV1Beta1 returns an rbacv1beta1 RoleBinding.
func RoleBindingV1Beta1(opts ...core.MetaMutator) ast.FileObject {
	return RoleBindingV1Beta1AtPath("namespaces/foo/rolebinding.yaml", opts...)
}

// ClusterRoleObject returns an rbacv1 ClusterRole.
func ClusterRoleObject(opts ...core.MetaMutator) *rbacv1.ClusterRole {
	obj := &rbacv1.ClusterRole{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ClusterRole returns an rbacv1 ClusterRole at the specified path.
func ClusterRole(opts ...core.MetaMutator) ast.FileObject {
	return ClusterRoleAtPath("cluster/cr.yaml", opts...)
}

// ClusterRoleBindingAtPath returns a ClusterRoleBinding at the specified path.
func ClusterRoleBindingAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	obj := &rbacv1.ClusterRoleBinding{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// ClusterRoleBindingObject initializes a ClusterRoleBinding.
func ClusterRoleBindingObject(opts ...core.MetaMutator) *rbacv1.ClusterRoleBinding {
	obj := &rbacv1.ClusterRoleBinding{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ClusterRoleBindingV1Beta1Object initializes a v1beta1 ClusterRoleBinding.
func ClusterRoleBindingV1Beta1Object(opts ...core.MetaMutator) *rbacv1beta1.ClusterRoleBinding {
	obj := &rbacv1beta1.ClusterRoleBinding{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ClusterRoleBinding returns an initialized ClusterRoleBinding.
func ClusterRoleBinding(opts ...core.MetaMutator) ast.FileObject {
	return ClusterRoleBindingAtPath("cluster/crb.yaml", opts...)
}

// ClusterRoleAtPath returns a ClusterRole at the specified path.
func ClusterRoleAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	obj := &rbacv1.ClusterRole{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// ClusterSelectorObject initializes a ClusterSelector object.
func ClusterSelectorObject(opts ...core.MetaMutator) *v1.ClusterSelector {
	obj := &v1.ClusterSelector{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ClusterSelector returns a Nomos ClusterSelector.
func ClusterSelector(opts ...core.MetaMutator) ast.FileObject {
	return ClusterSelectorAtPath("clusterregistry/cs.yaml", opts...)
}

// ClusterSelectorAtPath returns a ClusterSelector at the specified path.
func ClusterSelectorAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(ClusterSelectorObject(opts...), path)
}

// ClusterObject returns a fake Cluster object
func ClusterObject(opts ...core.MetaMutator) *clusterregistry.Cluster {
	obj := &clusterregistry.Cluster{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// Cluster returns a K8S Cluster resource in a FileObject.
func Cluster(opts ...core.MetaMutator) ast.FileObject {
	return ClusterAtPath("clusterregistry/cluster.yaml", opts...)
}

// ClusterAtPath returns a Cluster at the specified path.
func ClusterAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(ClusterObject(opts...), path)
}

// RootSyncV1Beta1 returns a K8S RootSync resource in a FileObject.
func RootSyncV1Beta1(name string, opts ...core.MetaMutator) ast.FileObject {
	rootSync := RootSyncObjectV1Beta1(name, opts...)
	return FileObject(rootSync, fmt.Sprintf("namespaces/%s/%s.yaml", configsync.ControllerNamespace, name))
}

// RepoSyncV1Beta1 returns a K8S RepoSync resource in a FileObject.
func RepoSyncV1Beta1(ns, name string, opts ...core.MetaMutator) ast.FileObject {
	repoSync := RepoSyncObjectV1Beta1(ns, name, opts...)
	return FileObject(repoSync, fmt.Sprintf("namespaces/%s/%s.yaml", ns, name))
}

// CustomResourceDefinitionV1Beta1Object returns an initialized CustomResourceDefinition.
func CustomResourceDefinitionV1Beta1Object(opts ...core.MetaMutator) *v1beta1.CustomResourceDefinition {
	result := &v1beta1.CustomResourceDefinition{}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// CustomResourceDefinitionV1Beta1 returns a FileObject containing a
// CustomResourceDefinition at a default path.
func CustomResourceDefinitionV1Beta1(opts ...core.MetaMutator) ast.FileObject {
	return FileObject(CustomResourceDefinitionV1Beta1Object(opts...), "cluster/crd.yaml")
}

// CustomResourceDefinitionV1Beta1Unstructured returns a v1Beta1 CRD as an unstructured
func CustomResourceDefinitionV1Beta1Unstructured(opts ...core.MetaMutator) *unstructured.Unstructured {
	obj := CustomResourceDefinitionV1Beta1Object(opts...)
	uObj, err := kinds.ToUnstructured(obj, core.Scheme)
	if err != nil {
		// Should be impossible, and this is test-only code so it's fine.
		klog.Fatal(err)
	}
	normalizeUnstructured(uObj)
	return uObj
}

// CustomResourceDefinitionV1Object returns an initialized CustomResourceDefinition.
func CustomResourceDefinitionV1Object(opts ...core.MetaMutator) *apiextensionsv1.CustomResourceDefinition {
	result := &apiextensionsv1.CustomResourceDefinition{}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// CustomResourceDefinitionV1 returns a FileObject containing a
// CustomResourceDefinition at a default path.
func CustomResourceDefinitionV1(opts ...core.MetaMutator) ast.FileObject {
	return FileObject(CustomResourceDefinitionV1Object(opts...), "cluster/crd.yaml")
}

// CustomResourceDefinitionV1Unstructured returns a v1 CRD as an unstructured
func CustomResourceDefinitionV1Unstructured(opts ...core.MetaMutator) *unstructured.Unstructured {
	obj := CustomResourceDefinitionV1Object(opts...)
	uObj, err := kinds.ToUnstructured(obj, core.Scheme)
	if err != nil {
		// Should be impossible, and this is test-only code so it's fine.
		klog.Fatal(err)
	}
	normalizeUnstructured(uObj)
	return uObj
}

// AnvilAtPath returns an Anvil Custom Resource.
func AnvilAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kinds.Anvil())
	obj.SetName("anvil")
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// AnvilCRDAtPath returns an Anvil Custom Resource Definition.
func AnvilCRDAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	gvk := kinds.Anvil()
	obj := &v1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "anvil",
		},
		Spec: v1beta1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: v1beta1.CustomResourceDefinitionNames{
				Kind: gvk.Kind,
			},
		},
	}
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// SyncObject returns a Sync configured for a particular
func SyncObject(gk schema.GroupKind, opts ...core.MetaMutator) *v1.Sync {
	obj := &v1.Sync{}
	obj.Name = strings.ToLower(gk.String())
	obj.ObjectMeta.Finalizers = append(obj.ObjectMeta.Finalizers, v1.SyncFinalizer)
	obj.Spec.Group = gk.Group
	obj.Spec.Kind = gk.Kind

	mutate(obj, opts...)
	return obj
}

// PersistentVolumeObject returns a PersistentVolume Object.
func PersistentVolumeObject(opts ...core.MetaMutator) *corev1.PersistentVolume {
	result := &corev1.PersistentVolume{}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// RoleObject initializes a Role.
func RoleObject(opts ...core.MetaMutator) *rbacv1.Role {
	obj := &rbacv1.Role{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// RoleAtPath returns an initialized Role in a FileObject.
func RoleAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(RoleObject(opts...), path)
}

// Role returns a Role with a default file path.
func Role(opts ...core.MetaMutator) ast.FileObject {
	return RoleAtPath("namespaces/foo/role.yaml", opts...)
}

// ConfigMapObject returns an initialized ConfigMap.
func ConfigMapObject(opts ...core.MetaMutator) *corev1.ConfigMap {
	obj := &corev1.ConfigMap{}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ConfigMap returns a ConfigMap with a default file path.
func ConfigMap(opts ...core.MetaMutator) ast.FileObject {
	return FileObject(ConfigMapObject(opts...), "namespaces/foo/configmap.yaml")
}

// ToTypeMeta creates the TypeMeta that corresponds to the passed GroupVersionKind.
func ToTypeMeta(gvk schema.GroupVersionKind) metav1.TypeMeta {
	return metav1.TypeMeta{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
	}
}

// ServiceObject returns a default-initialized Service with the passed opts
// applied.
func ServiceObject(opts ...core.MetaMutator) *corev1.Service {
	result := &corev1.Service{}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// ServiceAccountObject returns an initialized ServiceAccount.
func ServiceAccountObject(name string, opts ...core.MetaMutator) *corev1.ServiceAccount {
	result := &corev1.ServiceAccount{}
	mutate(result, core.Name(name))
	mutate(result, opts...)

	return result
}
