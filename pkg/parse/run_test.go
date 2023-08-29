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
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/openapitest"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

const (
	symLink = "rev"
)

func newParser(t *testing.T, fs FileSource, renderingEnabled bool) Parser {
	parser := &root{}
	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	parser.sourceFormat = filesystem.SourceFormatUnstructured
	parser.opts = opts{
		parser:             filesystem.NewParser(&reader.File{}),
		syncName:           rootSyncName,
		reconcilerName:     rootReconcilerName,
		client:             syncerFake.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
		discoveryInterface: syncerFake.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
		converter:          converter,
		files:              files{FileSource: fs},
		updater: updater{
			scope:      declared.RootReconciler,
			resources:  &declared.Resources{},
			remediator: &noOpRemediator{},
			applier:    &fakeApplier{},
		},
		mux:              &sync.Mutex{},
		renderingEnabled: renderingEnabled,
	}
	return parser
}

func createRootDir(rootDir, commit string) error {
	if err := os.MkdirAll(rootDir, os.ModePerm); err != nil {
		return err
	}
	commitDir := filepath.Join(rootDir, commit)
	if err := os.MkdirAll(commitDir, os.ModePerm); err != nil {
		return err
	}
	symLinkPath := filepath.Join(rootDir, symLink)
	return os.Symlink(commitDir, symLinkPath)
}

func writeFile(rootDir, file, content string) error {
	errFile := filepath.Join(rootDir, file)
	return os.WriteFile(errFile, []byte(content), 0644)
}

