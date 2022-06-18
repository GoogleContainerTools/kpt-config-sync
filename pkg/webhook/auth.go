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
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	"kpt.dev/configsync/pkg/api/configmanagement"
)

const (
	// groups use the plural "serviceaccounts"
	saGroup          = "system:serviceaccounts"
	saNamespaceGroup = saGroup + ":" + configmanagement.ControllerNamespace

	// usernames use the singular "serviceaccount"
	saGroupPrefix          = "system:serviceaccount"
	saNamespaceGroupPrefix = saGroupPrefix + ":" + configmanagement.ControllerNamespace + ":"
)

// isConfigSyncSA returns true if the given UserInfo represents a Config Sync
// service account.
func isConfigSyncSA(userInfo authenticationv1.UserInfo) bool {
	foundSA := false
	foundNS := false
	for _, group := range userInfo.Groups {
		switch group {
		case saGroup:
			foundSA = true
		case saNamespaceGroup:
			foundNS = true
		}
	}

	return foundSA && foundNS &&
		strings.HasPrefix(userInfo.Username, saNamespaceGroupPrefix)
}

// configSyncSAName returns the name of the service account, with the prefix
// removed, assuming the prefix is present.
// Run isConfigSyncSA first to detect the presense of the prefix.
func configSyncSAName(userInfo authenticationv1.UserInfo) string {
	return strings.TrimPrefix(userInfo.Username, saNamespaceGroupPrefix)
}
