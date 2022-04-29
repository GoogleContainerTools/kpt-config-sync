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

package webhook

import (
	"testing"

	authenticationv1 "k8s.io/api/authentication/v1"
)

func TestIsConfigSyncSA(t *testing.T) {
	testCases := []struct {
		name     string
		userInfo authenticationv1.UserInfo
		want     bool
	}{
		{
			name: "Config Sync service account",
			userInfo: authenticationv1.UserInfo{
				Groups: []string{"foogroup", "system:serviceaccounts", "bargroup", "system:serviceaccounts:config-management-system", "bazgroup"},
			},
			want: true,
		},
		{
			name: "Gatekeeper service account",
			userInfo: authenticationv1.UserInfo{
				Groups: []string{"system:serviceaccounts", "system:serviceaccounts:gatekeeper-system"},
			},
			want: false,
		},
		{
			name: "Invalid Config Sync service account",
			userInfo: authenticationv1.UserInfo{
				Groups: []string{"foogroup", "system:serviceaccounts:config-management-system"},
			},
			want: false,
		},
		{
			name: "Invalid service account",
			userInfo: authenticationv1.UserInfo{
				Groups: []string{"foogroup", "system:serviceaccounts"},
			},
			want: false,
		},
		{
			name: "Unauthenticated user",
			userInfo: authenticationv1.UserInfo{
				Groups: []string{},
			},
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isConfigSyncSA(tc.userInfo); got != tc.want {
				t.Errorf("isConfigSyncSA got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestIsImporter(t *testing.T) {
	testCases := []struct {
		name     string
		username string
		want     bool
	}{
		{
			name:     "Config Sync importer service account",
			username: "system:serviceaccount:config-management-system:importer",
			want:     true,
		},
		{
			name:     "Config Sync monitor service account",
			username: "system:serviceaccount:config-management-system:monitor",
			want:     false,
		},
		{
			name:     "Random other service account named importer",
			username: "system:serviceaccount:foo-namespace:importer",
			want:     false,
		},
		{
			name:     "Empty username",
			username: "",
			want:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isImporter(tc.username); got != tc.want {
				t.Errorf("isImporter() got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestIsRootReconciler(t *testing.T) {
	testCases := []struct {
		name     string
		username string
		want     bool
	}{
		{
			name:     "Config Sync root reconciler service account",
			username: "system:serviceaccount:config-management-system:root-reconciler",
			want:     true,
		},
		{
			name:     "Config Sync monitor service account",
			username: "system:serviceaccount:config-management-system:monitor",
			want:     false,
		},
		{
			name:     "Random other service account named root-reconciler",
			username: "system:serviceaccount:foo-namespace:root-reconciler",
			want:     false,
		},
		{
			name:     "Empty username",
			username: "",
			want:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRootReconciler(tc.username); got != tc.want {
				t.Errorf("isRootReconciler() got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestCanManage(t *testing.T) {
	testCases := []struct {
		name     string
		username string
		manager  string
		want     bool
	}{
		{
			name:     "Root reconciler can manage its own object",
			username: "system:serviceaccount:config-management-system:root-reconciler",
			manager:  ":root",
			want:     true,
		},
		{
			name:     "Root reconciler can manage object with any namespace manager",
			username: "system:serviceaccount:config-management-system:root-reconciler",
			manager:  "bookstore",
			want:     true,
		},
		{
			name:     "Root reconciler can not manage object with other root manager",
			username: "system:serviceaccount:config-management-system:root-reconciler",
			manager:  ":root_test-rs",
			want:     false,
		},
		{
			name:     "Root reconciler can manage object with no manager",
			username: "system:serviceaccount:config-management-system:root-reconciler",
			manager:  "",
			want:     true,
		},
		{
			name:     "Namespace reconciler can manage its own object",
			username: "system:serviceaccount:config-management-system:ns-reconciler-bookstore",
			manager:  "bookstore",
			want:     true,
		},
		{
			name:     "Namespace reconciler can not manage object with manager in different namespace",
			username: "system:serviceaccount:config-management-system:ns-reconciler-bookstore",
			manager:  "videostore",
			want:     false,
		},
		{
			name:     "Namespace reconciler can not manage object with different manager in the same namespace",
			username: "system:serviceaccount:config-management-system:ns-reconciler-bookstore",
			manager:  "bookstore_test-rs",
			want:     false,
		},
		{
			name:     "Namespace reconciler can not manage object with any root manager",
			username: "system:serviceaccount:config-management-system:ns-reconciler-bookstore",
			manager:  ":root",
			want:     false,
		},
		{
			name:     "Namespace reconciler can manage object with no manager",
			username: "system:serviceaccount:config-management-system:ns-reconciler-bookstore",
			manager:  "",
			want:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManage(tc.username, tc.manager); got != tc.want {
				t.Errorf("canManage() got %v; want %v", got, tc.want)
			}
		})
	}
}
