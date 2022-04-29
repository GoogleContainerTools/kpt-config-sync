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

package system

import (
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/repo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OldAllowedRepoVersion is the old (but still supported) Repo.Spec.Version.
const OldAllowedRepoVersion = "0.1.0"

// UnsupportedRepoSpecVersionCode is the error code for UnsupportedRepoSpecVersion
const UnsupportedRepoSpecVersionCode = "1027"

var unsupportedRepoSpecVersion = status.NewErrorBuilder(UnsupportedRepoSpecVersionCode)

// UnsupportedRepoSpecVersion reports that the repo version is not supported.
func UnsupportedRepoSpecVersion(resource client.Object, version string) status.Error {
	return unsupportedRepoSpecVersion.
		Sprintf(`Unsupported Repo spec.version: %q. Must use version %q`, version, repo.CurrentVersion).
		BuildWithResources(resource)
}
