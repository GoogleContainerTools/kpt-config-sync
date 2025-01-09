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

package k8sobjects

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
)

// NamespaceSelectorObject returns an initialized NamespaceSelector.
func NamespaceSelectorObject(opts ...core.MetaMutator) *v1.NamespaceSelector {
	result := &v1.NamespaceSelector{
		TypeMeta: ToTypeMeta(kinds.NamespaceSelector()),
	}
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
	obj := &corev1.ResourceQuota{TypeMeta: ToTypeMeta(kinds.ResourceQuota())}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// RoleBindingObject initializes a RoleBinding.
func RoleBindingObject(opts ...core.MetaMutator) *rbacv1.RoleBinding {
	obj := &rbacv1.RoleBinding{TypeMeta: ToTypeMeta(kinds.RoleBinding())}
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
	obj := &rbacv1beta1.RoleBinding{TypeMeta: ToTypeMeta(kinds.RoleBindingV1Beta1())}
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
	obj := &rbacv1.ClusterRole{TypeMeta: ToTypeMeta(kinds.ClusterRole())}
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
	obj := &rbacv1.ClusterRoleBinding{TypeMeta: ToTypeMeta(kinds.ClusterRoleBinding())}
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// ClusterRoleBindingObject initializes a ClusterRoleBinding.
func ClusterRoleBindingObject(opts ...core.MetaMutator) *rbacv1.ClusterRoleBinding {
	obj := &rbacv1.ClusterRoleBinding{TypeMeta: ToTypeMeta(kinds.ClusterRoleBinding())}
	defaultMutate(obj)
	mutate(obj, opts...)

	return obj
}

// ClusterRoleBindingV1Beta1Object initializes a v1beta1 ClusterRoleBinding.
func ClusterRoleBindingV1Beta1Object(opts ...core.MetaMutator) *rbacv1beta1.ClusterRoleBinding {
	obj := &rbacv1beta1.ClusterRoleBinding{TypeMeta: ToTypeMeta(kinds.ClusterRoleBindingV1Beta1())}
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
	obj := &rbacv1.ClusterRole{TypeMeta: ToTypeMeta(kinds.ClusterRole())}
	defaultMutate(obj)
	mutate(obj, opts...)

	return FileObject(obj, path)
}

// ClusterSelectorObject initializes a ClusterSelector object.
func ClusterSelectorObject(opts ...core.MetaMutator) *v1.ClusterSelector {
	obj := &v1.ClusterSelector{TypeMeta: ToTypeMeta(kinds.ClusterSelector())}
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
	obj := &clusterregistry.Cluster{TypeMeta: ToTypeMeta(kinds.Cluster())}
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
	// RootSync type won't let us remove the status, so convert to unstructured.
	uObj, err := kinds.ToUnstructured(rootSync, core.Scheme)
	if err != nil {
		panic(fmt.Sprintf("Failed to convert RepoSync to Unstructured: %v", err))
	}
	// Since we're faking the output of a parsed file, remove the status.
	delete(uObj.Object, "status")
	return FileObject(uObj, fmt.Sprintf("namespaces/%s/%s.yaml", configsync.ControllerNamespace, name))
}

// RepoSyncV1Beta1 returns a K8S RepoSync resource in a FileObject.
func RepoSyncV1Beta1(ns, name string, opts ...core.MetaMutator) ast.FileObject {
	repoSync := RepoSyncObjectV1Beta1(ns, name, opts...)
	// RootSync type won't let us remove the status, so convert to unstructured.
	uObj, err := kinds.ToUnstructured(repoSync, core.Scheme)
	if err != nil {
		panic(fmt.Sprintf("Failed to convert RepoSync to Unstructured: %v", err))
	}
	// Since we're faking the output of a parsed file, remove the status.
	delete(uObj.Object, "status")
	return FileObject(uObj, fmt.Sprintf("namespaces/%s/%s.yaml", ns, name))
}

// CustomResourceDefinitionV1Beta1Object returns a v1beta1.CustomResourceDefinition.
func CustomResourceDefinitionV1Beta1Object(opts ...core.MetaMutator) *apiextensionsv1beta1.CustomResourceDefinition {
	result := &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: ToTypeMeta(kinds.CustomResourceDefinitionV1Beta1()),
	}
	defaultMutate(result)
	mutate(result, opts...)
	return result
}

// CustomResourceDefinitionV1Object returns a v1.CustomResourceDefinition.
func CustomResourceDefinitionV1Object(opts ...core.MetaMutator) *apiextensionsv1.CustomResourceDefinition {
	result := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: ToTypeMeta(kinds.CustomResourceDefinitionV1()),
	}
	defaultMutate(result)
	mutate(result, opts...)
	return result
}

// AnvilAtPath returns an Anvil Custom Resource.
func AnvilAtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(kinds.Anvil())
	defaultMutate(obj)
	obj.SetName("anvil")
	mutate(obj, opts...)
	return FileObject(obj, path)
}

// SyncObject returns a Sync configured for a particular
func SyncObject(gk schema.GroupKind, opts ...core.MetaMutator) *v1.Sync {
	obj := &v1.Sync{TypeMeta: ToTypeMeta(kinds.Sync())}
	obj.Name = strings.ToLower(gk.String())
	obj.ObjectMeta.Finalizers = append(obj.ObjectMeta.Finalizers, v1.SyncFinalizer)
	obj.Spec.Group = gk.Group
	obj.Spec.Kind = gk.Kind

	mutate(obj, opts...)
	return obj
}

// PersistentVolumeObject returns a PersistentVolume Object.
func PersistentVolumeObject(opts ...core.MetaMutator) *corev1.PersistentVolume {
	result := &corev1.PersistentVolume{TypeMeta: ToTypeMeta(kinds.PersistentVolume())}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// RoleObject initializes a Role.
func RoleObject(opts ...core.MetaMutator) *rbacv1.Role {
	obj := &rbacv1.Role{TypeMeta: ToTypeMeta(kinds.Role())}
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
	obj := &corev1.ConfigMap{TypeMeta: ToTypeMeta(kinds.ConfigMap())}
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
	result := &corev1.Service{TypeMeta: ToTypeMeta(kinds.Service())}
	defaultMutate(result)
	mutate(result, opts...)

	return result
}

// ServiceAccountObject returns an initialized ServiceAccount.
func ServiceAccountObject(name string, opts ...core.MetaMutator) *corev1.ServiceAccount {
	result := &corev1.ServiceAccount{TypeMeta: ToTypeMeta(kinds.ServiceAccount())}
	mutate(result, core.Name(name))
	mutate(result, opts...)

	return result
}
