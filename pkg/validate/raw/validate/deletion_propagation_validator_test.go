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

package validate

import (
	"testing"

	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDeletionPropagationAnnotation(t *testing.T) {
	rootSyncDeletionPropagationError := NewDeletionPropagationAnnotationError(configsync.RootSyncKind)
	repoSyncDeletionPropagationError := NewDeletionPropagationAnnotationError(configsync.RepoSyncKind)

	testCases := []struct {
		name     string
		obj      client.Object
		syncKind string
		wantErr  status.Error
	}{
		{
			name:     "RootSync no annotation passes",
			obj:      k8sobjects.RootSyncObjectV1Beta1("test-name"),
			syncKind: configsync.RootSyncKind,
		},
		{
			name:     "RepoSync no annotation passes",
			obj:      k8sobjects.RepoSyncObjectV1Beta1("test-ns", "test-name"),
			syncKind: configsync.RepoSyncKind,
		},
		{
			name: "RootSync foreground passes",
			obj: k8sobjects.RootSyncObjectV1Beta1("test-name",
				metadata.WithDeletionPropagationPolicy(metadata.DeletionPropagationPolicyForeground)),
			syncKind: configsync.RootSyncKind,
		},
		{
			name: "RepoSync foreground passes",
			obj: k8sobjects.RepoSyncObjectV1Beta1("test-ns", "test-name",
				metadata.WithDeletionPropagationPolicy(metadata.DeletionPropagationPolicyForeground)),
			syncKind: configsync.RepoSyncKind,
		},
		{
			name: "RootSync orphan passes",
			obj: k8sobjects.RootSyncObjectV1Beta1("test-name",
				metadata.WithDeletionPropagationPolicy(metadata.DeletionPropagationPolicyOrphan)),
			syncKind: configsync.RootSyncKind,
		},
		{
			name: "RepoSync orphan passes",
			obj: k8sobjects.RepoSyncObjectV1Beta1("test-ns", "test-name",
				metadata.WithDeletionPropagationPolicy(metadata.DeletionPropagationPolicyOrphan)),
			syncKind: configsync.RepoSyncKind,
		},
		{
			name: "RootSync empty annotation fails",
			obj: k8sobjects.RootSyncObjectV1Beta1("test-name",
				metadata.WithDeletionPropagationPolicy("")),
			syncKind: configsync.RootSyncKind,
			wantErr:  rootSyncDeletionPropagationError,
		},
		{
			name: "RepoSync empty annotation fails",
			obj: k8sobjects.RepoSyncObjectV1Beta1("test-ns", "test-name",
				metadata.WithDeletionPropagationPolicy("")),
			syncKind: configsync.RepoSyncKind,
			wantErr:  repoSyncDeletionPropagationError,
		},
		{
			name: "RootSync invalid annotation fails",
			obj: k8sobjects.RootSyncObjectV1Beta1("test-name",
				metadata.WithDeletionPropagationPolicy("invalid")),
			syncKind: configsync.RootSyncKind,
			wantErr:  rootSyncDeletionPropagationError,
		},
		{
			name: "RepoSync invalid annotation fails",
			obj: k8sobjects.RepoSyncObjectV1Beta1("test-ns", "test-name",
				metadata.WithDeletionPropagationPolicy("invalid")),
			syncKind: configsync.RepoSyncKind,
			wantErr:  repoSyncDeletionPropagationError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := DeletionPropagationAnnotation(tc.obj, tc.syncKind)
			testerrors.AssertEqual(t, tc.wantErr, err)
		})
	}
}
