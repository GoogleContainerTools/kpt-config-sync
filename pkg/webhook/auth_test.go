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
				Groups:   []string{"foogroup", "system:serviceaccounts", "bargroup", "system:serviceaccounts:config-management-system", "bazgroup"},
				Username: "system:serviceaccount:config-management-system:unused",
			},
			want: true,
		},
		{
			name: "Config Sync service account with wrong username",
			userInfo: authenticationv1.UserInfo{
				Groups:   []string{"foogroup", "system:serviceaccounts", "bargroup", "system:serviceaccounts:config-management-system", "bazgroup"},
				Username: "system:serviceaccount:wrong-namespace:root-reconciler",
			},
			want: false,
		},
		{
			name: "Gatekeeper service account",
			userInfo: authenticationv1.UserInfo{
				Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:gatekeeper-system"},
				Username: "system:serviceaccount:gatekeeper-system:unused",
			},
			want: false,
		},
		{
			name: "Invalid Config Sync service account",
			userInfo: authenticationv1.UserInfo{
				Groups:   []string{"foogroup", "system:serviceaccounts:config-management-system"},
				Username: "unreachable",
			},
			want: false,
		},
		{
			name: "Invalid service account",
			userInfo: authenticationv1.UserInfo{
				Groups:   []string{"foogroup", "system:serviceaccounts"},
				Username: "unreachable",
			},
			want: false,
		},
		{
			name: "Unauthenticated user",
			userInfo: authenticationv1.UserInfo{
				Groups:   []string{},
				Username: "unreachable",
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
