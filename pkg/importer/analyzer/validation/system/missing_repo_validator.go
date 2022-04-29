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
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
)

// MissingRepoErrorCode is the error code for MissingRepoError
const MissingRepoErrorCode = "1017"

var missingRepoError = status.NewErrorBuilder(MissingRepoErrorCode)

// MissingRepoError reports that there is no Repo definition in system/
func MissingRepoError() status.Error {
	return missingRepoError.
		Sprintf("The %s/ directory must declare a Repo Resource.", repo.SystemDir).
		BuildWithPaths(cmpath.RelativeSlash(repo.SystemDir))
}
