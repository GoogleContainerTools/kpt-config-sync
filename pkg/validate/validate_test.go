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

package validate

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/hnc"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/hierarchyconfig"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/metadata"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/semantic"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/syntax"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/discoverytest"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/cli-utils/pkg/common"
)

const dir = "acme"

func clusterSelector(name, key, value string) *v1.ClusterSelector {
	cs := fake.ClusterSelectorObject(core.Name(name))
	cs.Spec.Selector.MatchLabels = map[string]string{key: value}
	return cs
}

func namespaceSelector(name, key, value string) *v1.NamespaceSelector {
	ns := fake.NamespaceSelectorObject(core.Name(name))
	ns.Spec.Selector.MatchLabels = map[string]string{key: value}
	return ns
}

func crdUnstructured(t *testing.T, gvk schema.GroupVersionKind, opts ...core.MetaMutator) *unstructured.Unstructured {
	t.Helper()
	u := fake.CustomResourceDefinitionV1Beta1Unstructured()
	pluralKind := strings.ToLower(gvk.Kind) + "s"
	u.SetName(pluralKind + "." + gvk.Group)
	if err := unstructured.SetNestedField(u.Object, gvk.Group, "spec", "group"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(u.Object, gvk.Kind, "spec", "names", "kind"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(u.Object, pluralKind, "spec", "names", "plural"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(u.Object, string(apiextensionsv1beta1.NamespaceScoped), "spec", "scope"); err != nil {
		t.Fatal(err)
	}
	versions := []interface{}{
		map[string]interface{}{
			"name":   gvk.Version,
			"served": true,
		},
	}
	if err := unstructured.SetNestedSlice(u.Object, versions, "spec", "versions"); err != nil {
		t.Fatal(err)
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

func crdObject(gvk schema.GroupVersionKind, opts ...core.MetaMutator) *apiextensionsv1beta1.CustomResourceDefinition {
	o := fake.CustomResourceDefinitionV1Beta1Object()
	o.Spec.Names.Plural = strings.ToLower(gvk.Kind) + "s"
	o.SetName(o.Spec.Names.Plural + "." + gvk.Group)
	o.Spec.Group = gvk.Group
	o.Spec.Names.Kind = gvk.Kind
	o.Spec.Versions = append(o.Spec.Versions,
		apiextensionsv1beta1.CustomResourceDefinitionVersion{Name: gvk.Version, Served: true},
	)
	o.Spec.Scope = apiextensionsv1beta1.ClusterScoped

	for _, opt := range opts {
		opt(o)
	}

	return o
}

func TestHierarchical(t *testing.T) {
	testCases := []struct {
		name          string
		discoveryCRDs []*apiextensionsv1beta1.CustomResourceDefinition
		options       Options
		objs          []ast.FileObject
		want          []ast.FileObject
		wantErrs      status.MultiError
	}{
		{
			name: "only a valid repo",
			objs: []ast.FileObject{
				fake.Repo(),
			},
		},
		{
			name: "namespace and object in it",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo"),
				fake.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/role.yaml")),
			},
		},
		{
			name: "abstract namespaces with object inheritance",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/bar/foo"),
				fake.Namespace("namespaces/bar/qux/lym"),
				fake.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first")),
				fake.RoleAtPath("namespaces/bar/qux/role.yaml",
					core.Name("second")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/bar/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first"),
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/role.yaml")),
				fake.Namespace("namespaces/bar/qux/lym",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/qux/lym/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "2"),
					core.Label("qux.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("lym.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first"),
					core.Namespace("lym"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/role.yaml")),
				fake.RoleAtPath("namespaces/bar/qux/role.yaml",
					core.Name("second"),
					core.Namespace("lym"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/qux/role.yaml")),
			},
		},
		{
			name: "CRD and CR",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.FileObject(crdUnstructured(t, kinds.Anvil()), "cluster/crd.yaml"),
				fake.Namespace("namespaces/foo"),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				fake.FileObject(crdUnstructured(t, kinds.Anvil(),
					core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{"f:group":{},"f:names":{"f:kind":{},"f:plural":{}},"f:scope":{},"f:versions":{}},"f:status":{"f:acceptedNames":{"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/crd.yaml")), "cluster/crd.yaml"),
				fake.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{".":{},"f:group":{},"f:names":{".":{},"f:kind":{},"f:plural":{}},"f:scope":{}},"f:status":{".":{},"f:acceptedNames":{".":{},"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml")),
			},
		},
		{
			name: "CR in repo and CRD on API server",
			discoveryCRDs: []*apiextensionsv1beta1.CustomResourceDefinition{
				crdObject(kinds.Anvil()),
			},
			objs: []ast.FileObject{
				fake.Repo(),
				fake.AnvilAtPath("cluster/anvil.yaml"),
			},
			want: []ast.FileObject{
				fake.AnvilAtPath("cluster/anvil.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{".":{},"f:group":{},"f:names":{".":{},"f:kind":{},"f:plural":{}},"f:scope":{}},"f:status":{".":{},"f:acceptedNames":{".":{},"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/anvil.yaml")),
			},
		},
		{
			name: "CR without CRD and allow unknown kinds",
			options: Options{
				AllowUnknownKinds: true,
			},
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo"),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.UnknownScopeAnnotationKey, csmetadata.UnknownScopeAnnotationValue),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{".":{},"f:group":{},"f:names":{".":{},"f:kind":{},"f:plural":{}},"f:scope":{}},"f:status":{".":{},"f:acceptedNames":{".":{},"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml")),
			},
		},
		{
			name: "objects with cluster selectors",
			options: Options{
				ClusterName: "prod",
			},
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterAtPath("clusterregistry/cluster-dev.yaml",
					core.Name("dev"),
					core.Label("environment", "dev")),
				fake.ClusterAtPath("clusterregistry/cluster-prod.yaml",
					core.Name("prod"),
					core.Label("environment", "prod")),
				fake.FileObject(clusterSelector("prod-only", "environment", "prod"), "clusterregistry/prod-only_cs.yaml"),
				fake.FileObject(clusterSelector("dev-only", "environment", "dev"), "clusterregistry/dev-only_cs.yaml"),
				// Should be selected
				fake.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("prod-admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				fake.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				fake.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Namespace("bookstore")),
				fake.Namespace("namespaces/prod-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.RoleAtPath("namespaces/prod-shipping/prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping")),
				fake.RoleAtPath("namespaces/prod-shipping/prod-swe.yaml",
					core.Name("prod-swe")),
				// Should not be selected
				fake.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.ClusterRoleAtPath("cluster/dev-admin_cr.yaml",
					core.Name("dev-admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				fake.ClusterRoleAtPath("cluster/dev-owner_cr.yaml",
					core.Name("dev-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.RoleAtPath("namespaces/dev-abstract.yaml",
					core.Name("abstract"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.RoleAtPath("namespaces/bookstore/role-dev.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev"),
					core.Namespace("bookstore")),
				fake.Namespace("namespaces/dev-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.RoleAtPath("namespaces/dev-shipping/dev-sre.yaml",
					core.Name("dev-sre"),
					core.Namespace("dev-shipping")),
				fake.RoleAtPath("namespaces/dev-shipping/dev-swe.yaml",
					core.Name("dev-swe")),
			},
			want: []ast.FileObject{
				fake.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("prod-admin"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/cluster-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-admin_cr.yaml")),
				fake.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/cluster-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-dev_cr.yaml")),
				fake.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-owner_cr.yaml")),
				fake.Namespace("namespaces/bookstore",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bookstore.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/cluster-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/role-prod.yaml")),
				fake.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-abstract.yaml")),
				fake.Namespace("namespaces/prod-shipping",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("prod-shipping.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleAtPath("namespaces/prod-shipping/prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/prod-sre.yaml")),
				fake.RoleAtPath("namespaces/prod-shipping/prod-swe.yaml",
					core.Name("prod-swe"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/prod-swe.yaml")),
				fake.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-abstract.yaml")),
			},
		},
		{
			name: "object with namespace selector",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.FileObject(namespaceSelector("sre-supported", "env", "prod"),
					"namespaces/bar/ns-selector.yaml"),
				fake.FileObject(namespaceSelector("dev-supported", "env", "test"),
					"namespaces/bar/ns-selector.yaml"),
				fake.RoleBindingAtPath("namespaces/bar/sre-rb.yaml",
					core.Name("rb"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre-supported")),
				fake.RoleBindingAtPath("namespaces/bar/dev-rb.yaml",
					core.Name("rb"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev-supported")),
				fake.Namespace("namespaces/bar/prod-ns",
					core.Label("env", "prod")),
				fake.Namespace("namespaces/bar/test-ns",
					core.Label("env", "test")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/bar/prod-ns",
					core.Label("env", "prod"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{"f:env":{}}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/prod-ns/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("prod-ns.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleBindingAtPath("namespaces/bar/sre-rb.yaml",
					core.Name("rb"),
					core.Namespace("prod-ns"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/namespace-selector":{}},"f:labels":{}},"f:roleRef":{"f:apiGroup":{},"f:kind":{},"f:name":{}}}`),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre-supported"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/sre-rb.yaml")),
				fake.Namespace("namespaces/bar/test-ns",
					core.Label("env", "test"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{"f:env":{}}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/test-ns/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("test-ns.tree.hnc.x-k8s.io/depth", "0")),
				fake.RoleBindingAtPath("namespaces/bar/dev-rb.yaml",
					core.Name("rb"),
					core.Namespace("test-ns"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/namespace-selector":{}},"f:labels":{}},"f:roleRef":{"f:apiGroup":{},"f:kind":{},"f:name":{}}}`),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev-supported"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/dev-rb.yaml")),
			},
		},
		{
			name: "abstract namespaces with shared names",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/bar/foo"),
				fake.Namespace("namespaces/foo/bar"),
				fake.Namespace("namespaces/foo/foo/qux"),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/bar/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.Namespace("namespaces/foo/bar",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/bar/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "0")),
				fake.Namespace("namespaces/foo/foo/qux",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/foo/qux/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("qux.tree.hnc.x-k8s.io/depth", "0")),
			},
		},
		{
			name: "system namespaces",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/default"),
				fake.Namespace("namespaces/kube-system"),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/default",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/default/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Label("default.tree.hnc.x-k8s.io/depth", "0")),
				fake.Namespace("namespaces/kube-system",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/kube-system/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Label("kube-system.tree.hnc.x-k8s.io/depth", "0")),
			},
		},
		{
			name: "objects in non-namespace subdirectories",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.HierarchyConfigAtPath("system/sub/hc.yaml"),
				fake.ClusterAtPath("clusterregistry/foo/cluster.yaml"),
				fake.ClusterRoleBindingAtPath("cluster/foo/crb.yaml"),
			},
			want: []ast.FileObject{
				fake.ClusterRoleBindingAtPath("cluster/foo/crb.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:roleRef":{"f:apiGroup":{},"f:kind":{},"f:name":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/foo/crb.yaml")),
			},
		},
		{
			name: "same namespace resource with different cluster selector",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.FileObject(clusterSelector("prod-only", "environment", "prod"), "clusterregistry/prod-only_cs.yaml"),
				fake.FileObject(clusterSelector("dev-only", "environment", "dev"), "clusterregistry/dev-only_cs.yaml"),
				fake.NamespaceAtPath("namespaces/foo/foo-prod.yaml",
					core.Name("foo"), core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				fake.NamespaceAtPath("namespaces/foo/foo-dev.yaml",
					core.Name("foo"), core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				fake.NamespaceAtPath("namespaces/foo/foo-stg.yaml",
					core.Name("foo"), core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "stg-only")),
				fake.NamespaceAtPath("namespaces/foo/foo-test.yaml",
					core.Name("foo"), core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "test-only")),
			},
		},
		{
			name:     "no objects fails",
			wantErrs: fake.Errors(system.MissingRepoErrorCode),
		},
		{
			name: "invalid repo fails",
			objs: []ast.FileObject{
				fake.Repo(fake.RepoVersion("0.0.0")),
			},
			wantErrs: fake.Errors(system.UnsupportedRepoSpecVersionCode),
		},
		{
			name: "duplicate repos fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Repo(),
			},
			wantErrs: fake.Errors(status.MultipleSingletonsErrorCode),
		},
		{
			name: "top-level namespace fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces"),
			},
			wantErrs: fake.Errors(metadata.IllegalTopLevelNamespaceErrorCode),
		},
		{
			name: "namespace with child directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/bar"),
				fake.RoleAtPath("namespaces/bar/foo/rb.yaml"),
			},
			wantErrs: fake.Errors(validation.IllegalNamespaceSubdirectoryErrorCode),
		},
		{
			name: "CR without CRD fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo"),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.UnknownScopeAnnotationKey, csmetadata.UnknownScopeAnnotationValue),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{".":{},"f:group":{},"f:names":{".":{},"f:kind":{},"f:plural":{}},"f:scope":{}},"f:status":{".":{},"f:acceptedNames":{".":{},"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
				),
			},
			wantErrs: fake.Errors(status.UnknownKindErrorCode),
		},
		{
			name: "object in namespace directory without namespace fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.RoleAtPath("namespaces/foo/rb.yaml"),
			},
			wantErrs: fake.Errors(semantic.UnsyncableResourcesErrorCode),
		},
		{
			name: "object with deprecated GVK fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo"),
				fake.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "extensions",
						Version: "v1beta1",
						Kind:    "Deployment"},
					"namespaces/foo/deployment.yaml"),
			},
			wantErrs: fake.Errors(nonhierarchical.DeprecatedGroupKindErrorCode),
		},
		{
			name: "abstract resource with hierarchy mode none fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.HierarchyConfig(fake.HierarchyConfigResource(v1.HierarchyModeNone,
					kinds.RoleBinding().GroupVersion(),
					kinds.RoleBinding().Kind)),
				fake.RoleBindingAtPath("namespaces/rb.yaml"),
				fake.Namespace("namespaces/foo"),
			},
			wantErrs: fake.Errors(validation.IllegalAbstractNamespaceObjectKindErrorCode),
		},
		{
			name: "cluster-scoped objects with same name fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterRoleAtPath("cluster/cr1.yaml",
					core.Name("reader")),
				fake.ClusterRoleAtPath("cluster/cr2.yaml",
					core.Name("reader")),
			},
			wantErrs: fake.Errors(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "namespaces with same name fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/bar/foo"),
				fake.Namespace("namespaces/qux/foo"),
			},
			wantErrs: fake.Errors(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "invalid namespace name/directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo bar"),
			},
			wantErrs: fake.Errors(
				nonhierarchical.InvalidMetadataNameErrorCode,
				nonhierarchical.InvalidDirectoryNameErrorCode),
		},
		{
			name: "NamespaceSelector in namespace directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.NamespaceSelectorAtPath("namespaces/foo/bar/nss.yaml"),
				fake.Namespace("namespaces/foo/bar"),
			},
			wantErrs: fake.Errors(syntax.IllegalKindInNamespacesErrorCode),
		},
		{
			name: "NamespaceSelectors with cluster selector annotations fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.NamespaceSelector(
					core.Name("legacy-selected"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				fake.NamespaceSelector(
					core.Name("inline-selected"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod-cluster")),
			},
			wantErrs: fake.Errors(
				nonhierarchical.IllegalSelectorAnnotationErrorCode,
				nonhierarchical.IllegalSelectorAnnotationErrorCode),
		},
		{
			name: "cluster-scoped object with namespace selector fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/bar",
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "prod")),
			},
			wantErrs: fake.Errors(nonhierarchical.IllegalSelectorAnnotationErrorCode),
		},
		{
			name: "namespace-scoped objects under incorrect directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.RoleBindingAtPath("cluster/rb.yaml",
					core.Name("cluster-is-wrong")),
				fake.RoleBindingAtPath("clusterregistry/rb.yaml",
					core.Name("clusterregistry-is-wrong")),
				fake.RoleBindingAtPath("system/rb.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: fake.Errors(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "cluster-scoped objects under incorrect directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterRoleBindingAtPath("namespaces/foo/crb.yaml",
					core.Name("namespaces-is-wrong")),
				fake.ClusterRoleBindingAtPath("clusterregistry/rb.yaml",
					core.Name("clusterregistry-is-wrong")),
				fake.ClusterRoleBindingAtPath("system/rb.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: fake.Errors(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "cluster registry objects under incorrect directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterAtPath("namespaces/foo/cluster.yaml",
					core.Name("namespaces-is-wrong")),
				fake.ClusterAtPath("cluster/cluster.yaml",
					core.Name("cluster-is-wrong")),
				fake.ClusterAtPath("system/cluster.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: fake.Errors(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "system objects under incorrect directory fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.HierarchyConfigAtPath("namespaces/foo/hc.yaml",
					core.Name("namespaces-is-wrong")),
				fake.HierarchyConfigAtPath("cluster/hc.yaml",
					core.Name("cluster-is-wrong")),
				fake.HierarchyConfigAtPath("clusterregistry/hc.yaml",
					core.Name("clusterregistry-is-wrong")),
			},
			wantErrs: fake.Errors(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "illegal metadata on objects fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namesapces/foo",
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				fake.Role(
					core.Name("first"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "hello")),
				fake.Role(
					core.Name("second"),
					core.Annotation(csmetadata.DeclaredFieldsKey, "hello")),
			},
			wantErrs: fake.Errors(
				metadata.IllegalAnnotationDefinitionErrorCode,
				metadata.IllegalAnnotationDefinitionErrorCode,
				hnc.IllegalDepthLabelErrorCode),
		},
		{
			name: "duplicate object names from object inheritance fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo/bar/qux"),
				fake.RoleAtPath("namespaces/rb-1.yaml",
					core.Name("alice")),
				fake.RoleAtPath("namespaces/foo/bar/qux/rb-2.yaml",
					core.Name("alice")),
			},
			wantErrs: fake.Errors(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "objects with invalid names fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterRole(
					core.Name("")),
				fake.ClusterRole(
					core.Name("a/b")),
			},
			wantErrs: fake.Errors(
				nonhierarchical.MissingObjectNameErrorCode,
				nonhierarchical.InvalidMetadataNameErrorCode),
		},
		{
			name: "objects with disallowed fields fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.ClusterRole(
					core.ResourceVersion("123")),
				fake.ClusterRole(
					core.CreationTimeStamp(metav1.NewTime(time.Now()))),
			},
			wantErrs: fake.Errors(
				syntax.IllegalFieldsInConfigErrorCode,
				syntax.IllegalFieldsInConfigErrorCode),
		},
		{
			name: "HierarchyConfigs with invalid resource kinds fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.HierarchyConfig(
					fake.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.CustomResourceDefinitionV1Beta1().GroupVersion(), kinds.CustomResourceDefinitionV1Beta1().Kind),
					core.Name("crd-hc")),
				fake.HierarchyConfig(
					fake.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.Namespace().GroupVersion(), kinds.Namespace().Kind),
					core.Name("namespace-hc")),
				fake.HierarchyConfig(
					fake.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.Sync().GroupVersion(), kinds.Sync().Kind),
					core.Name("sync-hc")),
				fake.FileObject(crdUnstructured(t, kinds.Anvil()), "cluster/crd.yaml"),
				fake.Namespace("namespaces/foo"),
			},
			wantErrs: fake.Errors(
				hierarchyconfig.ClusterScopedResourceInHierarchyConfigErrorCode,
				hierarchyconfig.UnsupportedResourceInHierarchyConfigErrorCode,
				hierarchyconfig.UnsupportedResourceInHierarchyConfigErrorCode),
		},
		{
			name: "managed object in unmanaged namespace fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.Namespace("namespaces/foo",
					core.Annotation(csmetadata.ResourceManagementKey, csmetadata.ResourceManagementDisabled)),
				fake.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo")),
			},
			wantErrs: fake.Errors(nonhierarchical.ManagedResourceInUnmanagedNamespaceErrorCode),
		},
		{
			name: "RepoSync with invalid fields fails",
			objs: []ast.FileObject{
				fake.Repo(),
				fake.FileObject(fake.RepoSyncObjectV1Beta1("foo", "invalid"), "namespaces/foo/rs.yamo"),
			},
			wantErrs: fake.Errors(validate.InvalidSyncCode),
		},
	}

	converter, err := declared.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := discoverytest.Client(discoverytest.CRDsToAPIGroupResources(tc.discoveryCRDs))
			tc.options.BuildScoper = discovery.ScoperBuilder(dc)
			tc.options.PolicyDir = cmpath.RelativeSlash(dir)
			tc.options.Converter = converter

			got, errs := Hierarchical(tc.objs, tc.options)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got Hierarchical() error %v; want %v", errs, tc.wantErrs)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestUnstructured(t *testing.T) {
	testCases := []struct {
		name          string
		discoveryCRDs []*apiextensionsv1beta1.CustomResourceDefinition
		options       Options
		objs          []ast.FileObject
		want          []ast.FileObject
		wantErrs      status.MultiError
	}{
		{
			name: "no objects",
		},
		{
			name: "cluster-scoped object",
			objs: []ast.FileObject{
				fake.ClusterRoleAtPath("cluster/cr.yaml"),
			},
			want: []ast.FileObject{
				fake.ClusterRoleAtPath("cluster/cr.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/cr.yaml")),
			},
		},
		{
			name: "namespace-scoped objects",
			objs: []ast.FileObject{
				fake.RoleAtPath("role.yaml",
					core.Namespace("foo")),
				fake.RoleBindingAtPath("rb.yaml",
					core.Namespace("bar")),
			},
			want: []ast.FileObject{
				fake.RoleAtPath("role.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/role.yaml")),
				fake.RoleBindingAtPath("rb.yaml",
					core.Namespace("bar"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:roleRef":{"f:apiGroup":{},"f:kind":{},"f:name":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/rb.yaml")),
			},
		},
		{
			name: "CRD and CR",
			objs: []ast.FileObject{
				fake.FileObject(crdUnstructured(t, kinds.Anvil()), "crd.yaml"),
				fake.AnvilAtPath("anvil.yaml"),
			},
			want: []ast.FileObject{
				fake.FileObject(crdUnstructured(t, kinds.Anvil(),
					core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{"f:group":{},"f:names":{"f:kind":{},"f:plural":{}},"f:scope":{},"f:versions":{}},"f:status":{"f:acceptedNames":{"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/crd.yaml")), "crd.yaml"),
				fake.AnvilAtPath("anvil.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{".":{},"f:group":{},"f:names":{".":{},"f:kind":{},"f:plural":{}},"f:scope":{}},"f:status":{".":{},"f:acceptedNames":{".":{},"f:kind":{},"f:plural":{}},"f:conditions":{},"f:storedVersions":{}}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/anvil.yaml")),
			},
		},
		{
			name: "objects with cluster selectors",
			options: Options{
				ClusterName: "prod",
			},
			objs: []ast.FileObject{
				fake.Cluster(
					core.Name("prod"),
					core.Label("environment", "prod")),
				fake.FileObject(clusterSelector("prod-only", "environment", "prod"), "prod-only_cs.yaml"),
				fake.FileObject(clusterSelector("dev-only", "environment", "dev"), "dev-only_cs.yaml"),
				fake.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				fake.ClusterRole(
					core.Name("admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				fake.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.ClusterRole(
					core.Name("dev-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				fake.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Namespace("bookstore")),
				fake.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				fake.RoleAtPath("namespaces/bookstore/role-dev.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only"),
					core.Namespace("bookstore")),
				fake.Namespace("prod-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				fake.RoleAtPath("prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping")),
				fake.Namespace("dev-shipping",
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				fake.Role(
					core.Name("dev-sre"),
					core.Namespace("dev-shipping")),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/bookstore",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/namespace.yaml")),
				fake.Namespace("prod-shipping",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-shipping/namespace.yaml")),
				fake.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("admin"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/cluster-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-admin_cr.yaml")),
				fake.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configsync.gke.io/cluster-name-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-owner_cr.yaml")),
				fake.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/cluster-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/role-prod.yaml")),
				fake.RoleAtPath("prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-sre.yaml")),
			},
		},
		{
			name: "objects with namespace selectors",
			objs: []ast.FileObject{
				fake.FileObject(namespaceSelector("sre", "sre-supported", "true"), "sre_nss.yaml"),
				fake.FileObject(namespaceSelector("dev", "dev-supported", "true"), "dev_nss.yaml"),
				fake.Namespace("prod-shipping",
					core.Label("sre-supported", "true")),
				fake.Namespace("dev-shipping",
					core.Label("dev-supported", "true")),
				fake.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre")),
				fake.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev")),
			},
			want: []ast.FileObject{
				fake.Namespace("prod-shipping",
					core.Label("sre-supported", "true"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{"f:sre-supported":{}}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-shipping/namespace.yaml")),
				fake.Namespace("dev-shipping",
					core.Label("dev-supported", "true"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{"f:dev-supported":{}}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/dev-shipping/namespace.yaml")),
				fake.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/namespace-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/sre-role.yaml")),
				fake.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Namespace("dev-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/namespace-selector":{}},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/dev-role.yaml")),
			},
		},
		{
			name: "namespaced object gets assigned default namespace",
			options: Options{
				DefaultNamespace: "shipping",
			},
			objs: []ast.FileObject{
				fake.RoleAtPath("sre-role.yaml",
					core.Name("sre-role"),
					core.Namespace("")),
			},
			want: []ast.FileObject{
				fake.RoleAtPath("sre-role.yaml",
					core.Name("sre-role"),
					core.Namespace("shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/sre-role.yaml")),
			},
		},
		{
			name: "CR with management disabled that is missing its CRD",
			objs: []ast.FileObject{
				fake.Namespace("namespaces/foo"),
				fake.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "anthos.cloud.google.com",
						Version: "v1alpha1",
						Kind:    "Validator",
					},
					"foo/validator.yaml",
					core.Namespace("foo"),
					core.Annotation(csmetadata.ResourceManagementKey, csmetadata.ResourceManagementDisabled)),
			},
			want: []ast.FileObject{
				fake.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml")),
				fake.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "anthos.cloud.google.com",
						Version: "v1alpha1",
						Kind:    "Validator",
					},
					"foo/validator.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1alpha1"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{".":{},"f:configmanagement.gke.io/managed":{}},"f:labels":{}}}`),
					core.Annotation(csmetadata.ResourceManagementKey, csmetadata.ResourceManagementDisabled),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/foo/validator.yaml")),
			},
		},
		{
			name: "duplicate objects fails",
			objs: []ast.FileObject{
				fake.Role(
					core.Name("alice"),
					core.Namespace("shipping")),
				fake.Role(
					core.Name("alice"),
					core.Namespace("shipping")),
			},
			wantErrs: fake.Errors(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "removing CRD while in-use fails",
			options: Options{
				PreviousCRDs: []*apiextensionsv1beta1.CustomResourceDefinition{
					crdObject(kinds.Anvil()),
				},
			},
			objs: []ast.FileObject{
				fake.AnvilAtPath("anvil.yaml"),
			},
			wantErrs: fake.Errors(nonhierarchical.UnsupportedCRDRemovalErrorCode),
		},
	}

	converter, err := declared.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := discoverytest.Client(discoverytest.CRDsToAPIGroupResources(tc.discoveryCRDs))
			tc.options.BuildScoper = discovery.ScoperBuilder(dc)
			tc.options.PolicyDir = cmpath.RelativeSlash(dir)
			tc.options.Converter = converter

			got, errs := Unstructured(tc.objs, tc.options)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got Unstructured() error %v; want %v", errs, tc.wantErrs)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
