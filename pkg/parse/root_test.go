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

package parse

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	applierfake "kpt.dev/configsync/pkg/applier/fake"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	fsfake "kpt.dev/configsync/pkg/importer/filesystem/fake"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/conflict"
	remediatorfake "kpt.dev/configsync/pkg/remediator/fake"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	syncertest "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/openapitest"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"kpt.dev/configsync/pkg/testing/testmetrics"
	discoveryutil "kpt.dev/configsync/pkg/util/discovery"
	webhookconfiguration "kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	rootSyncName       = "my-rs"
	rootReconcilerName = "root-reconciler-my-rs"
	nilGitContext      = `{"repo":""}`
	testGitCommit      = "example-commit"
)

func gitSpec(repo string, auth configsync.AuthType) core.MetaMutator {
	return func(o client.Object) {
		if rs, ok := o.(*v1beta1.RootSync); ok {
			rs.Spec.Git = &v1beta1.Git{
				Repo: repo,
				Auth: auth,
			}
		}
	}
}

func TestRoot_Parse(t *testing.T) {
	testCases := []struct {
		name                string
		format              filesystem.SourceFormat
		namespaceStrategy   configsync.NamespaceStrategy
		existingObjects     []client.Object
		parseOutputs        []fsfake.ParserOutputs
		expectedObjsToApply []ast.FileObject
	}{
		{
			name:   "no objects",
			format: filesystem.SourceFormatUnstructured,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // One Parse call, no results or errors
			},
		},
		{
			name:              "implicit namespace if unstructured and not present",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "no implicit namespace if namespaceStrategy is explicit",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyExplicit,
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "implicit namespace if unstructured, present and self-managed",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			existingObjects: []client.Object{fake.NamespaceObject("foo",
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
				core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
				core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
				core.Annotation(metadata.GitContextKey, nilGitContext),
				core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
				core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
				core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
				difftest.ManagedBy(declared.RootScope, rootSyncName))},
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "no implicit namespace if unstructured, present, but managed by others",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			existingObjects: []client.Object{fake.NamespaceObject("foo",
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
				core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
				core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
				core.Annotation(metadata.GitContextKey, nilGitContext),
				core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
				core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
				core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
				difftest.ManagedBy(declared.RootScope, "other-root-sync"))},
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "no implicit namespace if unstructured, present, but unmanaged",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			existingObjects:   []client.Object{fake.NamespaceObject("foo")},
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "no implicit namespace if unstructured and namespace is config-management-system",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.RootSyncV1Beta1("test", fake.WithRootSyncSourceType(configsync.GitSource), gitSpec("https://github.com/test/test.git", configsync.AuthNone)),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.RootSyncV1Beta1("test", gitSpec("https://github.com/test/test.git", configsync.AuthNone),
					fake.WithRootSyncSourceType(configsync.GitSource),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1beta1"),
					core.Annotation(metadata.SourcePathAnnotationKey, fmt.Sprintf("namespaces/%s/test.yaml", configsync.ControllerNamespace)),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "configsync.gke.io_rootsync_config-management-system_test"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "multiple objects share a single implicit namespace",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("bar")),
						fake.ConfigMap(core.Namespace("bar")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_bar_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.ConfigMap(core.Namespace("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/configmap.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_configmap_bar_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_bar"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:              "multiple implicit namespaces",
			format:            filesystem.SourceFormatUnstructured,
			namespaceStrategy: configsync.NamespaceStrategyImplicit,
			existingObjects: []client.Object{
				fake.NamespaceObject("foo"), // foo exists but not managed, should NOT be added as an implicit namespace
				// bar not exists, should be added as an implicit namespace
				fake.NamespaceObject("baz", // baz exists and self-managed, should be added as an implicit namespace
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_baz"),
					difftest.ManagedBy(declared.RootScope, rootSyncName)),
			},
			parseOutputs: []fsfake.ParserOutputs{
				{
					FileObjects: []ast.FileObject{
						fake.Role(core.Namespace("foo")),
						fake.Role(core.Namespace("bar")),
						fake.ConfigMap(core.Namespace("bar")),
						fake.Role(core.Namespace("baz")),
						fake.ConfigMap(core.Namespace("baz")),
					},
				},
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.Role(core.Namespace("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_bar_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.ConfigMap(core.Namespace("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/configmap.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_configmap_bar_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.Role(core.Namespace("baz"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_baz_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.ConfigMap(core.Namespace("baz"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/configmap.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_configmap_baz_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("bar"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_bar"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("baz"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, testGitCommit),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_baz"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
	}

	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// We're not testing the Parser here, just how the `root` calls the
			// Parser. So the outputs are faked.
			// Then we validate the inputs at the end of the test.
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: tc.parseOutputs,
			}
			// We're not testing the applier here, so the output doesn't matter.
			// We just need to make sure it only gets called once.
			// Then we validate the inputs at the end of the test.
			fakeApplier := &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call
				},
			}
			tc.existingObjects = append(tc.existingObjects, fake.RootSyncObjectV1Beta1(rootSyncName))
			parser := &root{
				Options: &Options{
					Parser:             fakeConfigParser,
					SyncName:           rootSyncName,
					ReconcilerName:     rootReconcilerName,
					Client:             syncertest.NewClient(t, core.Scheme, tc.existingObjects...),
					DiscoveryInterface: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
					Converter:          converter,
					Updater: Updater{
						Scope:          declared.RootScope,
						Resources:      &declared.Resources{},
						Remediator:     &remediatorfake.Remediator{},
						Applier:        fakeApplier,
						SyncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
					},
				},
				RootOptions: &RootOptions{
					SourceFormat:      tc.format,
					NamespaceStrategy: tc.namespaceStrategy,
				},
			}
			files := []cmpath.Absolute{
				"example.yaml",
			}
			state := &reconcilerState{
				cache: cacheForCommit{
					source: sourceState{
						commit:  testGitCommit,
						syncDir: "/",
						files:   files,
					},
				},
			}
			if err := parseAndUpdate(context.Background(), parser, triggerReimport, state); err != nil {
				t.Fatal(err)
			}

			// Validate that Parser.Parse got the expected inputs from the cached source state.
			expectedParseInputs := []fsfake.ParserInputs{
				{
					FilePaths: reader.FilePaths{
						RootDir:   "/",
						PolicyDir: "",
						Files:     files,
					},
				},
			}
			testutil.AssertEqual(t, expectedParseInputs, fakeConfigParser.Inputs, "unexpected Parser.Parse call inputs")

			// After parsing, the objects set is processed and modified.
			// Validate that the result is stored in state.cache.objsToApply.
			testutil.AssertEqual(t, tc.expectedObjsToApply, state.cache.objsToApply, "unexpected state.cache.objsToApply contents")

			// Build expected apply inputs from the expectedFileObjects
			expectedApplyInputs := []applierfake.ApplierInputs{
				// One Apply call
				{Objects: filesystem.AsCoreObjects(tc.expectedObjsToApply)},
			}
			testutil.AssertEqual(t, expectedApplyInputs, fakeApplier.ApplyInputs, "unexpected Applier.Apply call inputs")
		})
	}
}

func TestRoot_DeclaredFields(t *testing.T) {
	testCases := []struct {
		name                string
		webhookEnabled      bool
		existingObjects     []client.Object
		parsed              []ast.FileObject
		expectedObjsToApply []ast.FileObject
	}{
		{
			name:           "has declared-fields annotation, admission webhook is disabled",
			webhookEnabled: false,
			existingObjects: []client.Object{fake.NamespaceObject("foo",
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
				core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
				core.Annotation(metadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
				core.Annotation(metadata.GitContextKey, nilGitContext),
				core.Annotation(metadata.SyncTokenAnnotationKey, ""),
				core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
				core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
				difftest.ManagedBy(declared.RootScope, "other-root-sync"))},
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:           "has declared-fields annotation, admission webhook enabled",
			webhookEnabled: true,
			existingObjects: []client.Object{fake.NamespaceObject("foo",
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
				core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
				core.Annotation(metadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{},"f:labels":{}},"f:rules":{}}`),
				core.Annotation(metadata.GitContextKey, nilGitContext),
				core.Annotation(metadata.SyncTokenAnnotationKey, ""),
				core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
				core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
				difftest.ManagedBy(declared.RootScope, "other-root-sync")),
			},
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/source-path":{}},"f:labels":{"f:configsync.gke.io/declared-version":{}}},"f:rules":{}}`),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:           "has no declared-fields annotation, admission webhook is enabled",
			webhookEnabled: true,
			existingObjects: []client.Object{
				fake.NamespaceObject("foo",
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, "other-root-sync")),
				fake.AdmissionWebhookObject(webhookconfiguration.Name),
			},
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.DeclaredFieldsKey, `{"f:metadata":{"f:annotations":{"f:configmanagement.gke.io/source-path":{}},"f:labels":{"f:configsync.gke.io/declared-version":{}}},"f:rules":{}}`),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			name:           "has no declared-fields annotation, admission webhook disabled",
			webhookEnabled: false,
			existingObjects: []client.Object{
				fake.NamespaceObject("foo",
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, "other-root-sync")),
			},
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
	}

	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: []fsfake.ParserOutputs{
					// One Parse call, no errors
					{FileObjects: tc.parsed},
				},
			}
			fakeApplier := &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call
				},
			}
			tc.existingObjects = append(tc.existingObjects, fake.RootSyncObjectV1Beta1(rootSyncName))
			parser := &root{
				Options: &Options{
					Parser:             fakeConfigParser,
					SyncName:           rootSyncName,
					ReconcilerName:     rootReconcilerName,
					Client:             syncertest.NewClient(t, core.Scheme, tc.existingObjects...),
					DiscoveryInterface: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
					Converter:          converter,
					WebhookEnabled:     tc.webhookEnabled,
					Updater: Updater{
						Scope:          declared.RootScope,
						Resources:      &declared.Resources{},
						Remediator:     &remediatorfake.Remediator{},
						Applier:        fakeApplier,
						SyncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
					},
				},
				RootOptions: &RootOptions{
					SourceFormat:      filesystem.SourceFormatUnstructured,
					NamespaceStrategy: configsync.NamespaceStrategyExplicit,
				},
			}
			state := &reconcilerState{}
			if err := parseAndUpdate(context.Background(), parser, triggerReimport, state); err != nil {
				t.Fatal(err)
			}

			// After parsing, the objects set is processed and modified.
			// Validate that the result is stored in state.cache.objsToApply.
			testutil.AssertEqual(t, tc.expectedObjsToApply, state.cache.objsToApply, "unexpected state.cache.objsToApply contents")
		})
	}
}

