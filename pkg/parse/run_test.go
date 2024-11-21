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
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/clock"
	fakeclock "k8s.io/utils/clock/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	applierfake "kpt.dev/configsync/pkg/applier/fake"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	fsfake "kpt.dev/configsync/pkg/importer/filesystem/fake"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/remediator/conflict"
	remediatorfake "kpt.dev/configsync/pkg/remediator/fake"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/openapitest"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	symLink = "rev"
)

func newRootReconciler(t *testing.T, clock clock.Clock, fakeClient client.Client, parser filesystem.ConfigParser, fs FileSource, renderingEnabled bool) *reconciler {
	converter, err := openapitest.ValueConverterForTest()
	if err != nil {
		t.Fatal(err)
	}
	state := &ReconcilerState{
		syncErrorCache: NewSyncErrorCache(conflict.NewHandler(), fight.NewHandler()),
	}
	opts := &Options{
		Clock:             clock,
		ConfigParser:      parser,
		SyncName:          rootSyncName,
		Scope:             declared.RootScope,
		ReconcilerName:    rootReconcilerName,
		Client:            fakeClient,
		DiscoveryClient:   syncerFake.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
		Converter:         converter,
		Files:             Files{FileSource: fs},
		DeclaredResources: &declared.Resources{},
	}
	rootOpts := &RootOptions{
		Options:      opts,
		SourceFormat: configsync.SourceFormatUnstructured,
	}
	recOpts := &ReconcilerOptions{
		Options: opts,
		Updater: &Updater{
			Scope:      opts.Scope,
			Resources:  opts.DeclaredResources,
			Remediator: &remediatorfake.Remediator{},
			Applier: &applierfake.Applier{
				ApplyOutputs: []applierfake.ApplierOutputs{
					{}, // One Apply call, no errors
				},
			},
			SyncErrorCache: state.syncErrorCache,
		},
		FullSyncPeriod:     configsync.DefaultReconcilerFullSyncPeriod,
		StatusUpdatePeriod: configsync.DefaultReconcilerSyncStatusUpdatePeriod,
		RenderingEnabled:   renderingEnabled,
	}
	return &reconciler{
		options: recOpts,
		syncStatusClient: &rootSyncStatusClient{
			options: opts,
		},
		parser: &rootSyncParser{
			options: rootOpts,
		},
		reconcilerState: state,
	}
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
				k8sobjects.Namespace("namespaces/prod", core.Label("environment", "prod")),
				k8sobjects.Role(core.Namespace("prod")),
			},
			knownScopeObjs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/prod", core.Label("environment", "prod")),
				k8sobjects.Role(core.Namespace("prod")),
			},
		},
		{
			name: "has unknown scope objects",
			objs: []ast.FileObject{
				k8sobjects.ClusterRole(
					core.Annotation(metadata.UnknownScopeAnnotationKey, metadata.UnknownScopeAnnotationValue),
				),
				k8sobjects.Namespace("namespaces/prod", core.Label("environment", "prod")),
				k8sobjects.Role(core.Namespace("prod")),
			},
			knownScopeObjs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/prod", core.Label("environment", "prod")),
				k8sobjects.Role(core.Namespace("prod")),
			},
			unknownScopeObjs: []ast.FileObject{
				k8sobjects.ClusterRole(
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

func TestReconciler_Reconcile(t *testing.T) {
	fakeMetaTime := metav1.Now().Rfc3339Copy() // truncate to second precision
	fakeTime := fakeMetaTime.Time
	fakeClock := fakeclock.NewFakeClock(fakeTime)

	tempDir, err := os.MkdirTemp(os.TempDir(), "parser-run-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Error(err)
		}
	})

	sourceCommit := "abcd123"

	fileSource := FileSource{
		SourceType:   configsync.GitSource,
		SourceRepo:   "https://github.com/test/test.git",
		SourceBranch: "main",
	}

	rootSyncOutput := &v1beta1.RootSync{
		// FakeClient populates TypeMeta when converting from unstructured to typed.
		TypeMeta: metav1.TypeMeta{
			Kind:       configsync.RootSyncKind,
			APIVersion: v1beta1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      rootSyncName,
			Namespace: configmanagement.ControllerNamespace,
			// FakeClient populates a few meta values
			UID: "1",
			// ResourceVersion: "2", // Determined by test behavior
			Generation: 1, // Spec is never updated after creation
		},
	}

	testCases := []struct {
		name                  string
		trigger               string
		reconcilerStateFunc   func(state *ReconcilerState, sourcePath string)
		commit                string
		renderingEnabled      bool
		hasKustomization      bool
		hydratedRootExist     bool
		retryCap              time.Duration
		srcRootCreateLatency  time.Duration
		gitError              string
		hydratedError         string
		hydrationDone         bool
		imageVerified         bool
		expectedSourceChanged bool
		needRetry             bool
		parseOutputs          []fsfake.ParserOutputs
		expectedRootSyncFunc  func(sourcePath string) *v1beta1.RootSync
	}{
		{
			name:                  "reconcile success with existing source commit directory",
			trigger:               triggerSync,
			expectedSourceChanged: true,
			needRetry:             false,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // parse should be called exactly once
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.LastSyncedCommit = sourceCommit
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
		{
			name:                  "reconcile success with delayed source commit directory creation",
			trigger:               triggerSync,
			retryCap:              100 * time.Millisecond,
			srcRootCreateLatency:  5 * time.Millisecond,
			expectedSourceChanged: true,
			needRetry:             false,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // parse should be called exactly once
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.LastSyncedCommit = sourceCommit
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
		{
			name:                  "read retryable error because missing source commit directory",
			trigger:               triggerSync,
			retryCap:              5 * time.Millisecond,
			srcRootCreateLatency:  10 * time.Millisecond,
			expectedSourceChanged: false,
			needRetry:             true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(sourcePath string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch error)
				rs.ObjectMeta.ResourceVersion = "2"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Errors: status.ToCSE(
						status.SourceError.Wrap(
							util.NewRetriableError(
								fmt.Errorf("failed to check the status of the source root directory %q: %w", sourcePath,
									&fs.PathError{Op: "stat", Path: sourcePath, Err: syscall.Errno(2)}))).
							Build(),
					),
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Source",
						Message:            "Source",
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.SourceError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					},
				}
				return rs
			},
		},
		{
			name:                  "fetch retriable error because git-sync error",
			trigger:               triggerSync,
			gitError:              "git sync permission issue",
			expectedSourceChanged: false,
			needRetry:             true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch error)
				rs.ObjectMeta.ResourceVersion = "2"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					LastUpdate: fakeMetaTime,
					Errors: status.ToCSE(
						status.SourceError.Wrap(
							util.NewRetriableError(
								fmt.Errorf("error in the git-sync container: %w",
									fmt.Errorf("git sync permission issue")))).
							Build(),
					),
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Source",
						Message:            "Source",
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.SourceError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					},
				}
				return rs
			},
		},
		{
			name:                  "render in progress",
			trigger:               triggerSync,
			renderingEnabled:      true,
			hydratedRootExist:     true,
			expectedSourceChanged: false,
			needRetry:             true,
			imageVerified:         true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render in-progress)
				rs.ObjectMeta.ResourceVersion = "3"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingInProgress,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionTrue,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Rendering",
						Message:            RenderingInProgress,
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
		{
			name:                  "render error because hydration failed",
			trigger:               triggerSync,
			renderingEnabled:      true,
			hasKustomization:      true,
			hydratedRootExist:     true,
			hydrationDone:         true,
			imageVerified:         true,
			hydratedError:         `{"code": "1068", "error": "rendering error"}`,
			expectedSourceChanged: false,
			needRetry:             true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render error)
				rs.ObjectMeta.ResourceVersion = "3"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:  sourceCommit,
					Message: RenderingFailed,
					Errors: status.ToCSE(
						status.HydrationError(status.ActionableHydrationErrorCode, fmt.Errorf("rendering error")),
					),
					// TODO: Fix bug with rendering status not setting ErrorCountAfterTruncation = 1 (b/379720690)
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Rendering",
						Message:            RenderingFailed,
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.RenderingError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					},
				}
				return rs
			},
		},
		{
			name:                  "parse blocking error because missing name without rendering",
			trigger:               triggerSync,
			expectedSourceChanged: true,
			needRetry:             true,
			parseOutputs: []fsfake.ParserOutputs{
				{
					Errors: status.ResourceErrorBuilder.Wrap(
						fmt.Errorf("missing field %q", "metadata.name")).
						Build(),
				},
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (parse error)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:     sourceCommit,
					LastUpdate: fakeMetaTime,
					Errors: status.ToCSE(
						status.ResourceErrorBuilder.Wrap(
							fmt.Errorf("missing field %q", "metadata.name")).
							Build(),
					),
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					ErrorSummary: &v1beta1.ErrorSummary{},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Source",
						Message:            "Source",
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.SourceError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					},
				}
				return rs
			},
		},
		{
			name:                  "parse blocking error because missing name with rendering",
			trigger:               triggerSync,
			renderingEnabled:      true,
			hasKustomization:      true,
			hydratedRootExist:     true,
			hydrationDone:         true,
			expectedSourceChanged: true,
			needRetry:             true,
			imageVerified:         true,
			parseOutputs: []fsfake.ParserOutputs{
				{
					Errors: status.ResourceErrorBuilder.Wrap(
						fmt.Errorf("missing field %q", "metadata.name")).
						Build(),
				},
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render success) + Update (parse error)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:     sourceCommit,
					LastUpdate: fakeMetaTime,
					Errors: status.ToCSE(
						status.ResourceErrorBuilder.Wrap(
							fmt.Errorf("missing field %q", "metadata.name")).
							Build(),
					),
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSucceeded,
					ErrorSummary: &v1beta1.ErrorSummary{},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Source",
						Message:            "Source",
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.SourceError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					},
				}
				return rs
			},
		},
		{
			name:                  "parse non-blocking error because unknown kind",
			trigger:               triggerSync,
			expectedSourceChanged: true,
			needRetry:             true,
			parseOutputs: []fsfake.ParserOutputs{
				{
					Errors: status.UnknownObjectKindError(
						k8sobjects.Unstructured(kinds.Anvil(), core.Name("deploy"),
							core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/obj.yaml"),
						)),
				},
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (parse error) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "5"
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:     sourceCommit,
					LastUpdate: fakeMetaTime,
					Errors: status.ToCSE(
						status.UnknownObjectKindError(
							k8sobjects.Unstructured(kinds.Anvil(), core.Name("deploy"),
								core.Annotation(metadata.SourcePathAnnotationKey, "namespaces/obj.yaml"),
							)),
					),
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					ErrorSummary: &v1beta1.ErrorSummary{},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.SourceError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 1},
					},
				}
				return rs
			},
		},
		{
			name:                  "reconcile success with rendering",
			trigger:               triggerSync,
			renderingEnabled:      true,
			hasKustomization:      true,
			hydratedRootExist:     true,
			hydrationDone:         true,
			expectedSourceChanged: true,
			needRetry:             false,
			imageVerified:         true,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // parse should be called exactly once
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render success) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.LastSyncedCommit = sourceCommit
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSucceeded,
					ErrorSummary: &v1beta1.ErrorSummary{},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
		{
			name:                  "reconcile success without rendering",
			trigger:               triggerSync,
			hydratedRootExist:     false,
			hydrationDone:         false,
			expectedSourceChanged: true,
			needRetry:             false,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // parse should be called exactly once
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.LastSyncedCommit = sourceCommit
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					ErrorSummary: &v1beta1.ErrorSummary{},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
		{
			name:                  "render transient error because hydration enabled with wet source",
			trigger:               triggerSync,
			renderingEnabled:      true,
			hasKustomization:      false,
			hydratedRootExist:     false,
			hydrationDone:         true,
			expectedSourceChanged: false,
			needRetry:             true,
			imageVerified:         true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Patch (requires-rendering annotation) + Update (render error)
				rs.ObjectMeta.ResourceVersion = "4"
				// Tell reconciler-manager to disable the hydration-controller container
				core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, "false")
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:  sourceCommit,
					Message: RenderingNotRequired,
					Errors: status.ToCSE(
						status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains only wet configs and hydration-controller is running")),
					),
					// TODO: Fix bug with rendering status not setting ErrorCountAfterTruncation = 1
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Rendering",
						Message:            RenderingNotRequired,
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.RenderingError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					},
				}
				return rs
			},
		},
		{
			name:                  "render transient error because hydration disabled with dry source",
			trigger:               triggerSync,
			renderingEnabled:      false,
			hasKustomization:      true,
			hydratedRootExist:     true,
			hydrationDone:         true,
			expectedSourceChanged: false,
			needRetry:             true,
			parseOutputs:          nil, // parse should not be called
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Patch (requires-rendering annotation) + Update (render error)
				rs.ObjectMeta.ResourceVersion = "4"
				// Tell reconciler-manager to enable the hydration-controller container
				core.SetAnnotation(rs, metadata.RequiresRenderingAnnotationKey, "true")
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:  sourceCommit,
					Message: RenderingRequired,
					Errors: status.ToCSE(
						status.HydrationError(status.TransientErrorCode, fmt.Errorf("sync source contains dry configs and hydration-controller is not running")),
					),
					// TODO: Fix bug with rendering status not setting ErrorCountAfterTruncation = 1
					ErrorSummary: &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					LastUpdate:   fakeMetaTime,
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Rendering",
						Message:            RenderingRequired,
						Commit:             sourceCommit,
						ErrorSourceRefs:    []v1beta1.ErrorSource{v1beta1.RenderingError},
						ErrorSummary:       &v1beta1.ErrorSummary{TotalCount: 1, ErrorCountAfterTruncation: 0},
					},
				}
				return rs
			},
		},
		{
			name:    "reconcile success with full-sync after apply error",
			trigger: triggerSync,
			reconcilerStateFunc: func(state *ReconcilerState, sourcePath string) {
				// Set lastFullSyncTime old enough to trigger full-sync
				state.lastFullSyncTime = metav1.Time{Time: fakeClock.Now().Add(-configsync.DefaultReconcilerFullSyncPeriod - time.Second)}
				// Set checkpoint time of last error to slightly after the lastFullSyncTime
				lastCheckpointTime := metav1.Time{Time: state.lastFullSyncTime.Add(time.Second)}
				state.checkpoint = checkpoint{
					lastUpdateTime:     lastCheckpointTime,
					lastTransitionTime: lastCheckpointTime,
				}
				// Simulate fetch & parse success from current commit to ensure full-sync doesn't skip updating
				state.cache = cacheForCommit{
					source: &sourceState{
						spec:     SourceSpecFromFileSource(fileSource, fileSource.SourceType, sourceCommit),
						commit:   sourceCommit,
						syncPath: cmpath.Absolute(filepath.Join(sourcePath, sourceCommit)),
						files:    nil,
					},
					parse: &parseResult{
						lastUpdateTime: lastCheckpointTime,
					},
					declaredResourcesUpdated: true,
					applied:                  false,
					watchesUpdated:           false,
					needToRetry:              true,
				}
				// Simulate sync error
				state.syncErrorCache.AddApplyError(status.InternalError("apply error"))
			},
			expectedSourceChanged: false, // source spec & commit did not change, but was re-parsed
			needRetry:             false,
			parseOutputs: []fsfake.ParserOutputs{
				{}, // parse should be called exactly once
			},
			expectedRootSyncFunc: func(_ string) *v1beta1.RootSync {
				rs := rootSyncOutput.DeepCopy()
				// Create + Update (fetch success) + Update (render skipped) + Update (sync success)
				rs.ObjectMeta.ResourceVersion = "4"
				rs.Status.Status.LastSyncedCommit = sourceCommit
				rs.Status.Status.Source = v1beta1.SourceStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Rendering = v1beta1.RenderingStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					Message:      RenderingSkipped,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Status.Sync = v1beta1.SyncStatus{
					Git: &v1beta1.GitStatus{
						Repo:   fileSource.SourceRepo,
						Branch: fileSource.SourceBranch,
					},
					Commit:       sourceCommit,
					LastUpdate:   fakeMetaTime,
					ErrorSummary: &v1beta1.ErrorSummary{},
				}
				rs.Status.Conditions = []v1beta1.RootSyncCondition{
					{
						Type:               v1beta1.RootSyncSyncing,
						Status:             metav1.ConditionFalse,
						LastUpdateTime:     fakeMetaTime,
						LastTransitionTime: fakeMetaTime,
						Reason:             "Sync",
						Message:            "Sync Completed",
						Commit:             sourceCommit,
						ErrorSummary:       &v1beta1.ErrorSummary{},
					},
				}
				return rs
			},
		},
	}

	for index, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			util.SourceRetryBackoff = wait.Backoff{
				Duration: time.Millisecond,
				Factor:   2,
				Steps:    10,
				Cap:      tc.retryCap,
				Jitter:   0.1,
			}
			util.HydratedRetryBackoff = util.SourceRetryBackoff

			rootDir := filepath.Join(tempDir, fmt.Sprint(index))
			sourceRoot := filepath.Join(rootDir, "source")     // /repo/source
			hydratedRoot := filepath.Join(rootDir, "hydrated") // /repo/hydrated
			sourceDir := filepath.Join(sourceRoot, symLink)
			reconcilerSignalDir := filepath.Join(rootDir, "reconciler-signals")

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
						if tc.gitError != "" {
							if err = writeFile(sourceRoot, hydrate.ErrorFile, tc.gitError); err != nil {
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
						if err = createRootDir(reconcilerSignalDir, sourceCommit); err != nil {
							return fmt.Errorf("failed to create reconciler-signals directory: %v", err)
						}
					}
					return nil
				}()
				if err != nil {
					t.Log(err)
				}
			}()

			fs := FileSource{
				SourceDir:            cmpath.Absolute(sourceDir),
				RepoRoot:             cmpath.Absolute(rootDir),
				HydratedRoot:         hydratedRoot,
				HydratedLink:         symLink,
				SourceType:           fileSource.SourceType,
				SourceRepo:           fileSource.SourceRepo,
				SourceBranch:         fileSource.SourceBranch,
				ReconcilerSignalsDir: cmpath.Absolute(reconcilerSignalDir),
			}
			fakeClient := syncerFake.NewClient(t, core.Scheme, k8sobjects.RootSyncObjectV1Beta1(rootSyncName))
			fakeConfigParser := &fsfake.ConfigParser{
				Outputs: tc.parseOutputs,
			}
			reconciler := newRootReconciler(t, fakeClock, fakeClient, fakeConfigParser, fs, tc.renderingEnabled)
			if tc.reconcilerStateFunc != nil {
				// Mutate the ReconcilerState
				tc.reconcilerStateFunc(reconciler.reconcilerState, sourceRoot)
			}
			t.Logf("start running test at %v", time.Now())
			result := reconciler.Reconcile(context.Background(), tc.trigger)

			assert.Equal(t, tc.expectedSourceChanged, result.SourceChanged)
			assert.Equal(t, tc.needRetry, reconciler.ReconcilerState().cache.needToRetry)

			rs := &v1beta1.RootSync{}
			err = fakeClient.Get(context.Background(), rootsync.ObjectKey(rootSyncName), rs)
			require.NoError(t, err)
			testutil.AssertEqual(t, tc.expectedRootSyncFunc(sourceRoot), rs)

			if tc.imageVerified {
				readyToRenderFilePath := filepath.Join(reconcilerSignalDir, "ready-to-render")
				_, err = os.Stat(readyToRenderFilePath)
				assert.NoError(t, err, fmt.Sprintf("ready-to-render file should exist at %s", readyToRenderFilePath))
			}

			// Block and wait for the goroutine to complete.
			<-doneCh
		})
	}
}
