// Copyright 2026 Google LLC
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

package status

import (
	"strings"
	"testing"

	kptv1alpha1 "github.com/GoogleContainerTools/config-sync/pkg/api/kpt.dev/v1alpha1"
)

func TestPollUntilReached(t *testing.T) {
	tests := []struct {
		name    string
		states  map[string]*ClusterState
		want    bool
		wantErr bool
	}{
		{
			name: "all repositories synced",
			states: map[string]*ClusterState{
				"cluster": {
					repos: []*RepoState{
						{status: syncedMsg},
					},
				},
			},
			want: true,
		},
		{
			name: "pending repository",
			states: map[string]*ClusterState{
				"cluster": {
					repos: []*RepoState{
						{status: pendingMsg},
					},
				},
			},
		},
		{
			name: "repository with non-current resource",
			states: map[string]*ClusterState{
				"cluster": {
					repos: []*RepoState{
						{
							status: syncedMsg,
							resources: []kptv1alpha1.ResourceStatus{
								{Status: kptv1alpha1.Failed},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "cluster error",
			states: map[string]*ClusterState{
				"cluster": {
					Error: "unavailable",
					repos: []*RepoState{
						{status: syncedMsg},
					},
				},
			},
		},
		{
			name: "empty reachable cluster has nothing to wait for",
			states: map[string]*ClusterState{
				"cluster": {},
			},
			want: true,
		},
		{
			name: "cluster without sync objects does not block another cluster",
			states: map[string]*ClusterState{
				"empty-cluster":  {Error: "No RootSync resources found\nNo RepoSync resources found", noSyncObjects: true},
				"active-cluster": {repos: []*RepoState{{status: syncedMsg}}},
			},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := pollUntilReached(test.states, "", pollUntilCurrent)
			if (err != nil) != test.wantErr {
				t.Fatalf("pollUntilReached() error = %v, want error %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("pollUntilReached() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPollUntilReachedRequiresCurrentResourceForCurrentMode(t *testing.T) {
	tests := []struct {
		name          string
		resourceState kptv1alpha1.Status
		want          bool
		wantErr       bool
	}{
		{name: "current resource", resourceState: kptv1alpha1.Current, want: true},
		{name: "unknown resource", resourceState: kptv1alpha1.Unknown, want: false},
		{name: "failed resource", resourceState: kptv1alpha1.Failed, want: false, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			states := map[string]*ClusterState{
				"cluster": {repos: []*RepoState{{
					status:    syncedMsg,
					resources: []kptv1alpha1.ResourceStatus{{Status: test.resourceState}},
				}}},
			}
			got, err := pollUntilReached(states, "", pollUntilCurrent)
			if (err != nil) != test.wantErr {
				t.Fatalf("pollUntilReached() error = %v, want error %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("pollUntilReached() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPollUntilReachedFailsForFailedResourceInCurrentMode(t *testing.T) {
	states := map[string]*ClusterState{
		"cluster": {repos: []*RepoState{{
			scope:    "apps",
			syncName: "payments",
			status:   syncedMsg,
			resources: []kptv1alpha1.ResourceStatus{{
				ObjMetadata: kptv1alpha1.ObjMetadata{
					GroupKind: kptv1alpha1.GroupKind{Group: "apps", Kind: "Deployment"},
					Name:      "checkout",
				},
				Status: kptv1alpha1.Failed,
			}},
		}}},
	}

	done, err := pollUntilReached(states, "", pollUntilCurrent)
	if err == nil {
		t.Fatal("pollUntilReached() returned no error for a failed resource")
	}
	if done {
		t.Fatal("pollUntilReached() reported completion for a failed resource")
	}
	if !strings.Contains(err.Error(), "deployment.apps/checkout") {
		t.Fatalf("pollUntilReached() error = %q, want the failed resource identified", err)
	}
}

func TestPollUntilReachedUsesNameFilter(t *testing.T) {
	states := map[string]*ClusterState{
		"cluster": {
			repos: []*RepoState{
				{syncName: "selected", status: syncedMsg},
				{syncName: "other", status: pendingMsg},
			},
		},
	}
	got, err := pollUntilReached(states, "selected", pollUntilCurrent)
	if err != nil {
		t.Fatalf("pollUntilReached() returned unexpected error: %v", err)
	}
	if !got {
		t.Fatal("pollUntilReached() = false, want true when the selected repo is synced")
	}
}

func TestPollUntilReachedWaitsForNamedRepoToAppear(t *testing.T) {
	states := map[string]*ClusterState{"cluster": {repos: []*RepoState{{syncName: "other", status: syncedMsg}}}}
	if got, err := pollUntilReached(states, "selected", pollUntilCurrent); err != nil || got {
		t.Fatalf("pollUntilReached() = (%v, %v), want (false, nil) when no repo matches", got, err)
	}
}

func TestPollUntilReachedSyncedTargetIgnoresResourceReadiness(t *testing.T) {
	states := map[string]*ClusterState{
		"cluster": {
			repos: []*RepoState{{
				syncName: "selected",
				status:   syncedMsg,
				resources: []kptv1alpha1.ResourceStatus{
					{Status: kptv1alpha1.Unknown},
				},
			}},
		},
	}
	got, err := pollUntilReached(states, "", pollUntilSynced)
	if err != nil {
		t.Fatalf("pollUntilReached() returned unexpected error: %v", err)
	}
	if !got {
		t.Fatal("pollUntilReached() = false, want true when sync completed and mode is synced")
	}
}

func TestValidatePollUntil(t *testing.T) {
	if err := validatePollUntil(""); err != nil {
		t.Fatalf("validatePollUntil(\"\") returned error: %v", err)
	}
	if err := validatePollUntil(pollUntilComplete); err != nil {
		t.Fatalf("validatePollUntil(%q) returned error: %v", pollUntilComplete, err)
	}
	for _, value := range []string{pollUntilSynced, pollUntilCurrent} {
		if err := validatePollUntil(value); err != nil {
			t.Errorf("validatePollUntil(%q) returned error: %v", value, err)
		}
	}
	if err := validatePollUntil("ready"); err == nil {
		t.Fatal("validatePollUntil(\"ready\") returned nil")
	}
}