func fakeCRD(opts ...core.MetaMutator) ast.FileObject {
	crd := fake.CustomResourceDefinitionV1Object(opts...)
	crd.Spec.Group = "acme.com"
	crd.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Plural:   "anvils",
		Singular: "anvil",
		Kind:     "Anvil",
	}
	crd.Spec.Scope = apiextensionsv1.NamespaceScoped
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
		{
			Name:    "v1",
			Served:  true,
			Storage: false,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type:     "object",
							Required: []string{"lbs"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"lbs": {
									Type:    "integer",
									Minimum: ptr.To(1.0),
									Maximum: ptr.To(9000.0),
								},
							},
						},
					},
				},
			},
		},
	}
	return fake.FileObject(crd, "cluster/crd.yaml")
}

func fakeFileObjects() []ast.FileObject {
	var fileObjects []ast.FileObject
	for i := 0; i < 500; i++ {
		fileObjects = append(fileObjects, fake.RoleAtPath(fmt.Sprintf("namespaces/foo/role%v.yaml", i), core.Namespace("foo")))
	}
	return fileObjects
}

func fakeGVKs() []schema.GroupVersionKind {
	var gvks []schema.GroupVersionKind
	for i := 0; i < 500; i++ {
		gvks = append(gvks, schema.GroupVersionKind{Group: fmt.Sprintf("acme%v.com", i), Version: "v1", Kind: "APIService"})
	}
	return gvks
}

