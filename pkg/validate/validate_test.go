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
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
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
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
	"kpt.dev/configsync/pkg/status"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/discoverytest"
	"kpt.dev/configsync/pkg/testing/openapitest"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const dir = "acme"

func validRootSync(name, path string, opts ...core.MetaMutator) ast.FileObject {
	rs := k8sobjects.RootSyncObjectV1Beta1(name)
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo: "https://github.com/test/abc",
		Auth: "none",
	}
	for _, opt := range opts {
		opt(rs)
	}
	return k8sobjects.FileObject(rs, path)
}

func validRepoSync(ns, name, path string, opts ...core.MetaMutator) ast.FileObject {
	rs := k8sobjects.RepoSyncObjectV1Beta1(ns, name)
	rs.Spec.SourceType = configsync.GitSource
	rs.Spec.Git = &v1beta1.Git{
		Repo: "https://github.com/test/abc",
		Auth: "none",
	}
	for _, opt := range opts {
		opt(rs)
	}
	return k8sobjects.FileObject(rs, path)
}

func clusterSelector(name, key, value string) *v1.ClusterSelector {
	cs := k8sobjects.ClusterSelectorObject(core.Name(name))
	cs.Spec.Selector.MatchLabels = map[string]string{key: value}
	return cs
}

func namespaceSelector(name, key, value, mode string) *v1.NamespaceSelector {
	ns := k8sobjects.NamespaceSelectorObject(core.Name(name))
	ns.Spec.Selector.MatchLabels = map[string]string{key: value}
	ns.Spec.Mode = mode
	return ns
}

