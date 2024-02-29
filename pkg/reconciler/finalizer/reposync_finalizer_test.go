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

package finalizer

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var repoSync1Yaml = `
apiVersion: configsync.gke.io/v1beta1
kind: RepoSync
metadata:
  name: repo-sync-1
  namespace: example
  uid: "1"
  resourceVersion: "1"
  generation: 1
spec:
  sourceFormat: unstructured
  git:
    repo: https://github.com/config-sync-examples/crontab-crs
    branch: main
    dir: configs
    auth: none
`

func TestRepoSyncFinalize(t *testing.T) {
	repoSync1 := yamlToTypedObject(t, repoSync1Yaml).(*v1beta1.RepoSync)
	repoSync1.SetFinalizers([]string{
		metadata.ReconcilerFinalizer,
	})

	asserter := testutil.NewAsserter(
		cmpopts.EquateErrors(),
		cmpopts.IgnoreFields(metav1.Time{}, "Time"),
	)

	testCases := []struct {
		name                       string
		rsync                      client.Object
		setup                      func(*fake.Client) error
		destroyErrs                status.MultiError
		expectedRsyncBeforeDestroy client.Object
		expectedError              error
		expectedStopped            bool
		expectedRsyncAfterFinalize client.Object
	}{
		{
			name:  "happy path",
			rsync: repoSync1.DeepCopy(),
			expectedRsyncBeforeDestroy: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to set ReconcilerFinalizing condition
				obj.SetResourceVersion("2")
				obj.Status.Conditions = []v1beta1.RepoSyncCondition{
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizing,
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesDeleting",
						Message: "Deleting managed resource objects",
					},
				}
				return obj
			}(),
			expectedError:   nil,
			expectedStopped: true,
			expectedRsyncAfterFinalize: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to set ReconcilerFinalizing condition
				// +1 to remove ReconcilerFinalizing condition
				// +1 to remove Finalizer
				// TODO: optimize by combining consecutive updates
				obj.SetResourceVersion("4")
				// Finalizer has been removed
				obj.SetFinalizers(nil)
				// ReconcilerFinalizing condition added and then removed
				return obj
			}(),
		},
		{
			name:  "destroy failure",
			rsync: repoSync1.DeepCopy(),
			expectedRsyncBeforeDestroy: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to set ReconcilerFinalizing condition
				obj.SetResourceVersion("2")
				obj.Status.Conditions = []v1beta1.RepoSyncCondition{
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizing,
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesDeleting",
						Message: "Deleting managed resource objects",
					},
				}
				return obj
			}(),
			destroyErrs: status.APIServerError(fmt.Errorf("destroy error"), "example message"),
			expectedError: fmt.Errorf(
				"deleting managed objects: %w",
				status.APIServerError(fmt.Errorf("destroy error"), "example message")),
			expectedStopped: true,
			expectedRsyncAfterFinalize: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to set ReconcilerFinalizing condition
				// +1 to set ReconcilerFinalizerFailure condition
				obj.SetResourceVersion("3")
				// Finalizer NOT removed
				// ReconcilerFinalizing condition added and NOT removed
				// ReconcilerFinalizerFailure condition added
				obj.Status.Conditions = []v1beta1.RepoSyncCondition{
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizing,
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesDeleting",
						Message: "Deleting managed resource objects",
					},
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizerFailure,
						Status:  metav1.ConditionTrue,
						Reason:  "DestroyFailure",
						Message: "Failed to delete managed resource objects",
						Errors: []v1beta1.ConfigSyncError{
							{
								Code:         "2002",
								ErrorMessage: "KNV2002: example message: APIServer error: destroy error\n\nFor more information, see https://g.co/cloud/acm-errors#knv2002",
							},
						},
					},
				}
				return obj
			}(),
		},
		{
			name: "destroy recovery",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to set ReconcilerFinalizing condition
				// +1 to set ReconcilerFinalizerFailure condition
				obj.SetResourceVersion("3")
				// Finalizer NOT removed
				// ReconcilerFinalizing condition added and NOT removed
				// ReconcilerFinalizerFailure condition added
				obj.Status.Conditions = []v1beta1.RepoSyncCondition{
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizing,
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesDeleting",
						Message: "Deleting managed resource objects",
					},
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizerFailure,
						Status:  metav1.ConditionTrue,
						Reason:  "DestroyFailure",
						Message: "Failed to delete managed resource objects",
						Errors: []v1beta1.ConfigSyncError{
							{
								Code:         "2002",
								ErrorMessage: "KNV2002: example message: APIServer error: destroy error\n\nFor more information, see https://g.co/cloud/acm-errors#knv2002",
							},
						},
					},
				}
				return obj
			}(),
			expectedRsyncBeforeDestroy: func() client.Object {
				obj := repoSync1.DeepCopy()
				// No changes - continue deleting
				// TODO: Should finalizer re-entry be vissible to the user by toggling a condition?
				obj.SetResourceVersion("3")
				obj.Status.Conditions = []v1beta1.RepoSyncCondition{
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizing,
						Status:  metav1.ConditionTrue,
						Reason:  "ResourcesDeleting",
						Message: "Deleting managed resource objects",
					},
					{
						Type:    v1beta1.RepoSyncReconcilerFinalizerFailure,
						Status:  metav1.ConditionTrue,
						Reason:  "DestroyFailure",
						Message: "Failed to delete managed resource objects",
						Errors: []v1beta1.ConfigSyncError{
							{
								Code:         "2002",
								ErrorMessage: "KNV2002: example message: APIServer error: destroy error\n\nFor more information, see https://g.co/cloud/acm-errors#knv2002",
							},
						},
					},
				}
				return obj
			}(),
			destroyErrs:     nil,
			expectedError:   nil,
			expectedStopped: true,
			expectedRsyncAfterFinalize: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to remove ReconcilerFinalizing condition
				// +1 to remove ReconcilerFinalizerFailure condition
				// +1 to remove Finalizer
				// TODO: optimize by combining consecutive updates
				obj.SetResourceVersion("6")
				// Finalizer has been removed
				obj.SetFinalizers(nil)
				// ReconcilerFinalizing condition removed
				// ReconcilerFinalizerFailure condition removed
				return obj
			}(),
		},
		{
			name:  "rsync not found",
			rsync: repoSync1.DeepCopy(),
			setup: func(fakeClient *fake.Client) error {
				// remove the finalizer that blocks deletion
				ctx := context.Background()
				rs := &v1beta1.RepoSync{}
				rs.Name = repoSync1.Name
				rs.Namespace = repoSync1.Namespace
				if err := updateToRemoveFinalizers(ctx, fakeClient, rs); err != nil {
					return err
				}
				// delete RepoSync to cause update error
				return fakeClient.Delete(ctx, rs)
			},
			expectedError: fmt.Errorf(
				"setting Finalizing condition: %w",
				fmt.Errorf(
					"failed to set ReconcilerFinalizing condition: %w",
					status.APIServerErrorWrap(
						fmt.Errorf(
							"failed to update object status: %s: %w",
							kinds.ObjectSummary(repoSync1),
							apierrors.NewNotFound(
								schema.GroupResource{Group: "configsync.gke.io", Resource: "RepoSync"},
								"example/repo-sync-1"),
						),
						repoSync1.DeepCopy(),
					),
				),
			),
			expectedStopped:            true,
			expectedRsyncAfterFinalize: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClient(t, scheme, tc.rsync)
			ctx := context.Background()

			stopped := false
			continueCh := make(chan struct{})
			stopFunc := func() {
				defer close(continueCh)
				stopped = true
			}
			destroyFunc := func(context.Context) status.MultiError {
				// Lookup the current RepoSync
				key := client.ObjectKeyFromObject(repoSync1)
				rsync := &v1beta1.RepoSync{}
				err := fakeClient.Get(context.Background(), key, rsync)
				require.NoError(t, err)
				asserter.Equal(t, tc.expectedRsyncBeforeDestroy, rsync)
				// Return errors, if any
				return tc.destroyErrs
			}
			fakeDestroyer := newFakeDestroyer(tc.destroyErrs, destroyFunc)
			finalizer := &RepoSyncFinalizer{
				Destroyer:          fakeDestroyer,
				Client:             fakeClient,
				StopControllers:    stopFunc,
				ControllersStopped: continueCh,
			}

			if tc.setup != nil {
				err := tc.setup(fakeClient)
				require.NoError(t, err)
			}

			err := finalizer.Finalize(ctx, tc.rsync)
			if tc.expectedError != nil && err != nil {
				// AssertEqual is too lenient on APIServerError, because
				// baseErrorImpl.Is only checks the error code.
				// So check the error message and type, instead.
				assert.Equal(t, tc.expectedError.Error(), err.Error())
				assert.IsType(t, tc.expectedError, err)
			} else {
				testutil.AssertEqual(t, tc.expectedError, err)
			}

			assert.Equal(t, tc.expectedStopped, stopped)
			var expectedObjs []client.Object
			if tc.expectedRsyncAfterFinalize != nil {
				expectedObjs = append(expectedObjs, tc.expectedRsyncAfterFinalize)
			}
			fakeClient.Check(t, expectedObjs...)
		})
	}
}