func fakeParseError(err error, gvks ...schema.GroupVersionKind) status.MultiError {
	groups := make(map[schema.GroupVersion]error)
	for _, gvk := range gvks {
		gv := gvk.GroupVersion()
		groups[gv] = err
	}
	return status.APIServerError(&discovery.ErrGroupDiscoveryFailed{Groups: groups}, "API discovery failed")
}

func TestRoot_Parse_Discovery(t *testing.T) {
	testCases := []struct {
		name                string
		parsed              []ast.FileObject
		discoveryClient     discoveryutil.ServerResourcer
		expectedError       status.MultiError
		expectedObjsToApply []ast.FileObject
	}{
		{
			// unknown scoped object should not be skipped when sending to applier when discovery call fails
			name:            "unknown scoped object with discovery failure of deadline exceeded error",
			discoveryClient: syncertest.NewDiscoveryClientWithError(context.DeadlineExceeded, kinds.Namespace(), kinds.Role()),
			expectedError:   fakeParseError(context.DeadlineExceeded, kinds.Namespace(), kinds.Role()),
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
				// add a faked obect in parser.parsed without CRD so it's scope will be unknown when validating
				fake.Unstructured(kinds.Anvil(), core.Name("deploy")),
			},
			expectedObjsToApply: nil,
		},
		{
			// unknown scoped object should not be skipped when sending to applier when discovery call fails
			name:            "unknown scoped object with discovery failure of http request canceled failure",
			discoveryClient: syncertest.NewDiscoveryClientWithError(errors.New("net/http: request canceled (Client.Timeout exceeded while awaiting headers)"), kinds.Namespace(), kinds.Role()),
			expectedError:   fakeParseError(errors.New("net/http: request canceled (Client.Timeout exceeded while awaiting headers)"), kinds.Namespace(), kinds.Role()),
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
				// add a faked obect in parser.parsed without CRD so it's scope will be unknown when validating
				fake.Unstructured(kinds.Anvil(), core.Name("deploy")),
			},
			expectedObjsToApply: nil,
		},
		{
			// unknown scoped object should not be skipped when sending to applier when discovery call fails
			name:                "unknown scoped object with discovery failure of 500 deadline exceeded failure",
			discoveryClient:     syncertest.NewDiscoveryClientWithError(context.DeadlineExceeded, fakeGVKs()...),
			expectedError:       fakeParseError(context.DeadlineExceeded, fakeGVKs()...),
			parsed:              append(fakeFileObjects(), fake.Unstructured(kinds.Anvil(), core.Name("deploy"))),
			expectedObjsToApply: nil,
		},
		{
			// unknown scoped object get skipped when sending to applier when discovery call is good
			name:            "unknown scoped object without discovery failure",
			discoveryClient: syncertest.NewDiscoveryClientWithError(nil, kinds.Namespace(), kinds.Role()),
			expectedError: status.UnknownObjectKindError(
				fake.Unstructured(kinds.Anvil(), core.Name("deploy"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/obj.yaml"),
				)),
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
				// add a faked obect in parser.parsed without CRD so it's scope will be unknown when validating
				fake.Unstructured(kinds.Anvil(), core.Name("deploy")),
			},
			expectedObjsToApply: []ast.FileObject{
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
		{
			// happy path condition
			name:            "known scoped object without discovery failure",
			discoveryClient: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
			expectedError:   nil,
			parsed: []ast.FileObject{
				fake.Role(core.Namespace("foo")),
				fakeCRD(core.Name("anvils.acme.com")),
				fake.Unstructured(kinds.Anvil(), core.Name("deploy"), core.Namespace("foo")),
			},
			expectedObjsToApply: []ast.FileObject{
				fakeCRD(core.Name("anvils.acme.com"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "cluster/crd.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "apiextensions.k8s.io_customresourcedefinition_anvils.acme.com"),
					difftest.ManagedBy(declared.RootScope, rootSyncName)),
				fake.Role(core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/foo/role.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
				fake.Unstructured(kinds.Anvil(),
					core.Name("deploy"),
					core.Namespace("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Label(metadata.DeclaredVersionLabel, "v1"),
					core.Annotation(metadata.ResourceManagerKey, ":root_my-rs"),
					core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/obj.yaml"),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "acme.com_anvil_foo_deploy"),
				),
				fake.UnstructuredAtPath(kinds.Namespace(),
					"",
					core.Name("foo"),
					core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
					core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
					core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
					core.Annotation(metadata.GitContextKey, nilGitContext),
					core.Annotation(metadata.SyncTokenAnnotationKey, ""),
					core.Annotation(metadata.OwningInventoryKey, applier.InventoryID(rootSyncName, configmanagement.ControllerNamespace)),
					core.Annotation(metadata.ResourceIDKey, "_namespace_foo"),
					difftest.ManagedBy(declared.RootScope, rootSyncName),
				),
			},
		},
	}

	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: []fsfake.ParserOutputs{
					// One Parse call, no errors
					{FileObjects: tc.parsed},
				},
			}
			fakeApplier := &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call
				},
			}
			parser := &root{
				Options: &Options{
					Parser:             fakeConfigParser,
					SyncName:           rootSyncName,
					ReconcilerName:     rootReconcilerName,
					Client:             syncertest.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
					DiscoveryInterface: tc.discoveryClient,
					Converter:          converter,
					Updater: Updater{
						Scope:          declared.RootScope,
						Resources:      &declared.Resources{},
						Remediator:     &remediatorfake.Remediator{},
						Applier:        fakeApplier,
						SyncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
					},
				},
				RootOptions: &RootOptions{
					SourceFormat:      filesystem.SourceFormatUnstructured,
					NamespaceStrategy: configsync.NamespaceStrategyImplicit,
				},
			}
			state := &reconcilerState{}
			err := parseAndUpdate(context.Background(), parser, triggerReimport, state)
			testerrors.AssertEqual(t, tc.expectedError, err, "expected error to match")

			// After parsing, the objects set is processed and modified.
			// Validate that the result is stored in state.cache.objsToApply.
			testutil.AssertEqual(t, tc.expectedObjsToApply, state.cache.objsToApply, "unexpected state.cache.objsToApply contents")
		})
	}
}

