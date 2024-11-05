// Copyright 2024 Google LLC
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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	syncertest "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestSummarizeErrors(t *testing.T) {
	testCases := []struct {
		name                 string
		sourceStatus         v1beta1.SourceStatus
		renderingStatus      v1beta1.RenderingStatus
		syncStatus           v1beta1.SyncStatus
		expectedErrorSources []v1beta1.ErrorSource
		expectedErrorSummary *v1beta1.ErrorSummary
	}{
		{
			name:                 "both sourceStatus and syncStatus are empty",
			sourceStatus:         v1beta1.SourceStatus{},
			renderingStatus:      v1beta1.RenderingStatus{},
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
			renderingStatus:      v1beta1.RenderingStatus{},
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
			renderingStatus:      v1beta1.RenderingStatus{},
			syncStatus:           v1beta1.SyncStatus{},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                100,
				Truncated:                 true,
				ErrorCountAfterTruncation: 2,
			},
		},
		{
			name:            "sourceStatus is empty, syncStatus is not empty (no trucation)",
			sourceStatus:    v1beta1.SourceStatus{},
			renderingStatus: v1beta1.RenderingStatus{},
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
			name:            "sourceStatus is empty, syncStatus is not empty and trucates errors",
			sourceStatus:    v1beta1.SourceStatus{},
			renderingStatus: v1beta1.RenderingStatus{},
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
			renderingStatus: v1beta1.RenderingStatus{},
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
			renderingStatus: v1beta1.RenderingStatus{},
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
			renderingStatus: v1beta1.RenderingStatus{},
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
			renderingStatus: v1beta1.RenderingStatus{},
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
		{
			name: "source, rendering, and sync errors",
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
			renderingStatus: v1beta1.RenderingStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1068", ErrorMessage: "1068-error-message"},
					{Code: "2015", ErrorMessage: "2015-error-message"},
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
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.RenderingError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                6,
				Truncated:                 false,
				ErrorCountAfterTruncation: 6,
			},
		},
		{
			name: "source, rendering, and sync errors, all truncated",
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
			renderingStatus: v1beta1.RenderingStatus{
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1068", ErrorMessage: "1068-error-message"},
					{Code: "2015", ErrorMessage: "2015-error-message"},
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
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SourceError, v1beta1.RenderingError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                300,
				Truncated:                 true,
				ErrorCountAfterTruncation: 6,
			},
		},
		{
			name: "source errors from newer commit",
			sourceStatus: v1beta1.SourceStatus{
				Commit: "newer-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                1,
					Truncated:                 false,
					ErrorCountAfterTruncation: 1,
				},
			},
			renderingStatus: v1beta1.RenderingStatus{
				Commit: "older-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1068", ErrorMessage: "1068-error-message"},
					{Code: "2015", ErrorMessage: "2015-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Commit: "older-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
					{Code: "2009", ErrorMessage: "another error"},
					{Code: "2009", ErrorMessage: "yet-another error"},
				},

				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                4,
					Truncated:                 false,
					ErrorCountAfterTruncation: 4,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.RenderingError, v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                6,
				Truncated:                 false,
				ErrorCountAfterTruncation: 6,
			},
		},
		{
			name: "rendering errors from newer commit",
			sourceStatus: v1beta1.SourceStatus{
				Commit: "newer-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1021", ErrorMessage: "1021-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                1,
					Truncated:                 false,
					ErrorCountAfterTruncation: 1,
				},
			},
			renderingStatus: v1beta1.RenderingStatus{
				Commit: "newer-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "1068", ErrorMessage: "1068-error-message"},
					{Code: "2015", ErrorMessage: "2015-error-message"},
				},
				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                2,
					Truncated:                 false,
					ErrorCountAfterTruncation: 2,
				},
			},
			syncStatus: v1beta1.SyncStatus{
				Commit: "older-commit",
				Errors: []v1beta1.ConfigSyncError{
					{Code: "2009", ErrorMessage: "apiserver error"},
					{Code: "2009", ErrorMessage: "webhook error"},
					{Code: "2009", ErrorMessage: "another error"},
					{Code: "2009", ErrorMessage: "yet-another error"},
				},

				ErrorSummary: &v1beta1.ErrorSummary{
					TotalCount:                4,
					Truncated:                 false,
					ErrorCountAfterTruncation: 4,
				},
			},
			expectedErrorSources: []v1beta1.ErrorSource{v1beta1.SyncError},
			expectedErrorSummary: &v1beta1.ErrorSummary{
				TotalCount:                4,
				Truncated:                 false,
				ErrorCountAfterTruncation: 4,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotErrorSources, gotErrorSummary := summarizeErrorsForCommit(tc.sourceStatus, tc.renderingStatus, tc.syncStatus, tc.syncStatus.Commit)
			if diff := cmp.Diff(tc.expectedErrorSources, gotErrorSources); diff != "" {
				t.Errorf("summarizeErrors() got %v, expected %v", gotErrorSources, tc.expectedErrorSources)
			}
			if diff := cmp.Diff(tc.expectedErrorSummary, gotErrorSummary); diff != "" {
				t.Errorf("summarizeErrors() got %v, expected %v", gotErrorSummary, tc.expectedErrorSummary)
			}
		})
	}
}