func TestHierarchical(t *testing.T) {
	testCases := []struct {
		name          string
		discoveryCRDs []*apiextensionsv1.CustomResourceDefinition
		options       Options
		objs          []ast.FileObject
		want          []ast.FileObject
		wantErrs      status.MultiError
	}{
		{
			name: "only a valid repo",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
			},
		},
		{
			name: "namespace and object in it",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/role.yaml")),
			},
		},
		{
			name: "abstract namespaces with object inheritance",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/bar/foo"),
				k8sobjects.Namespace("namespaces/bar/qux/lym"),
				k8sobjects.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first")),
				k8sobjects.RoleAtPath("namespaces/bar/qux/role.yaml",
					core.Name("second")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/bar/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first"),
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/role.yaml")),
				k8sobjects.Namespace("namespaces/bar/qux/lym",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/qux/lym/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "2"),
					core.Label("qux.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("lym.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleAtPath("namespaces/bar/role.yaml",
					core.Name("first"),
					core.Namespace("lym"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/role.yaml")),
				k8sobjects.RoleAtPath("namespaces/bar/qux/role.yaml",
					core.Name("second"),
					core.Namespace("lym"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/qux/role.yaml")),
			},
		},
		{
			name: "CRD v1 and CR",
			options: Options{
				Scheme: core.Scheme,
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.AnvilCRDv1AtPath("cluster/crd.yaml"),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				k8sobjects.AnvilCRDv1AtPath("cluster/crd.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/crd.yaml")),
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml")),
			},
		},
		{
			name: "CRD v1beta1 and CR",
			options: Options{
				Scheme: core.Scheme,
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.AnvilCRDv1beta1AtPath("cluster/crd.yaml"),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				k8sobjects.AnvilCRDv1beta1AtPath("cluster/crd.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/crd.yaml")),
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml")),
			},
		},
		{
			name: "CR in repo and CRD on API server",
			discoveryCRDs: []*apiextensionsv1.CustomResourceDefinition{
				k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.ClusterScoped),
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.AnvilAtPath("cluster/anvil.yaml", core.Namespace("")),
			},
			want: []ast.FileObject{
				k8sobjects.AnvilAtPath("cluster/anvil.yaml", core.Namespace(""),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/anvil.yaml")),
			},
		},
		{
			name: "CR without CRD and allow unknown kinds",
			options: Options{
				AllowUnknownKinds: true,
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.UnknownScopeAnnotationKey, csmetadata.UnknownScopeAnnotationValue),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml")),
			},
		},
		{
			name: "objects with cluster selectors",
			options: Options{
				ClusterName: "prod",
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterAtPath("clusterregistry/cluster-dev.yaml",
					core.Name("dev"),
					core.Label("environment", "dev")),
				k8sobjects.ClusterAtPath("clusterregistry/cluster-prod.yaml",
					core.Name("prod"),
					core.Label("environment", "prod")),
				k8sobjects.FileObject(clusterSelector("prod-only", "environment", "prod"), "clusterregistry/prod-only_cs.yaml"),
				k8sobjects.FileObject(clusterSelector("dev-only", "environment", "dev"), "clusterregistry/dev-only_cs.yaml"),
				// Should be selected
				k8sobjects.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("prod-admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Namespace("bookstore")),
				k8sobjects.Namespace("namespaces/prod-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.RoleAtPath("namespaces/prod-shipping/prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping")),
				k8sobjects.RoleAtPath("namespaces/prod-shipping/prod-swe.yaml",
					core.Name("prod-swe")),
				// Should not be selected
				k8sobjects.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.ClusterRoleAtPath("cluster/dev-admin_cr.yaml",
					core.Name("dev-admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				k8sobjects.ClusterRoleAtPath("cluster/dev-owner_cr.yaml",
					core.Name("dev-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.RoleAtPath("namespaces/dev-abstract.yaml",
					core.Name("abstract"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-dev.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev"),
					core.Namespace("bookstore")),
				k8sobjects.Namespace("namespaces/dev-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.RoleAtPath("namespaces/dev-shipping/dev-sre.yaml",
					core.Name("dev-sre"),
					core.Namespace("dev-shipping")),
				k8sobjects.RoleAtPath("namespaces/dev-shipping/dev-swe.yaml",
					core.Name("dev-swe")),
			},
			want: []ast.FileObject{
				k8sobjects.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("prod-admin"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-admin_cr.yaml")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-dev_cr.yaml",
					core.Name("prod-dev"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-dev_cr.yaml")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-owner_cr.yaml")),
				k8sobjects.Namespace("namespaces/bookstore",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bookstore.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/role-prod.yaml")),
				k8sobjects.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-abstract.yaml")),
				k8sobjects.Namespace("namespaces/prod-shipping",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("prod-shipping.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleAtPath("namespaces/prod-shipping/prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/prod-sre.yaml")),
				k8sobjects.RoleAtPath("namespaces/prod-shipping/prod-swe.yaml",
					core.Name("prod-swe"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-shipping/prod-swe.yaml")),
				k8sobjects.RoleAtPath("namespaces/prod-abstract.yaml",
					core.Name("abstract"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/prod-abstract.yaml")),
			},
		},
		{
			name: "object with namespace selector",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.FileObject(namespaceSelector("sre-supported", "env", "prod", v1.NSSelectorStaticMode),
					"namespaces/bar/ns-selector.yaml"),
				k8sobjects.FileObject(namespaceSelector("dev-supported", "env", "test", v1.NSSelectorStaticMode),
					"namespaces/bar/ns-selector.yaml"),
				k8sobjects.RoleBindingAtPath("namespaces/bar/sre-rb.yaml",
					core.Name("rb"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre-supported")),
				k8sobjects.RoleBindingAtPath("namespaces/bar/dev-rb.yaml",
					core.Name("rb"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev-supported")),
				k8sobjects.Namespace("namespaces/bar/prod-ns",
					core.Label("env", "prod")),
				k8sobjects.Namespace("namespaces/bar/test-ns",
					core.Label("env", "test")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/bar/prod-ns",
					core.Label("env", "prod"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/prod-ns/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("prod-ns.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleBindingAtPath("namespaces/bar/sre-rb.yaml",
					core.Name("rb"),
					core.Namespace("prod-ns"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre-supported"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/sre-rb.yaml")),
				k8sobjects.Namespace("namespaces/bar/test-ns",
					core.Label("env", "test"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/test-ns/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("test-ns.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.RoleBindingAtPath("namespaces/bar/dev-rb.yaml",
					core.Name("rb"),
					core.Namespace("test-ns"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev-supported"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/dev-rb.yaml")),
			},
		},
		{
			name: "abstract namespaces with shared names",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/bar/foo"),
				k8sobjects.Namespace("namespaces/foo/bar"),
				k8sobjects.Namespace("namespaces/foo/foo/qux"),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/bar/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bar/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.Namespace("namespaces/foo/bar",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/bar/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("bar.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.Namespace("namespaces/foo/foo/qux",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/foo/qux/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "1"),
					core.Label("qux.tree.hnc.x-k8s.io/depth", "0")),
			},
		},
		{
			name: "system namespaces",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/default"),
				k8sobjects.Namespace("namespaces/kube-system"),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/default",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/default/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Label("default.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.Namespace("namespaces/kube-system",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/kube-system/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Label("kube-system.tree.hnc.x-k8s.io/depth", "0")),
			},
		},
		{
			name: "namespace without declared-fields annotation when webhook is enabled",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/test-namespace"),
			},
			options: Options{
				WebhookEnabled: true,
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/test-namespace",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/test-namespace/namespace.yaml"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/source-path":{},"f:hnc.x-k8s.io/managed-by":{}},"f:labels":{"f:configsync.gke.io/declared-version":{},"f:test-namespace.tree.hnc.x-k8s.io/depth":{}}},"f:spec":{},"f:status":{}}`),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("test-namespace.tree.hnc.x-k8s.io/depth", "0")),
			},
		},
		{
			name: "objects in non-namespace subdirectories",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.HierarchyConfigAtPath("system/sub/hc.yaml"),
				k8sobjects.ClusterAtPath("clusterregistry/foo/cluster.yaml"),
				k8sobjects.ClusterRoleBindingAtPath("cluster/foo/crb.yaml"),
			},
			want: []ast.FileObject{
				k8sobjects.ClusterRoleBindingAtPath("cluster/foo/crb.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/foo/crb.yaml")),
			},
		},
		{
			name: "same namespace resource with different cluster selector",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.FileObject(clusterSelector("prod-only", "environment", "prod"), "clusterregistry/prod-only_cs.yaml"),
				k8sobjects.FileObject(clusterSelector("dev-only", "environment", "dev"), "clusterregistry/dev-only_cs.yaml"),
				k8sobjects.NamespaceAtPath("namespaces/foo/foo-prod.yaml",
					core.Name("foo"), core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				k8sobjects.NamespaceAtPath("namespaces/foo/foo-dev.yaml",
					core.Name("foo"), core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				k8sobjects.NamespaceAtPath("namespaces/foo/foo-stg.yaml",
					core.Name("foo"), core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "stg-only")),
				k8sobjects.NamespaceAtPath("namespaces/foo/foo-test.yaml",
					core.Name("foo"), core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "test-only")),
			},
		},
		{
			name:     "no objects fails",
			wantErrs: status.FakeMultiError(system.MissingRepoErrorCode),
		},
		{
			name: "invalid repo fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(k8sobjects.RepoVersion("0.0.0")),
			},
			wantErrs: status.FakeMultiError(system.UnsupportedRepoSpecVersionCode),
		},
		{
			name: "duplicate repos fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Repo(),
			},
			wantErrs: status.FakeMultiError(status.MultipleSingletonsErrorCode),
		},
		{
			name: "top-level namespace fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces"),
			},
			wantErrs: status.FakeMultiError(metadata.IllegalTopLevelNamespaceErrorCode),
		},
		{
			name: "namespace with child directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/bar"),
				k8sobjects.RoleAtPath("namespaces/bar/foo/rb.yaml"),
			},
			wantErrs: status.FakeMultiError(validation.IllegalNamespaceSubdirectoryErrorCode),
		},
		{
			name: "CR without CRD fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, csmetadata.ManagedByValue),
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.AnvilAtPath("namespaces/foo/anvil.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.UnknownScopeAnnotationKey, csmetadata.UnknownScopeAnnotationValue),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/anvil.yaml"),
				),
			},
			wantErrs: status.FakeMultiError(status.UnknownKindErrorCode),
		},
		{
			name: "object in namespace directory without namespace fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.RoleAtPath("namespaces/foo/rb.yaml"),
			},
			wantErrs: status.FakeMultiError(semantic.UnsyncableResourcesErrorCode),
		},
		{
			name: "object with deprecated GVK fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "extensions",
						Version: "v1beta1",
						Kind:    "Deployment"},
					"namespaces/foo/deployment.yaml"),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.DeprecatedGroupKindErrorCode),
		},
		{
			name: "abstract resource with hierarchy mode none fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.HierarchyConfig(k8sobjects.HierarchyConfigResource(v1.HierarchyModeNone,
					kinds.RoleBinding().GroupVersion(),
					kinds.RoleBinding().Kind)),
				k8sobjects.RoleBindingAtPath("namespaces/rb.yaml"),
				k8sobjects.Namespace("namespaces/foo"),
			},
			wantErrs: status.FakeMultiError(validation.IllegalAbstractNamespaceObjectKindErrorCode),
		},
		{
			name: "cluster-scoped objects with same name fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterRoleAtPath("cluster/cr1.yaml",
					core.Name("reader")),
				k8sobjects.ClusterRoleAtPath("cluster/cr2.yaml",
					core.Name("reader")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "namespaces with same name fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/bar/foo"),
				k8sobjects.Namespace("namespaces/qux/foo"),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "invalid namespace name/directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo bar"),
			},
			wantErrs: status.FakeMultiError(
				nonhierarchical.InvalidMetadataNameErrorCode,
				nonhierarchical.InvalidDirectoryNameErrorCode),
		},
		{
			name: "NamespaceSelector in namespace directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.NamespaceSelectorAtPath("namespaces/foo/bar/nss.yaml"),
				k8sobjects.Namespace("namespaces/foo/bar"),
			},
			wantErrs: status.FakeMultiError(syntax.IllegalKindInNamespacesErrorCode),
		},
		{
			name: "NamespaceSelectors with cluster selector annotations fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.NamespaceSelector(
					core.Name("legacy-selected"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				k8sobjects.NamespaceSelector(
					core.Name("inline-selected"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod-cluster")),
			},
			wantErrs: status.FakeMultiError(
				nonhierarchical.IllegalSelectorAnnotationErrorCode,
				nonhierarchical.IllegalSelectorAnnotationErrorCode),
		},
		{
			name: "cluster-scoped object with namespace selector fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/bar",
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "prod")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.IllegalSelectorAnnotationErrorCode),
		},
		{
			name: "namespace-scoped objects under incorrect directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.RoleBindingAtPath("cluster/rb.yaml",
					core.Name("cluster-is-wrong")),
				k8sobjects.RoleBindingAtPath("clusterregistry/rb.yaml",
					core.Name("clusterregistry-is-wrong")),
				k8sobjects.RoleBindingAtPath("system/rb.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: status.FakeMultiError(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "cluster-scoped objects under incorrect directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterRoleBindingAtPath("namespaces/foo/crb.yaml",
					core.Name("namespaces-is-wrong")),
				k8sobjects.ClusterRoleBindingAtPath("clusterregistry/rb.yaml",
					core.Name("clusterregistry-is-wrong")),
				k8sobjects.ClusterRoleBindingAtPath("system/rb.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: status.FakeMultiError(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "cluster registry objects under incorrect directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterAtPath("namespaces/foo/cluster.yaml",
					core.Name("namespaces-is-wrong")),
				k8sobjects.ClusterAtPath("cluster/cluster.yaml",
					core.Name("cluster-is-wrong")),
				k8sobjects.ClusterAtPath("system/cluster.yaml",
					core.Name("system-is-wrong")),
			},
			wantErrs: status.FakeMultiError(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "system objects under incorrect directory fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.HierarchyConfigAtPath("namespaces/foo/hc.yaml",
					core.Name("namespaces-is-wrong")),
				k8sobjects.HierarchyConfigAtPath("cluster/hc.yaml",
					core.Name("cluster-is-wrong")),
				k8sobjects.HierarchyConfigAtPath("clusterregistry/hc.yaml",
					core.Name("clusterregistry-is-wrong")),
			},
			wantErrs: status.FakeMultiError(
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode,
				validation.IncorrectTopLevelDirectoryErrorCode),
		},
		{
			name: "illegal metadata on objects fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namesapces/foo",
					core.Label("foo.tree.hnc.x-k8s.io/depth", "0")),
				k8sobjects.Role(
					core.Name("first"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "hello")),
				k8sobjects.Role(
					core.Name("second"),
					core.Annotation(csmetadata.DeclaredFieldsKey, "hello")),
			},
			wantErrs: status.FakeMultiError(
				metadata.IllegalAnnotationDefinitionErrorCode,
				metadata.IllegalAnnotationDefinitionErrorCode,
				hnc.IllegalDepthLabelErrorCode),
		},
		{
			name: "duplicate object names from object inheritance fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo/bar/qux"),
				k8sobjects.RoleAtPath("namespaces/rb-1.yaml",
					core.Name("alice")),
				k8sobjects.RoleAtPath("namespaces/foo/bar/qux/rb-2.yaml",
					core.Name("alice")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "objects with invalid names fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterRole(
					core.Name("")),
				k8sobjects.ClusterRole(
					core.Name("a/b")),
			},
			wantErrs: status.FakeMultiError(
				nonhierarchical.MissingObjectNameErrorCode,
				nonhierarchical.InvalidMetadataNameErrorCode),
		},
		{
			name: "objects with disallowed fields fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.ClusterRole(
					core.ResourceVersion("123")),
				k8sobjects.ClusterRole(
					core.CreationTimeStamp(metav1.NewTime(time.Now()))),
			},
			wantErrs: status.FakeMultiError(
				syntax.IllegalFieldsInConfigErrorCode,
				syntax.IllegalFieldsInConfigErrorCode),
		},
		{
			name: "HierarchyConfigs with invalid resource kinds fails",
			options: Options{
				Scheme: core.Scheme,
			},
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.HierarchyConfig(
					k8sobjects.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.CustomResourceDefinitionV1Beta1().GroupVersion(), kinds.CustomResourceDefinitionV1Beta1().Kind),
					core.Name("crd-hc")),
				k8sobjects.HierarchyConfig(
					k8sobjects.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.Namespace().GroupVersion(), kinds.Namespace().Kind),
					core.Name("namespace-hc")),
				k8sobjects.HierarchyConfig(
					k8sobjects.HierarchyConfigResource(v1.HierarchyModeInherit,
						kinds.Sync().GroupVersion(), kinds.Sync().Kind),
					core.Name("sync-hc")),
				k8sobjects.AnvilCRDv1AtPath("cluster/crd.yaml"),
				k8sobjects.Namespace("namespaces/foo"),
			},
			wantErrs: status.FakeMultiError(
				hierarchyconfig.ClusterScopedResourceInHierarchyConfigErrorCode,
				hierarchyconfig.UnsupportedResourceInHierarchyConfigErrorCode,
				hierarchyconfig.UnsupportedResourceInHierarchyConfigErrorCode),
		},
		{
			name: "managed object in unmanaged namespace fails",
			objs: []ast.FileObject{
				k8sobjects.Repo(),
				k8sobjects.Namespace("namespaces/foo",
					csmetadata.WithManagementMode(csmetadata.ManagementDisabled)),
				k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
					core.Namespace("foo")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.ManagedResourceInUnmanagedNamespaceErrorCode),
		},
		{
			name: "RepoSync manages itself",
			options: Options{
				Scope:    "bookstore",
				SyncName: "repo-sync",
			},
			objs: []ast.FileObject{
				k8sobjects.NamespaceAtPath("namespaces/bookstore/ns.yaml"),
				k8sobjects.Repo(),
				validRepoSync("bookstore", "repo-sync", "namespaces/bookstore/rs.yaml"),
			},
			wantErrs: status.FakeMultiError(validate.SelfReconcileErrorCode),
		},
		{
			name: "RepoSync manages other RepoSync object",
			options: Options{
				Scope:    "bookstore",
				SyncName: "repo-sync",
			},
			objs: []ast.FileObject{
				k8sobjects.NamespaceAtPath("namespaces/bookstore/ns.yaml"),
				k8sobjects.Repo(),
				validRepoSync("bookstore", "repo-sync-1", "namespaces/bookstore/rs.yaml"),
			},
			want: []ast.FileObject{
				k8sobjects.NamespaceAtPath("namespaces/bookstore/ns.yaml",
					core.Annotation(csmetadata.SourcePathAnnotationKey, "acme/namespaces/bookstore/ns.yaml"),
					core.Annotation(csmetadata.HNCManagedBy, configmanagement.GroupName),
					core.Label("bookstore"+csmetadata.DepthSuffix, "0"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
				),
				validRepoSync("bookstore", "repo-sync-1", "namespaces/bookstore/rs.yaml",
					core.Annotation(csmetadata.SourcePathAnnotationKey, "acme/namespaces/bookstore/rs.yaml"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
				)},
		},
	}

	converter, err := openapitest.ValueConverterForTest()
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
				t.Error(diff)
			}
		})
	}
}