func TestSplitObjects(t *testing.T) {
	testCases := []struct {
		name             string
		objs             []ast.FileObject
		knownScopeObjs   []ast.FileObject
		unknownScopeObjs []ast.FileObject
	}{
		{
			name: "no unknown scope objects",
			objs: []ast.FileObject{
				fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				fake.Role(core.Namespace("prod")),
			},
			knownScopeObjs: []ast.FileObject{
				fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				fake.Role(core.Namespace("prod")),
			},
		},
		{
			name: "has unknown scope objects",
			objs: []ast.FileObject{
				fake.ClusterRole(
					core.Annotation(metadata.UnknownScopeAnnotationKey, metadata.UnknownScopeAnnotationValue),
				),
				fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				fake.Role(core.Namespace("prod")),
			},
			knownScopeObjs: []ast.FileObject{
				fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				fake.Role(core.Namespace("prod")),
			},
			unknownScopeObjs: []ast.FileObject{
				fake.ClusterRole(
					core.Annotation(metadata.UnknownScopeAnnotationKey, metadata.UnknownScopeAnnotationValue),
				),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotKnownScopeObjs, gotUnknownScopeObjs := splitObjects(tc.objs)
			if diff := cmp.Diff(tc.knownScopeObjs, gotKnownScopeObjs, ast.CompareFileObject); diff != "" {
				t.Errorf("cmp.Diff(tc.knownScopeObjs, gotKnownScopeObjs) = %v", diff)
			}
			if diff := cmp.Diff(tc.unknownScopeObjs, gotUnknownScopeObjs, ast.CompareFileObject); diff != "" {
				t.Errorf("cmp.Diff(tc.unknownScopeObjs, gotUnknownScopeObjs) = %v", diff)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "parser-run-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func(path string) {
		err = os.RemoveAll(path)
		if err != nil {
			t.Fatal(err)
		}
	}(tempDir)

	testCases := []struct {
		id                         string
		name                       string
		commit                     string
		sourceRootExist            bool
		renderingEnabled           bool
		hasKustomization           bool
		hydratedRootExist          bool
		sourceError                string
		hydratedError              string
		hydrationDone              bool
		needRetry                  bool
		expectedMsg                string
		expectedErrorSourceRefs    []v1beta1.ErrorSource
		expectedErrors             string
		expectedStateSourceErrs    status.MultiError
		expectedStateRenderingErrs status.MultiError
		expectedRSSourceErrs       []v1beta1.ConfigSyncError
		expectedRSRenderingErrs    []v1beta1.ConfigSyncError
	}{
		{
			id:             "0",
			name:           "transient error when no sourceRoot",
			needRetry:      true,
			expectedErrors: fmt.Sprintf("1 error(s)\n\n\n[1] KNV2016: stat %s/0/source: no such file or directory\n\nFor more information, see https://g.co/cloud/acm-errors#knv2016\n", tempDir),
			// transient error is not exposed to the RootSync status
		},
		{
			id:                      "1",
			name:                    "source error",
			sourceRootExist:         true,
			sourceError:             "git sync permission issue",
			needRetry:               true,
			expectedErrors:          "1 error(s)\n\n\n[1] KNV2004: error in the git-sync container: git sync permission issue\n\nFor more information, see https://g.co/cloud/acm-errors#knv2004\n",
			expectedStateSourceErrs: status.SourceError.Sprint("error in the git-sync container: git sync permission issue").Build(),
			// source error is exposed to the RootSync status
			expectedRSSourceErrs:    status.ToCSE(status.SourceError.Sprint("error in the git-sync container: git sync permission issue").Build()),
			expectedMsg:             "Source",
			expectedErrorSourceRefs: []v1beta1.ErrorSource{v1beta1.SourceError},
		},
		{
			id:                "2",
			name:              "rendering in progress",
			sourceRootExist:   true,
			renderingEnabled:  true,
			hydratedRootExist: true,
			needRetry:         true,
			expectedMsg:       "Rendering is still in progress",
		},
		{
			id:                         "3",
			name:                       "hydration error",
			sourceRootExist:            true,
			renderingEnabled:           true,
			hasKustomization:           true,
			hydratedRootExist:          true,
			hydrationDone:              true,
			hydratedError:              `{"code": "1068", "error": "rendering error"}`,
			needRetry:                  true,
			expectedMsg:                "Rendering failed",
			expectedErrors:             "1 error(s)\n\n\n[1] KNV1068: rendering error\n\nFor more information, see https://g.co/cloud/acm-errors#knv1068\n",
			expectedStateRenderingErrs: status.HydrationError(status.ActionableHydrationErrorCode, fmt.Errorf("rendering error")),
			// rendering error is exposed to the RootSync status
			expectedRSRenderingErrs: status.ToCSE(status.HydrationError(status.ActionableHydrationErrorCode, fmt.Errorf("rendering error"))),
			expectedErrorSourceRefs: []v1beta1.ErrorSource{v1beta1.RenderingError},
		},
		{
			id:                "4",
			name:              "successful read",
			sourceRootExist:   true,
			renderingEnabled:  true,
			hasKustomization:  true,
			hydratedRootExist: true,
			hydrationDone:     true,
			needRetry:         false,
			expectedMsg:       "Sync Completed",
		},
		{
			id:                "5",
			name:              "successful read without hydration",
			sourceRootExist:   true,
			hydratedRootExist: false,
			hydrationDone:     false,
			needRetry:         false,
			expectedMsg:       "Sync Completed",
		},
		{
			id:                "6",
			name:              "error because hydration enabled with wet source",
			sourceRootExist:   true,
			renderingEnabled:  true,
			hasKustomization:  false,
			hydratedRootExist: false,
			hydrationDone:     true,
			needRetry:         true,
			expectedMsg:       "Rendering is still in progress",
			expectedErrors:    "KNV2016: sync source contains only wet configs and hydration-controller is running\n\nFor more information, see https://g.co/cloud/acm-errors#knv2016",
		},
		{
			id:                "7",
			name:              "error because hydration disabled with dry source",
			sourceRootExist:   true,
			renderingEnabled:  false,
			hasKustomization:  true,
			hydratedRootExist: true,
			hydrationDone:     true,
			needRetry:         true,
			expectedMsg:       "Rendering is still in progress",
			expectedErrors:    "KNV2016: sync source contains dry configs and hydration-controller is not running\n\nFor more information, see https://g.co/cloud/acm-errors#knv2016",
		},
	}

	sourceCommit := "abcd123"
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			rootDir := filepath.Join(tempDir, tc.id)
			if err = os.Mkdir(rootDir, os.ModePerm); err != nil {
				t.Fatal(err)
			}
			sourceRoot := filepath.Join(rootDir, "source")     // /repo/source
			hydratedRoot := filepath.Join(rootDir, "hydrated") // /repo/hydrated
			sourceDir := filepath.Join(sourceRoot, symLink)

			if tc.sourceRootExist {
				if err = createRootDir(sourceRoot, sourceCommit); err != nil {
					t.Fatal(err)
				}
				if tc.hasKustomization {
					if err = writeFile(sourceDir, "kustomization.yaml", ""); err != nil {
						t.Fatal(err)
					}
				}
			}
			if tc.sourceError != "" {
				if err = writeFile(sourceRoot, hydrate.ErrorFile, tc.sourceError); err != nil {
					t.Fatal(err)
				}
			}
			if tc.hydratedRootExist {
				if err = createRootDir(hydratedRoot, sourceCommit); err != nil {
					t.Fatal(err)
				}
			}
			if tc.hydrationDone {
				if err = writeFile(rootDir, hydrate.DoneFile, sourceCommit); err != nil {
					t.Fatal(err)
				}
			}
			if tc.hydratedError != "" {
				if err = writeFile(hydratedRoot, hydrate.ErrorFile, tc.hydratedError); err != nil {
					t.Fatal(err)
				}
			}

			fs := FileSource{
				SourceDir:    cmpath.Absolute(sourceDir),
				RepoRoot:     cmpath.Absolute(rootDir),
				HydratedRoot: hydratedRoot,
				HydratedLink: symLink,
				SourceType:   v1beta1.GitSource,
				SourceRepo:   "https://github.com/test/test.git",
				SourceBranch: "main",
			}
			parser := newParser(t, fs, tc.renderingEnabled)
			state := &reconcilerState{
				backoff:     defaultBackoff(),
				retryTimer:  time.NewTimer(configsync.DefaultReconcilerRetryPeriod),
				retryPeriod: configsync.DefaultReconcilerRetryPeriod,
			}
			run(context.Background(), parser, triggerReimport, state)

			testutil.AssertEqual(t, tc.needRetry, state.cache.needToRetry, "[%s] unexpected state.cache.needToRetry return", tc.name)
			actualErrs := ""
			if state.cache.errs != nil {
				actualErrs = state.cache.errs.Error()
			}
			testutil.AssertEqual(t, tc.expectedErrors, actualErrs, "[%s] unexpected state.cache.errs return", tc.name)
			testutil.AssertEqual(t, tc.expectedStateSourceErrs, state.sourceStatus.errs, "[%s] unexpected state.sourceStatus.errs return", tc.name)
			testutil.AssertEqual(t, tc.expectedStateRenderingErrs, state.renderingStatus.errs, "[%s] unexpected state.renderingStatus.errs return", tc.name)

			rs := &v1beta1.RootSync{}
			if err = parser.options().client.Get(context.Background(), rootsync.ObjectKey(parser.options().syncName), rs); err != nil {
				t.Fatal(err)
			}
			testutil.AssertEqual(t, tc.expectedRSSourceErrs, rs.Status.Source.Errors, "[%s] unexpected source errors in RootSync return", tc.name)
			testutil.AssertEqual(t, tc.expectedRSRenderingErrs, rs.Status.Rendering.Errors, "[%s] unexpected rendering errors in RootSync return", tc.name)

			for _, c := range rs.Status.Conditions {
				if c.Type == v1beta1.RootSyncSyncing {
					testutil.AssertEqual(t, tc.expectedMsg, c.Message, "[%s] unexpected syncing message return", tc.name)
					testutil.AssertEqual(t, tc.expectedErrorSourceRefs, c.ErrorSourceRefs, "[%s] unexpected error source refs return", tc.name)
				}
			}
		})
	}
}