func TestRoot_SourceReconcilerErrorsMetricValidation(t *testing.T) {
	testCases := []struct {
		name            string
		parseErrors     status.MultiError
		expectedError   status.MultiError
		expectedMetrics []*view.Row
	}{
		{
			name: "single reconciler error in source component",
			parseErrors: status.Wrap(
				status.SourceError.Sprintf("source error").Build(),
			),
			expectedError: status.Wrap(
				status.SourceError.Sprintf("source error").Build(),
			),
			expectedMetrics: []*view.Row{
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
			},
		},
		{
			name: "multiple reconciler errors in source component",
			parseErrors: status.Wrap(
				status.SourceError.Sprintf("source error").Build(),
				status.InternalError("internal error"),
			),
			expectedError: status.Wrap(
				status.SourceError.Sprintf("source error").Build(),
				status.InternalError("internal error"),
			),
			expectedMetrics: []*view.Row{
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, tc.parseErrors)
			m := testmetrics.RegisterMetrics(metrics.ReconcilerErrorsView)
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: []fsfake.ParserOutputs{
					// One Parse call, with errors
					{Errors: tc.parseErrors},
				},
			}
			fakeApplier := &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call
				},
			}
			parser := &root{
				Options: &Options{
					Parser:             fakeConfigParser,
					SyncName:           rootSyncName,
					ReconcilerName:     rootReconcilerName,
					Client:             syncertest.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
					DiscoveryInterface: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
					Updater: Updater{
						Scope:          declared.RootScope,
						Resources:      &declared.Resources{},
						Remediator:     &remediatorfake.Remediator{},
						Applier:        fakeApplier,
						SyncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
					},
				},
				RootOptions: &RootOptions{
					SourceFormat: filesystem.SourceFormatUnstructured,
				},
			}
			state := &reconcilerState{}
			err := parseAndUpdate(context.Background(), parser, triggerReimport, state)
			testerrors.AssertEqual(t, tc.expectedError, err, "expected error to match")

			if diff := m.ValidateMetrics(metrics.ReconcilerErrorsView, tc.expectedMetrics); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestRoot_SourceAndSyncReconcilerErrorsMetricValidation(t *testing.T) {
	testCases := []struct {
		name            string
		applyErrors     []status.Error
		expectedError   status.MultiError
		expectedMetrics []*view.Row
	}{
		{
			name: "single reconciler error in sync component",
			applyErrors: []status.Error{
				applier.Error(errors.New("sync error")),
			},
			expectedError: applier.Error(errors.New("sync error")),
			expectedMetrics: []*view.Row{
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
			},
		},
		{
			name: "multiple reconciler errors in sync component",
			applyErrors: []status.Error{
				applier.Error(errors.New("sync error")),
				status.InternalError("internal error"),
			},
			expectedError: status.Wrap(
				applier.Error(errors.New("sync error")),
				status.InternalError("internal error"),
			),
			expectedMetrics: []*view.Row{
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "source"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
				{Data: &view.LastValueData{Value: 0}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "1xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "2xxx"}}},
				{Data: &view.LastValueData{Value: 1}, Tags: []tag.Tag{{Key: metrics.KeyComponent, Value: "sync"}, {Key: metrics.KeyErrorClass, Value: "9xxx"}}},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := testmetrics.RegisterMetrics(metrics.ReconcilerErrorsView)
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: []fsfake.ParserOutputs{
					{}, // One Parse call, no errors
				},
			}
			fakeApplier := &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{Errors: tc.applyErrors}, // One Apply call, optional errors
				},
			}
			parser := &root{
				Options: &Options{
					Parser: fakeConfigParser,
					Updater: Updater{
						Scope:          declared.RootScope,
						Resources:      &declared.Resources{},
						Remediator:     &remediatorfake.Remediator{},
						Applier:        fakeApplier,
						SyncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
					},
					SyncName:           rootSyncName,
					ReconcilerName:     rootReconcilerName,
					Client:             syncertest.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
					DiscoveryInterface: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
				},
				RootOptions: &RootOptions{
					SourceFormat: filesystem.SourceFormatUnstructured,
				},
			}
			state := &reconcilerState{}
			err := parseAndUpdate(context.Background(), parser, triggerReimport, state)
			testerrors.AssertEqual(t, tc.expectedError, err, "expected error to match")

			if diff := m.ValidateMetrics(metrics.ReconcilerErrorsView, tc.expectedMetrics); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestSummarizeErrors(t *testing.T) {
	testCases := []struct {
		name                 string
		sourceStatus         v1beta1.SourceStatus
		syncStatus           v1beta1.SyncStatus
		expectedErrorSources []v1beta1.ErrorSource
		expectedErrorSummary *v1beta1.ErrorSummary
	}{
		{
			name:                 "both sourceStatus and syncStatus are empty",
			sourceStatus:         v1beta1.SourceStatus{},
			syncStatus:           v1beta1.SyncStatus{},
			expectedErrorSources: nil,
			expectedErrorSummary: &v1beta1.ErrorSummary{},
		},
		{
			name: "sourceStatus is not empty (no trucation), syncStatus is empty",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus:           v1beta1.SyncStatus{},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                2,
				Truncated:                 false,
				ErrorCountAfterTruncation: 2,
			},
		},
		{
			name: "sourceStatus is not empty and trucates errors, syncStatus is empty",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus:           v1beta1.SyncStatus{},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                100,
				Truncated:                 true,
				ErrorCountAfterTruncation: 2,
			},
		},
		{
			name:         "sourceStatus is empty, syncStatus is not empty (no trucation)",
			sourceStatus: v1beta1.SourceStatus{},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                2,
				Truncated:                 false,
				ErrorCountAfterTruncation: 2,
			},
		},
		{
			name:         "sourceStatus is empty, syncStatus is not empty and trucates errors",
			sourceStatus: v1beta1.SourceStatus{},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                100,
				Truncated:                 true,
				ErrorCountAfterTruncation: 2,
			},
		},
		{
			name: "neither sourceStatus nor syncStatus is empty or trucates errors",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                4,
				Truncated:                 false,
				ErrorCountAfterTruncation: 4,
			},
		},
		{
			name: "neither sourceStatus nor syncStatus is empty, sourceStatus trucates errors",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                102,
				Truncated:                 true,
				ErrorCountAfterTruncation: 4,
			},
		},
		{
			name: "neither sourceStatus nor syncStatus is empty, syncStatus trucates errors",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},

				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                102,
				Truncated:                 true,
				ErrorCountAfterTruncation: 4,
			},
		},
		{
			name: "neither sourceStatus nor syncStatus is empty, both trucates errors",
			sourceStatus: v1beta1.SourceStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
					{Code: "1022", ErrorMessage: "1022-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
				},

				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                100,
					Truncated:                 true,
					ErrorCountAfterTruncation: 2,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                200,
				Truncated:                 true,
				ErrorCountAfterTruncation: 4,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErrorSources, gotErrorSummary := summarizeErrors(tc.sourceStatus, tc.syncStatus)
			if diff := cmp.Diff(tc.expectedErrorSources, gotErrorSources); diff != "" {
				t.Errorf("summarizeErrors() got %v, expected %v", gotErrorSources, tc.expectedErrorSources)
			}
			if diff := cmp.Diff(tc.expectedErrorSummary, gotErrorSummary); diff != "" {
				t.Errorf("summarizeErrors() got %v, expected %v", gotErrorSummary, tc.expectedErrorSummary)
			}
		})
	}
}