func TestUnstructured(t *testing.T) {
	testCases := []struct {
		name                                   string
		discoveryCRDs                          []*apiextensionsv1.CustomResourceDefinition
		options                                Options
		objs                                   []ast.FileObject
		onClusterObjects                       []client.Object
		originalDynamicNSSelectorEnabled       bool
		want                                   []ast.FileObject
		wantErrs                               status.MultiError
		wantDynamicNSSelectorEnabledAnnotation bool
	}{
		{
			name: "no objects",
		},
		{
			name:    "cluster-scoped object",
			options: Options{Scope: declared.RootScope},
			objs: []ast.FileObject{
				k8sobjects.ClusterRoleAtPath("cluster/cr.yaml"),
			},
			want: []ast.FileObject{
				k8sobjects.ClusterRoleAtPath("cluster/cr.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/cr.yaml")),
			},
		},
		{
			name:    "namespace-scoped objects",
			options: Options{Scope: declared.Scope("foo")},
			objs: []ast.FileObject{
				k8sobjects.RoleAtPath("role.yaml",
					core.Namespace("foo")),
				k8sobjects.RoleBindingAtPath("rb.yaml",
					core.Namespace("bar")),
			},
			want: []ast.FileObject{
				k8sobjects.RoleAtPath("role.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/role.yaml")),
				k8sobjects.RoleBindingAtPath("rb.yaml",
					core.Namespace("bar"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/rb.yaml")),
			},
		},
		{
			name: "CRD v1 and CR",
			options: Options{
				Scope:  declared.RootScope,
				Scheme: core.Scheme,
			},
			objs: []ast.FileObject{
				k8sobjects.AnvilCRDv1AtPath("crd.yaml"),
				k8sobjects.AnvilAtPath("anvil.yaml"),
			},
			want: []ast.FileObject{
				k8sobjects.AnvilCRDv1AtPath("crd.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/crd.yaml")),
				k8sobjects.AnvilAtPath("anvil.yaml",
					core.Namespace(metav1.NamespaceDefault),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/anvil.yaml")),
			},
		},
		{
			name: "CRD v1beta1 and CR",
			options: Options{
				Scope:  declared.RootScope,
				Scheme: core.Scheme,
			},
			objs: []ast.FileObject{
				k8sobjects.AnvilCRDv1beta1AtPath("crd.yaml"),
				k8sobjects.AnvilAtPath("anvil.yaml"),
			},
			want: []ast.FileObject{
				k8sobjects.AnvilCRDv1beta1AtPath("crd.yaml",
					core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/crd.yaml")),
				k8sobjects.AnvilAtPath("anvil.yaml",
					core.Namespace(metav1.NamespaceDefault),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/anvil.yaml")),
			},
		},
		{
			name: "namespace without declared-fields annotation when webhook is enabled",
			objs: []ast.FileObject{
				k8sobjects.Namespace("test-namespace"),
			},
			options: Options{
				Scope:          declared.RootScope,
				WebhookEnabled: true,
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("test-namespace",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/test-namespace/namespace.yaml"),
					core.Annotation(csmetadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/source-path":{}},"f:labels":{"f:configsync.gke.io/declared-version":{}}},"f:spec":{},"f:status":{}}`)),
			},
		},
		{
			name: "objects with cluster selectors",
			options: Options{
				ClusterName: "prod",
				Scope:       declared.RootScope,
			},
			objs: []ast.FileObject{
				k8sobjects.Cluster(
					core.Name("prod"),
					core.Label("environment", "prod")),
				k8sobjects.FileObject(clusterSelector("prod-only", "environment", "prod"), "prod-only_cs.yaml"),
				k8sobjects.FileObject(clusterSelector("dev-only", "environment", "dev"), "dev-only_cs.yaml"),
				k8sobjects.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only")),
				k8sobjects.ClusterRole(
					core.Name("admin"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.ClusterRole(
					core.Name("dev-owner"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "dev")),
				k8sobjects.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Namespace("bookstore")),
				k8sobjects.Namespace("namespaces/bookstore",
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-dev.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only"),
					core.Namespace("bookstore")),
				k8sobjects.Namespace("prod-shipping",
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod")),
				k8sobjects.RoleAtPath("prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping")),
				k8sobjects.Namespace("dev-shipping",
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "dev-only")),
				k8sobjects.Role(
					core.Name("dev-sre"),
					core.Namespace("dev-shipping")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/bookstore",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/namespace.yaml")),
				k8sobjects.Namespace("prod-shipping",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-shipping/namespace.yaml")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-admin_cr.yaml",
					core.Name("admin"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-admin_cr.yaml")),
				k8sobjects.ClusterRoleAtPath("cluster/prod-owner_cr.yaml",
					core.Name("prod-owner"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.ClusterNameSelectorAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/cluster/prod-owner_cr.yaml")),
				k8sobjects.RoleAtPath("namespaces/bookstore/role-prod.yaml",
					core.Name("role"),
					core.Namespace("bookstore"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.LegacyClusterSelectorAnnotationKey, "prod-only"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/bookstore/role-prod.yaml")),
				k8sobjects.RoleAtPath("prod-sre.yaml",
					core.Name("prod-sre"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.ClusterNameAnnotationKey, "prod"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-sre.yaml")),
			},
		},
		{
			name:    "objects with namespace selectors (static mode)",
			options: Options{Scope: declared.RootScope, SyncName: configsync.RootSyncName},
			objs: []ast.FileObject{
				k8sobjects.FileObject(namespaceSelector("sre", "sre-supported", "true", v1.NSSelectorStaticMode), "sre_nss.yaml"),
				k8sobjects.FileObject(namespaceSelector("dev", "dev-supported", "true", v1.NSSelectorStaticMode), "dev_nss.yaml"),
				k8sobjects.Namespace("prod-shipping",
					core.Label("sre-supported", "true")),
				k8sobjects.Namespace("dev-shipping",
					core.Label("dev-supported", "true")),
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre")),
				k8sobjects.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("prod-shipping",
					core.Label("sre-supported", "true"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-shipping/namespace.yaml")),
				k8sobjects.Namespace("dev-shipping",
					core.Label("dev-supported", "true"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/dev-shipping/namespace.yaml")),
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/sre-role.yaml")),
				k8sobjects.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Namespace("dev-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/dev-role.yaml")),
			},
		},
		{
			name:    "objects with namespace selectors (dynamic mode)",
			options: Options{Scope: declared.RootScope, SyncName: configsync.RootSyncName},
			objs: []ast.FileObject{
				k8sobjects.FileObject(namespaceSelector("sre", "sre-supported", "true", v1.NSSelectorDynamicMode), "sre_nss.yaml"),
				k8sobjects.FileObject(namespaceSelector("dev", "dev-supported", "true", v1.NSSelectorDynamicMode), "dev_nss.yaml"),
				k8sobjects.Namespace("prod-shipping",
					core.Label("sre-supported", "true")),
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre")),
				k8sobjects.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev")),
			},
			onClusterObjects: []client.Object{
				k8sobjects.NamespaceObject("dev-shipping", core.Label("dev-supported", "true")),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("prod-shipping",
					core.Label("sre-supported", "true"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/prod-shipping/namespace.yaml")),
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("role"),
					core.Namespace("prod-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "sre"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/sre-role.yaml")),
				k8sobjects.RoleAtPath("dev-role.yaml",
					core.Name("role"),
					core.Namespace("dev-shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.NamespaceSelectorAnnotationKey, "dev"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/dev-role.yaml")),
			},
			wantDynamicNSSelectorEnabledAnnotation: true,
		},
		{
			name: "namespaced object gets assigned default namespace",
			options: Options{
				Scope:    "shipping",
				SyncName: "repo-sync",
			},
			objs: []ast.FileObject{
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("sre-role"),
					core.Namespace("")),
			},
			want: []ast.FileObject{
				k8sobjects.RoleAtPath("sre-role.yaml",
					core.Name("sre-role"),
					core.Namespace("shipping"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/sre-role.yaml")),
			},
		},
		{
			name:    "CR with management disabled that is missing its CRD",
			options: Options{Scope: declared.RootScope},
			objs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "anthos.cloud.google.com",
						Version: "v1alpha1",
						Kind:    "Validator",
					},
					"foo/validator.yaml",
					core.Namespace("foo"),
					csmetadata.WithManagementMode(csmetadata.ManagementDisabled)),
			},
			want: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo",
					core.Label(csmetadata.DeclaredVersionLabel, "v1"),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/namespaces/foo/namespace.yaml")),
				k8sobjects.UnstructuredAtPath(
					schema.GroupVersionKind{
						Group:   "anthos.cloud.google.com",
						Version: "v1alpha1",
						Kind:    "Validator",
					},
					"foo/validator.yaml",
					core.Namespace("foo"),
					core.Label(csmetadata.DeclaredVersionLabel, "v1alpha1"),
					csmetadata.WithManagementMode(csmetadata.ManagementDisabled),
					core.Annotation(csmetadata.SourcePathAnnotationKey, dir+"/foo/validator.yaml")),
			},
		},
		{
			name:    "duplicate objects fails",
			options: Options{Scope: declared.Scope("shipping")},
			objs: []ast.FileObject{
				k8sobjects.Role(
					core.Name("alice"),
					core.Namespace("shipping")),
				k8sobjects.Role(
					core.Name("alice"),
					core.Namespace("shipping")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.NameCollisionErrorCode),
		},
		{
			name: "removing CRD while in-use fails",
			options: Options{
				PreviousCRDs: []*apiextensionsv1.CustomResourceDefinition{
					k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.ClusterScoped),
				},
				Scope: declared.RootScope,
			},
			objs: []ast.FileObject{
				k8sobjects.AnvilAtPath("anvil.yaml"),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.UnsupportedCRDRemovalErrorCode),
		},
		{
			name: "RootSync manages itself",
			options: Options{
				Scope:    declared.RootScope,
				SyncName: "root-sync",
			},
			objs: []ast.FileObject{
				validRootSync("root-sync", "rs.yaml"),
			},
			wantErrs: status.FakeMultiError(validate.SelfReconcileErrorCode),
		},
		{
			name: "RootSync manages other RootSync object",
			options: Options{
				Scope:    declared.RootScope,
				SyncName: "root-sync",
			},
			objs: []ast.FileObject{
				validRootSync("root-sync-1", "rs.yaml"),
			},
			want: []ast.FileObject{validRootSync("root-sync-1", "rs.yaml",
				core.Annotation(csmetadata.SourcePathAnnotationKey, "acme/rs.yaml"),
				core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
			)},
		},
		{
			name: "RepoSync manages itself",
			options: Options{
				Scope:    "bookstore",
				SyncName: "repo-sync",
			},
			objs: []ast.FileObject{
				validRepoSync("bookstore", "repo-sync", "rs.yaml"),
			},
			wantErrs: status.FakeMultiError(validate.SelfReconcileErrorCode),
		},
		{
			name: "RepoSync manages other RepoSync object",
			options: Options{
				Scope:    "bookstore",
				SyncName: "repo-sync",
			},
			objs: []ast.FileObject{
				validRepoSync("bookstore", "repo-sync-1", "rs.yaml"),
			},
			want: []ast.FileObject{validRepoSync("bookstore", "repo-sync-1", "rs.yaml",
				core.Annotation(csmetadata.SourcePathAnnotationKey, "acme/rs.yaml"),
				core.Label(csmetadata.DeclaredVersionLabel, "v1beta1"),
			)},
		},
	}

	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dc := discoverytest.Client(discoverytest.CRDsToAPIGroupResources(tc.discoveryCRDs))
			tc.options.BuildScoper = discovery.ScoperBuilder(dc)
			tc.options.PolicyDir = cmpath.RelativeSlash(dir)
			tc.options.Converter = converter
			tc.options.AllowAPICall = true
			tc.options.DynamicNSSelectorEnabled = tc.originalDynamicNSSelectorEnabled
			tc.options.NSControllerState = &namespacecontroller.State{}
			tc.options.FieldManager = configsync.FieldManager

			s := runtime.NewScheme()
			if err := corev1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}
			if err := v1beta1.AddToScheme(s); err != nil {
				t.Fatal(err)
			}

			rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName,
				core.Annotation(csmetadata.DynamicNSSelectorEnabledAnnotationKey, strconv.FormatBool(tc.originalDynamicNSSelectorEnabled)))
			tc.onClusterObjects = append(tc.onClusterObjects, rs)
			fakeClient := syncerFake.NewClient(t, s, tc.onClusterObjects...)

			got, errs := Unstructured(context.Background(), fakeClient, tc.objs, tc.options)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got Unstructured() error %v; want %v", errs, tc.wantErrs)
			}
			testutil.AssertEqual(t, tc.want, got)

			updatedDynamicNSSelectorEnabled, err := dynamicNSSelectorEnabled(fakeClient)
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, tc.wantDynamicNSSelectorEnabledAnnotation, updatedDynamicNSSelectorEnabled)

		})
	}
}

func dynamicNSSelectorEnabled(fakeClient client.Client) (bool, error) {
	rs := &v1beta1.RootSync{}
	if err := fakeClient.Get(context.Background(), client.ObjectKey{Namespace: configsync.ControllerNamespace, Name: configsync.RootSyncName}, rs); err != nil {
		return false, err
	}
	return strconv.ParseBool(rs.Annotations[csmetadata.DynamicNSSelectorEnabledAnnotationKey])
}
