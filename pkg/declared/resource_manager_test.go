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

package declared

import (
	"testing"

	"kpt.dev/configsync/pkg/api/configsync"
)

func TestResourceManager(t *testing.T) {
	testCases := []struct {
		name     string
		scope    Scope
		syncName string
		want     string
	}{
		{
			name:     "default root-sync",
			scope:    RootReconciler,
			syncName: configsync.RootSyncName,
			want:     string(RootReconciler),
		},
		{
			name:     "default repo-sync",
			scope:    Scope("test-ns"),
			syncName: configsync.RepoSyncName,
			want:     "test-ns",
		},
		{
			name:     "custom root-sync",
			scope:    RootReconciler,
			syncName: "test-root-sync",
			want:     ":root_test-root-sync",
		},
		{
			name:     "custom repo-sync",
			scope:    Scope("test-ns"),
			syncName: "test-repo-sync",
			want:     "test-ns_test-repo-sync",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResourceManager(tc.scope, tc.syncName)
			if got != tc.want {
				t.Errorf("got manager %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsRootManager(t *testing.T) {
	testCases := []struct {
		name    string
		manager string
		want    bool
	}{
		{
			name:    "default root-sync",
			manager: string(RootReconciler),
			want:    true,
		},
		{
			name:    "default repo-sync",
			manager: "test-ns",
			want:    false,
		},
		{
			name:    "custom root-sync",
			manager: ":root_test-root-sync",
			want:    true,
		},
		{
			name:    "custom repo-sync",
			manager: "test-ns_test-repo-sync",
			want:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsRootManager(tc.manager)
			if got != tc.want {
				t.Errorf("IsRootManager, got %t, want %t", got, tc.want)
			}
		})
	}
}

// ManagerScopeAndName returns the scope and name of the resource manager.
func TestManagerScopeAndName(t *testing.T) {
	testCases := []struct {
		name      string
		manager   string
		wantScope Scope
		wantName  string
	}{
		{
			name:      "default root-sync",
			manager:   string(RootReconciler),
			wantScope: RootReconciler,
			wantName:  configsync.RootSyncName,
		},
		{
			name:      "default repo-sync",
			manager:   "test-ns",
			wantScope: "test-ns",
			wantName:  configsync.RepoSyncName,
		},
		{
			name:      "custom root-sync",
			manager:   ":root_test-root-sync",
			wantScope: RootReconciler,
			wantName:  "test-root-sync",
		},
		{
			name:      "custom repo-sync",
			manager:   "test-ns_test-repo-sync",
			wantScope: "test-ns",
			wantName:  "test-repo-sync",
		},
		{
			name:      "manager unset",
			manager:   "",
			wantScope: "",
			wantName:  "",
		},
		{
			name:      "invalid manager with an extra _",
			manager:   "test-ns_test-rs_ignored",
			wantScope: "test-ns",
			wantName:  "test-rs",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotScope, gotName := ManagerScopeAndName(tc.manager)
			if gotScope != tc.wantScope {
				t.Errorf("Verify manager scope, got %q, want %q", gotScope, tc.wantScope)
			}
			if gotName != tc.wantName {
				t.Errorf("Verify manager name, got %q, want %q", gotName, tc.wantName)
			}
		})
	}
}