func TestRepoSyncAddFinalizer(t *testing.T) {
	repoSync1 := yamlToTypedObject(t, repoSync1Yaml).(*v1beta1.RepoSync)

	testCases := []struct {
		name            string
		rsync           client.Object
		setup           func(*fake.Client) error
		expectedError   error
		expectedUpdated bool
		expectedRsync   client.Object
	}{
		{
			name:            "add finalizer",
			rsync:           repoSync1.DeepCopy(),
			expectedError:   nil,
			expectedUpdated: true,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to add Finalizer
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
		},
		{
			name: "add finalizer again",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
			expectedError:   nil,
			expectedUpdated: false,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// No change
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
		},
		{
			name: "add finalizer to list",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					"some-other-finalizer",
				})
				return obj
			}(),
			expectedError:   nil,
			expectedUpdated: true,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to add Finalizer
				obj.SetResourceVersion("3")
				obj.SetFinalizers([]string{
					"some-other-finalizer",
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
		},
		{
			name:  "rsync not found",
			rsync: repoSync1.DeepCopy(),
			setup: func(fakeClient *fake.Client) error {
				// remove the finalizer that blocks deletion
				ctx := context.Background()
				rs := &v1beta1.RepoSync{}
				rs.Name = repoSync1.Name
				rs.Namespace = repoSync1.Namespace
				if err := updateToRemoveFinalizers(ctx, fakeClient, rs); err != nil {
					return err
				}
				// delete RepoSync to cause update error
				return fakeClient.Delete(ctx, rs)
			},
			expectedError: fmt.Errorf(
				"failed to add finalizer: %w",
				status.APIServerErrorWrap(
					fmt.Errorf(
						"failed to update object: %s: %w",
						kinds.ObjectSummary(repoSync1),
						apierrors.NewNotFound(
							schema.GroupResource{Group: "configsync.gke.io", Resource: "RepoSync"},
							"example/repo-sync-1")),
					repoSync1.DeepCopy()),
			),
			expectedUpdated: false,
			expectedRsync:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClient(t, scheme, tc.rsync)
			ctx := context.Background()

			finalizer := &RepoSyncFinalizer{
				Client: fakeClient,
			}

			if tc.setup != nil {
				err := tc.setup(fakeClient)
				require.NoError(t, err)
			}

			updated, err := finalizer.AddFinalizer(ctx, tc.rsync)
			if tc.expectedError != nil && err != nil {
				// AssertEqual doesn't work well on APIServerError, because the
				// Error.Is impl is too lenient. So check the error message and
				// type, instead.
				assert.Equal(t, tc.expectedError.Error(), err.Error())
				assert.IsType(t, tc.expectedError, err)
			} else {
				testutil.AssertEqual(t, tc.expectedError, err)
			}

			assert.Equal(t, tc.expectedUpdated, updated)
			var expectedObjs []client.Object
			if tc.expectedRsync != nil {
				expectedObjs = append(expectedObjs, tc.expectedRsync)
			}
			fakeClient.Check(t, expectedObjs...)
		})
	}
}

