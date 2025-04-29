// Copyright 2025 Google LLC
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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

func TestSyncStatusController(t *testing.T) {
	// Register the v1beta1 scheme
	s := runtime.NewScheme()
	assert.NoError(t, scheme.AddToScheme(s))
	assert.NoError(t, v1beta1.AddToScheme(s))

	tests := []struct {
		name           string
		syncKind       string
		setupObjects   []client.Object
		expectedLogs   bool
		expectedError  bool
		expectedCommit string
	}{
		{
			name:     "RootSync with error",
			syncKind: "RootSync",
			setupObjects: []client.Object{
				&v1beta1.RootSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rootsync",
						Namespace: "config-management-system",
					},
					Status: v1beta1.RootSyncStatus{
						Status: v1beta1.Status{
							Source: v1beta1.SourceStatus{
								Commit: "abc123",
								Errors: []v1beta1.ConfigSyncError{
									{
										Code:         "1234",
										ErrorMessage: "test error",
									},
								},
							},
						},
					},
				},
			},
			expectedLogs:   true,
			expectedError:  false,
			expectedCommit: "abc123",
		},
		{
			name:     "RepoSync with error",
			syncKind: "RepoSync",
			setupObjects: []client.Object{
				&v1beta1.RepoSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-reposync",
						Namespace: "test-namespace",
					},
					Status: v1beta1.RepoSyncStatus{
						Status: v1beta1.Status{
							Source: v1beta1.SourceStatus{
								Commit: "def456",
								Errors: []v1beta1.ConfigSyncError{
									{
										Code:         "5678",
										ErrorMessage: "test error",
									},
								},
							},
						},
					},
				},
			},
			expectedLogs:   true,
			expectedError:  false,
			expectedCommit: "def456",
		},
		{
			name:     "RootSync without error",
			syncKind: "RootSync",
			setupObjects: []client.Object{
				&v1beta1.RootSync{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rootsync-success",
						Namespace: "config-management-system",
					},
					Status: v1beta1.RootSyncStatus{
						Status: v1beta1.Status{
							Source: v1beta1.SourceStatus{
								Commit: "ghi789",
							},
							Rendering: v1beta1.RenderingStatus{
								Commit: "ghi789",
							},
							Sync: v1beta1.SyncStatus{
								Commit: "ghi789",
							},
						},
					},
				},
			},
			expectedLogs:   false,
			expectedError:  false,
			expectedCommit: "ghi789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client with the test objects
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(tt.setupObjects...).
				Build()

			// Create a test logger
			logger := testr.New(t)

			// Create a status tracker
			statusTracker := NewStatusTracker()

			// Create the controller
			controller := NewSyncStatusController(client, logger, statusTracker, tt.syncKind)

			// Create the request
			var req reconcile.Request
			switch tt.syncKind {
			case "RootSync":
				req = reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-rootsync",
						Namespace: "config-management-system",
					},
				}
				if tt.name == "RootSync without error" {
					req.Name = "test-rootsync-success"
				}
			case "RepoSync":
				req = reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      "test-reposync",
						Namespace: "test-namespace",
					},
				}
			}

			// Run the reconciliation
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			_, err := controller.Reconcile(ctx, req)

			// Check the results
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Check if errors were logged as expected
			if tt.expectedLogs {
				// Get the sync object to verify its state
				var syncID SyncID
				switch tt.syncKind {
				case "RootSync":
					syncID = SyncID{
						Name:      req.Name,
						Kind:      "RootSync",
						Namespace: "config-management-system",
					}
				case "RepoSync":
					syncID = SyncID{
						Name:      req.Name,
						Kind:      "RepoSync",
						Namespace: "test-namespace",
					}
				}

				// Verify that the error was logged
				var expectedError string
				if tt.syncKind == "RootSync" {
					expectedError = "aggregated errors: Code: 1234, Message: test error"
				} else {
					expectedError = "aggregated errors: Code: 5678, Message: test error"
				}

				assert.True(t, statusTracker.IsLogged(syncID, tt.expectedCommit, expectedError))

				// Test that a different error message or different sync resource is not logged
				differentSyncID := SyncID{
					Name:      syncID.Name + "-different",
					Kind:      syncID.Kind,
					Namespace: syncID.Namespace,
				}
				assert.False(t, statusTracker.IsLogged(differentSyncID, tt.expectedCommit, expectedError), "Different sync resource should not be logged")
				assert.False(t, statusTracker.IsLogged(syncID, tt.expectedCommit, "different error"), "Different error should not be logged")
			}
		})
	}
}
