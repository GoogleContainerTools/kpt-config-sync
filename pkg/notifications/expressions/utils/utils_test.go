// Copyright 2023 Google LLC
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

package utils

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
)

func TestLastCommit(t *testing.T) {
	testCases := []struct {
		description string
		obj         runtime.Object
		apiKind     string
		expected    string
	}{
		{
			description: "LastCommit for a RootSync",
			obj: &v1beta1.RootSync{
				Status: v1beta1.RootSyncStatus{
					Conditions: []v1beta1.RootSyncCondition{
						{
							Type:   v1beta1.RootSyncReconciling,
							Commit: "foo",
						},
						{
							Type:   v1beta1.RootSyncSyncing,
							Commit: "bar",
						},
					},
				},
			},
			apiKind:  configsync.RootSyncKind,
			expected: "bar",
		},
		{
			description: "LastCommit for a RootSync with no Syncing condition",
			obj: &v1beta1.RootSync{
				Status: v1beta1.RootSyncStatus{
					Conditions: []v1beta1.RootSyncCondition{
						{
							Type:   v1beta1.RootSyncReconciling,
							Commit: "foo",
						},
					},
				},
			},
			apiKind:  configsync.RootSyncKind,
			expected: "",
		},
		{
			description: "LastCommit for a RepoSync",
			obj: &v1beta1.RepoSync{
				Status: v1beta1.RepoSyncStatus{
					Conditions: []v1beta1.RepoSyncCondition{
						{
							Type:   v1beta1.RepoSyncReconciling,
							Commit: "foo",
						},
						{
							Type:   v1beta1.RepoSyncSyncing,
							Commit: "bar",
						},
					},
				},
			},
			apiKind:  configsync.RepoSyncKind,
			expected: "bar",
		},
		{
			description: "LastCommit for a RepoSync with no Syncing condition",
			obj: &v1beta1.RepoSync{
				Status: v1beta1.RepoSyncStatus{
					Conditions: []v1beta1.RepoSyncCondition{
						{
							Type:   v1beta1.RepoSyncReconciling,
							Commit: "foo",
						},
					},
				},
			},
			apiKind:  configsync.RepoSyncKind,
			expected: "",
		},
		{
			description: "LastCommit for an unsupported kind",
			obj:         &corev1.ConfigMap{Data: map[string]string{}},
			apiKind:     configsync.RepoSyncKind,
			expected:    "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			un, err := kinds.ToUnstructured(tc.obj, core.Scheme)
			if err != nil {
				t.Fatal(err)
			}
			output := lastCommit(un, tc.apiKind)()
			if output != tc.expected {
				t.Errorf("want %s, got %s", tc.expected, output)
			}
		})
	}
}

func TestConfigSyncErrors(t *testing.T) {
	testCases := []struct {
		description string
		obj         runtime.Object
		apiKind     string
		expected    []v1beta1.ConfigSyncError
	}{
		{
			description: "multiple errors for a RootSync",
			obj: &v1beta1.RootSync{
				Status: v1beta1.RootSyncStatus{
					Status: v1beta1.Status{
						Source: v1beta1.SourceStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "source error 1"},
								{ErrorMessage: "source error 2"},
							},
						},
						Rendering: v1beta1.RenderingStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "rendering error 1"},
								{ErrorMessage: "rendering error 2"},
							},
						},
						Sync: v1beta1.SyncStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "sync error 1"},
								{ErrorMessage: "sync error 2"},
							},
						},
					},
				},
			},
			apiKind: configsync.RootSyncKind,
			expected: []v1beta1.ConfigSyncError{
				{ErrorMessage: "source error 1"},
				{ErrorMessage: "source error 2"},
				{ErrorMessage: "rendering error 1"},
				{ErrorMessage: "rendering error 2"},
				{ErrorMessage: "sync error 1"},
				{ErrorMessage: "sync error 2"},
			},
		},
		{
			description: "sync errors for a RootSync",
			obj: &v1beta1.RootSync{
				Status: v1beta1.RootSyncStatus{
					Status: v1beta1.Status{
						Source: v1beta1.SourceStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "source error 1"},
								{ErrorMessage: "source error 2"},
							},
						},
					},
				},
			},
			apiKind: configsync.RootSyncKind,
			expected: []v1beta1.ConfigSyncError{
				{ErrorMessage: "source error 1"},
				{ErrorMessage: "source error 2"},
			},
		},
		{
			description: "multiple errors for a RepoSync",
			obj: &v1beta1.RepoSync{
				Status: v1beta1.RepoSyncStatus{
					Status: v1beta1.Status{
						Source: v1beta1.SourceStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "source error 1"},
								{ErrorMessage: "source error 2"},
							},
						},
						Rendering: v1beta1.RenderingStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "rendering error 1"},
								{ErrorMessage: "rendering error 2"},
							},
						},
						Sync: v1beta1.SyncStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "sync error 1"},
								{ErrorMessage: "sync error 2"},
							},
						},
					},
				},
			},
			apiKind: configsync.RepoSyncKind,
			expected: []v1beta1.ConfigSyncError{
				{ErrorMessage: "source error 1"},
				{ErrorMessage: "source error 2"},
				{ErrorMessage: "rendering error 1"},
				{ErrorMessage: "rendering error 2"},
				{ErrorMessage: "sync error 1"},
				{ErrorMessage: "sync error 2"},
			},
		},
		{
			description: "sync errors for a RepoSync",
			obj: &v1beta1.RepoSync{
				Status: v1beta1.RepoSyncStatus{
					Status: v1beta1.Status{
						Source: v1beta1.SourceStatus{
							Errors: []v1beta1.ConfigSyncError{
								{ErrorMessage: "source error 1"},
								{ErrorMessage: "source error 2"},
							},
						},
					},
				},
			},
			apiKind: configsync.RepoSyncKind,
			expected: []v1beta1.ConfigSyncError{
				{ErrorMessage: "source error 1"},
				{ErrorMessage: "source error 2"},
			},
		},
		{
			description: "errors for an unsupported kind",
			obj:         &corev1.ConfigMap{Data: map[string]string{}},
			apiKind:     configsync.RepoSyncKind,
			expected:    nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			un, err := kinds.ToUnstructured(tc.obj, core.Scheme)
			if err != nil {
				t.Fatal(err)
			}
			output := configSyncErrors(un, tc.apiKind)()
			if !reflect.DeepEqual(tc.expected, output) {
				t.Fatalf("want %v, got %v", tc.expected, output)
			}
		})
	}
}