func TestRepoSyncRemoveFinalizer(t *testing.T) {
	repoSync1 := yamlToTypedObject(t, repoSync1Yaml).(*v1beta1.RepoSync)

	testCases := []struct {
		name            string
		rsync           client.Object
		setup           func(*fake.Client) error
		expectedError   error
		expectedUpdated bool
		expectedRsync   client.Object
	}{
		{
			name: "remove finalizer",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
			expectedError:   nil,
			expectedUpdated: true,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to remove Finalizer
				obj.SetResourceVersion("3")
				obj.SetFinalizers(nil)
				return obj
			}(),
		},
		{
			name: "remove finalizer again",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to remove Finalizer
				obj.SetResourceVersion("3")
				obj.SetFinalizers(nil)
				return obj
			}(),
			expectedError:   nil,
			expectedUpdated: false,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// No change
				obj.SetResourceVersion("3")
				obj.SetFinalizers(nil)
				return obj
			}(),
		},
		{
			name: "remove finalizer from list",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				obj.SetResourceVersion("2")
				obj.SetFinalizers([]string{
					"some-other-finalizer",
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
			expectedError:   nil,
			expectedUpdated: true,
			expectedRsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				// +1 to add Finalizer
				obj.SetResourceVersion("3")
				obj.SetFinalizers([]string{
					"some-other-finalizer",
				})
				return obj
			}(),
		},
		{
			name: "rsync not found",
			rsync: func() client.Object {
				obj := repoSync1.DeepCopy()
				obj.SetFinalizers([]string{
					metadata.ReconcilerFinalizer,
				})
				return obj
			}(),
			setup: func(fakeClient *fake.Client) error {
				// remove the finalizer that blocks deletion
				ctx := context.Background()
				rs := &v1beta1.RepoSync{}
				rs.Name = repoSync1.Name
				rs.Namespace = repoSync1.Namespace
				if err := updateToRemoveFinalizers(ctx, fakeClient, rs); err != nil {
					return err
				}
				// delete RepoSync to cause update error
				return fakeClient.Delete(ctx, rs)
			},
			expectedError: fmt.Errorf(
				"failed to remove finalizer: %w",
				status.APIServerErrorWrap(
					fmt.Errorf(
						"failed to update object: %s: %w",
						kinds.ObjectSummary(repoSync1),
						apierrors.NewNotFound(
							schema.GroupResource{Group: "configsync.gke.io", Resource: "RepoSync"},
							"example/repo-sync-1")),
					repoSync1.DeepCopy()),
			),
			expectedUpdated: false,
			expectedRsync:   nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := fake.NewClient(t, scheme, tc.rsync)
			ctx := context.Background()

			finalizer := &RepoSyncFinalizer{
				Client: fakeClient,
			}

			if tc.setup != nil {
				err := tc.setup(fakeClient)
				require.NoError(t, err)
			}

			updated, err := finalizer.RemoveFinalizer(ctx, tc.rsync)
			if tc.expectedError != nil && err != nil {
				// AssertEqual doesn't work well on APIServerError, because the
				// Error.Is impl is too lenient. So check the error message and
				// type, instead.
				assert.Equal(t, tc.expectedError.Error(), err.Error())
				assert.IsType(t, tc.expectedError, err)
			} else {
				testutil.AssertEqual(t, tc.expectedError, err)
			}

			assert.Equal(t, tc.expectedUpdated, updated)
			var expectedObjs []client.Object
			if tc.expectedRsync != nil {
				expectedObjs = append(expectedObjs, tc.expectedRsync)
			}
			fakeClient.Check(t, expectedObjs...)
		})
	}
}
