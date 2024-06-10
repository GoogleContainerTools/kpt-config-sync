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
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/wait"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	applierfake "kpt.dev/configsync/pkg/applier/fake"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
	remediatorfake "kpt.dev/configsync/pkg/remediator/fake"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/openapitest"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

const (
	symLink = "rev"
)

func newParser(t *testing.T, fs FileSource, renderingEnabled bool, retryPeriod time.Duration, pollingPeriod time.Duration) Parser {
	parser := &root{}
	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}

	parser.RootOptions = &RootOptions{
		SourceFormat: filesystem.SourceFormatUnstructured,
	}
	parser.Options = &Options{
		Parser:             filesystem.NewParser(&reader.File{}),
		StatusUpdatePeriod: configsync.DefaultReconcilerSyncStatusUpdatePeriod,
		SyncName:           rootSyncName,
		ReconcilerName:     rootReconcilerName,
		Client:             syncerFake.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
		DiscoveryInterface: syncerFake.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
		Converter:          converter,
		Files:              Files{FileSource: fs},
		Updater: Updater{
			Scope:      declared.RootScope,
			Resources:  &declared.Resources{},
			Remediator: &remediatorfake.Remediator{},
			Applier: &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call, no errors
				},
			},
		},
		RenderingEnabled: renderingEnabled,
		RetryPeriod:      retryPeriod,
		ResyncPeriod:     2 * time.Second,
		PollingPeriod:    pollingPeriod,
	}

	return parser
}