func TestPrependRootSyncRemediatorStatus(t *testing.T) {
	const rootSyncName = "my-root-sync"
	const thisManager = "this-manager"
	const otherManager = "other-manager"
	conflictingObject := k8sobjects.NamespaceObject("foo-ns", core.Annotation(metadata.ResourceManagerKey, otherManager))
	conflictAB := status.ManagementConflictErrorWrap(conflictingObject, thisManager)
	invertedObject := k8sobjects.NamespaceObject("foo-ns", core.Annotation(metadata.ResourceManagerKey, thisManager))
	conflictBA := status.ManagementConflictErrorWrap(invertedObject, otherManager)
	conflictABInverted := conflictAB.Invert()
	// KptManagementConflictError is created with the desired object, not the current live object.
	// So its manager annotation matches the reconciler doing the applying.
	// This is the opposite of the objects passed to ManagementConflictErrorWrap.
	kptConflictError := applier.KptManagementConflictError(invertedObject)
	// Assert the value of each error message to make each value clear
	const conflictABMessage = `KNV1060: The "this-manager" reconciler detected a management conflict with the "other-manager" reconciler. Remove the object from one of the sources of truth so that the object is only managed by one reconciler.

metadata.name: foo-ns
group:
version: v1
kind: Namespace

For more information, see https://g.co/cloud/acm-errors#knv1060`
	const conflictBAMessage = `KNV1060: The "other-manager" reconciler detected a management conflict with the "this-manager" reconciler. Remove the object from one of the sources of truth so that the object is only managed by one reconciler.

metadata.name: foo-ns
group:
version: v1
kind: Namespace

For more information, see https://g.co/cloud/acm-errors#knv1060`
	const kptConflictMessage = `KNV1060: The "this-manager" reconciler detected a management conflict with another reconciler. Remove the object from one of the sources of truth so that the object is only managed by one reconciler.

metadata.name: foo-ns
group:
version: v1
kind: Namespace

For more information, see https://g.co/cloud/acm-errors#knv1060`
	testutil.AssertEqual(t, conflictABMessage, conflictAB.ToCSE().ErrorMessage)
	testutil.AssertEqual(t, conflictBAMessage, conflictBA.ToCSE().ErrorMessage)
	testutil.AssertEqual(t, kptConflictMessage, kptConflictError.ToCSE().ErrorMessage)
	testutil.AssertEqual(t, conflictBA, conflictABInverted)
	testCases := map[string]struct {
		thisSyncErrors []v1beta1.ConfigSyncError
		expectedErrors []v1beta1.ConfigSyncError
	}{
		"empty errors": {
			thisSyncErrors: []v1beta1.ConfigSyncError{},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictAB.ToCSE(),
			},
		},
		"unchanged conflict error": {
			thisSyncErrors: []v1beta1.ConfigSyncError{
				conflictAB.ToCSE(),
			},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictAB.ToCSE(),
			},
		},
		"prepend conflict error": {
			thisSyncErrors: []v1beta1.ConfigSyncError{
				{ErrorMessage: "foo"},
			},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictAB.ToCSE(),
				{ErrorMessage: "foo"},
			},
		},
		"dedupe AB and BA errors": {
			thisSyncErrors: []v1beta1.ConfigSyncError{
				conflictBA.ToCSE(),
			},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictBA.ToCSE(),
			},
		},
		"dedupe AB and AB inverted errors": {
			thisSyncErrors: []v1beta1.ConfigSyncError{
				conflictABInverted.ToCSE(),
			},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictABInverted.ToCSE(),
			},
		},
		// TODO: De-dupe ManagementConflictErrorWrap & KptManagementConflictError
		// These are currently de-duped locally by the conflict handler,
		// but not remotely by prependRootSyncRemediatorStatus.
		"prepend KptManagementConflictError": {
			thisSyncErrors: []v1beta1.ConfigSyncError{
				kptConflictError.ToCSE(),
			},
			expectedErrors: []v1beta1.ConfigSyncError{
				conflictAB.ToCSE(),
				kptConflictError.ToCSE(),
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			rootSync := k8sobjects.RootSyncObjectV1Beta1(rootSyncName)
			rootSync.Status.Sync.Errors = tc.thisSyncErrors
			fakeClient := syncertest.NewClient(t, core.Scheme, rootSync)
			ctx := context.Background()
			err := prependRootSyncRemediatorStatus(ctx, fakeClient, rootSyncName,
				[]status.ManagementConflictError{conflictAB}, defaultDenominator)
			require.NoError(t, err)
			var updatedRootSync v1beta1.RootSync
			statuserr := fakeClient.Get(ctx, rootsync.ObjectKey(rootSyncName), &updatedRootSync)
			require.NoError(t, statuserr)
			testutil.AssertEqual(t, tc.expectedErrors, updatedRootSync.Status.Sync.Errors)
		})
	}
}