func createRootDir(rootDir, commit string) error {
	if err := os.MkdirAll(rootDir, os.ModePerm); err != nil {
		return err
	}
	commitDir := filepath.Join(rootDir, commit)
	if err := os.Mkdir(commitDir, os.ModePerm); err != nil {
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
	t.Cleanup(func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Error(err)
		}
	})
	sourceDir0 := filepath.Join(tempDir, "0", "source")

	testCases := []struct {
		id                         string
		name                       string
		commit                     string
		renderingEnabled           bool
		hasKustomization           bool
		hydratedRootExist          bool
		retryCap                   time.Duration
		srcRootCreateLatency       time.Duration
		sourceError                string
		hydratedError              string
		hydrationDone              bool
		needRetry                  bool
		expectedMsg                string
		expectedErrorSourceRefs    []v1beta1.ErrorSource
		expectedErrors             status.MultiError
		expectedStateSourceErrs    status.MultiError
		expectedStateRenderingErrs status.MultiError
	}{
		{
			id:                      "0",
			name:                    "source commit directory isn't created within the retry cap",
			retryCap:                5 * time.Millisecond,
			srcRootCreateLatency:    10 * time.Millisecond,
			needRetry:               true,
			expectedMsg:             "Source",
			expectedErrorSourceRefs: []v1beta1.ErrorSource{v1beta1.SourceError},
			expectedErrors: status.SourceError.Wrap(
				util.NewRetriableError(
					fmt.Errorf("failed to check the status of the source root directory %q: %w", sourceDir0,
						&fs.PathError{Op: "stat", Path: sourceDir0, Err: syscall.Errno(2)}))).
				Build(),
			expectedStateSourceErrs: status.SourceError.Wrap(
				util.NewRetriableError(
					fmt.Errorf("failed to check the status of the source root directory %q: %w", sourceDir0,
						&fs.PathError{Op: "stat", Path: sourceDir0, Err: syscall.Errno(2)}))).
				Build(),
		},
		{
			id:                   "1",
			name:                 "source commit directory created within the retry cap",
			retryCap:             100 * time.Millisecond,
			srcRootCreateLatency: 5 * time.Millisecond,
			needRetry:            false,
			expectedMsg:          "Sync Completed",
		},
		{
			id:          "2",
			name:        "source error",
			sourceError: "git sync permission issue",
			needRetry:   true,
			expectedErrors: status.SourceError.Wrap(
				fmt.Errorf("error in the git-sync container: %w",
					fmt.Errorf("git sync permission issue"))).
				Build(),
			expectedStateSourceErrs: status.SourceError.Wrap(
				util.NewRetriableError(
					fmt.Errorf("error in the git-sync container: %w",
						fmt.Errorf("git sync permission issue")))).
				Build(),
			// source error is exposed to the RootSync status
			expectedMsg:             "Source",
			expectedErrorSourceRefs: []v1beta1.ErrorSource{v1beta1.SourceError},
		},
		{
			id:                "3",
			name:              "rendering in progress",
			renderingEnabled:  true,
			hydratedRootExist: true,
			needRetry:         true,
			expectedMsg:       "Rendering is still in progress",
		},
		{
			id:                         "4",
			name:                       "hydration error",
			renderingEnabled:           true,
			hasKustomization:           true,
			hydratedRootExist:          true,
			hydrationDone:              true,
			hydratedError:              `{"code": "1068", "error": "rendering error"}`,
			needRetry:                  true,
			expectedMsg:                "Rendering failed",
			expectedErrors:             status.HydrationError(status.ActionableHydrationErrorCode, fmt.Errorf("rendering error")),
			expectedStateRenderingErrs: status.HydrationError(status.ActionableHydrationErrorCode, fmt.Errorf("rendering error")),
			// rendering error is exposed to the RootSync status
			expectedErrorSourceRefs: []v1beta1.ErrorSource{v1beta1.RenderingError},
		},
		{
			id:                "5",
			name:              "successful read",
			renderingEnabled:  true,
			hasKustomization:  true,
			hydratedRootExist: true,
			hydrationDone:     true,
			needRetry:         false,
			expectedMsg:       "Sync Completed",
		},
		{
			id:                "6",
			name:              "successful read without hydration",
			hydratedRootExist: false,
			hydrationDone:     false,
			needRetry:         false,
			expectedMsg:       "Sync Completed",
		},
		{
			id:                         "7",
			name:                       "error because hydration enabled with wet source",
			renderingEnabled:           true,
			hasKustomization:           false,
			hydratedRootExist:          false,
			hydrationDone:              true,
			needRetry:                  true,
			expectedMsg:                "Rendering not required but is currently enabled",
			expectedErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.RenderingError},
			expectedErrors:             status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains only wet configs and hydration-controller is running")),
			expectedStateRenderingErrs: status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains only wet configs and hydration-controller is running")),
		},
		{
			id:                         "8",
			name:                       "error because hydration disabled with dry source",
			renderingEnabled:           false,
			hasKustomization:           true,
			hydratedRootExist:          true,
			hydrationDone:              true,
			needRetry:                  true,
			expectedMsg:                "Rendering required but is currently disabled",
			expectedErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.RenderingError},
			expectedErrors:             status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains dry configs and hydration-controller is not running")),
			expectedStateRenderingErrs: status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains dry configs and hydration-controller is not running")),
		},
	}

	sourceCommit := "abcd123"
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			util.SourceRetryBackoff = wait.Backoff{
				Duration: time.Millisecond,
				Factor:   2,
				Steps:    10,
				Cap:      tc.retryCap,
				Jitter:   0.1,
			}
			util.HydratedRetryBackoff = util.SourceRetryBackoff

			rootDir := filepath.Join(tempDir, tc.id)
			sourceRoot := filepath.Join(rootDir, "source")     // /repo/source
			hydratedRoot := filepath.Join(rootDir, "hydrated") // /repo/hydrated
			sourceDir := filepath.Join(sourceRoot, symLink)

			// Simulating the creation of source configs and errors in the background
			doneCh := make(chan struct{})
			go func() {
				defer close(doneCh)
				err := func() error {
					// Create the source root directory conditionally with latency
					srcRootDirCreated := false
					if tc.srcRootCreateLatency > 0 {
						t.Logf("sleeping for %q before creating source root directory %q", tc.srcRootCreateLatency, rootDir)
						time.Sleep(tc.srcRootCreateLatency)
					}
					if tc.srcRootCreateLatency <= tc.retryCap {
						if err = os.Mkdir(rootDir, os.ModePerm); err != nil {
							return fmt.Errorf("failed to create source root directory %q: %v", rootDir, err)
						}
						srcRootDirCreated = true
						t.Logf("source root directory %q created at %v", rootDir, time.Now())
					}

					if srcRootDirCreated {
						if err = createRootDir(sourceRoot, sourceCommit); err != nil {
							return fmt.Errorf("failed to create source commit directory: %v", err)
						}
						if tc.hasKustomization {
							if err = writeFile(sourceDir, "kustomization.yaml", ""); err != nil {
								return fmt.Errorf("failed to write kustomization file: %v", err)
							}
						}

						if tc.sourceError != "" {
							if err = writeFile(sourceRoot, hydrate.ErrorFile, tc.sourceError); err != nil {
								return fmt.Errorf("failed to write source error file: %v", err)
							}
						}
						if tc.hydratedRootExist {
							if err = createRootDir(hydratedRoot, sourceCommit); err != nil {
								return fmt.Errorf("failed to create hydrated commit directory: %v", err)
							}
						}
						if tc.hydrationDone {
							if err = writeFile(rootDir, hydrate.DoneFile, sourceCommit); err != nil {
								return fmt.Errorf("failed to write done file: %v", err)
							}
						}
						if tc.hydratedError != "" {
							if err = writeFile(hydratedRoot, hydrate.ErrorFile, tc.hydratedError); err != nil {
								return fmt.Errorf("failed to write hydrated error file: %v", err)
							}
						}
					}
					return nil
				}()
				if err != nil {
					t.Log(err)
				}
			}()

			fs := FileSource{
				SourceDir:    cmpath.Absolute(sourceDir),
				RepoRoot:     cmpath.Absolute(rootDir),
				HydratedRoot: hydratedRoot,
				HydratedLink: symLink,
				SourceType:   v1beta1.GitSource,
				SourceRepo:   "https://github.com/test/test.git",
				SourceBranch: "main",
			}
			parser := newParser(t, fs, tc.renderingEnabled, configsync.DefaultReconcilerRetryPeriod, configsync.DefaultReconcilerPollingPeriod)
			state := &reconcilerState{
				backoff:     defaultBackoff(),
				retryTimer:  time.NewTimer(configsync.DefaultReconcilerRetryPeriod),
				retryPeriod: configsync.DefaultReconcilerRetryPeriod,
			}
			t.Logf("start running test at %v", time.Now())
			run(context.Background(), parser, triggerReimport, state)

			assert.Equal(t, tc.needRetry, state.cache.needToRetry)
			testerrors.AssertEqual(t, tc.expectedErrors, state.cache.errs, "[%s] unexpected state.cache.errs return", tc.name)
			testerrors.AssertEqual(t, tc.expectedStateSourceErrs, state.sourceStatus.errs, "[%s] unexpected state.sourceStatus.errs return", tc.name)
			testerrors.AssertEqual(t, tc.expectedStateRenderingErrs, state.renderingStatus.errs, "[%s] unexpected state.renderingStatus.errs return", tc.name)

			rs := &v1beta1.RootSync{}
			if err = parser.options().Client.Get(context.Background(), rootsync.ObjectKey(parser.options().SyncName), rs); err != nil {
				t.Fatal(err)
			}
			expectedRSSourceErrs := status.ToCSE(state.sourceStatus.errs)
			expectedRSRenderingErrs := status.ToCSE(state.renderingStatus.errs)
			testutil.AssertEqual(t, expectedRSSourceErrs, rs.Status.Source.Errors, "[%s] unexpected source errors in RootSync return", tc.name)
			testutil.AssertEqual(t, expectedRSRenderingErrs, rs.Status.Rendering.Errors, "[%s] unexpected rendering errors in RootSync return", tc.name)

			for _, c := range rs.Status.Conditions {
				if c.Type == v1beta1.RootSyncSyncing {
					testutil.AssertEqual(t, tc.expectedMsg, c.Message, "[%s] unexpected syncing message return", tc.name)
					testutil.AssertEqual(t, tc.expectedErrorSourceRefs, c.ErrorSourceRefs, "[%s] unexpected error source refs return", tc.name)
				}
			}

			// Block and wait for the goroutine to complete.
			<-doneCh
		})
	}
}

func TestBackoffRetryCount(t *testing.T) {
	parser := newParser(t, FileSource{}, false, 10*time.Microsecond, 150*time.Microsecond)
	testState := &namespacecontroller.State{}
	reimportCount := 0
	retryCount := 0
	testIsDone := func(reimportCount *int, _ *int) bool {
		return *reimportCount == 35
	}

	t.Logf("start running test at %v", time.Now())

	ctx, cancel := context.WithCancel(context.Background())
	backoff := defaultBackoff()
	backoff.Duration = 10 * time.Microsecond

	Run(ctx, parser, testState, RunOpts{
		runFunc: mockRun(&reimportCount, &retryCount, cancel, testIsDone),
		backoff: backoff,
	})

	assert.Equal(t, 12, retryCount)
}

func mockRun(reimportCount *int, retryCount *int, cancelFn func(), testIsDone func(reimportCount *int, retryCount *int) bool) RunFunc {
	return func(_ context.Context, _ Parser, trigger string, state *reconcilerState) {
		state.cache.needToRetry = true

		switch trigger {
		case triggerReimport:
			*reimportCount++
		case triggerRetry:
			*retryCount++
		}

		if testIsDone(reimportCount, retryCount) {
			cancelFn()
		}
	}
}
