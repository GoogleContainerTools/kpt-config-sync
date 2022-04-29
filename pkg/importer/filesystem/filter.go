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

package filesystem

import (
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
)

func isHierarchyFile(root cmpath.Absolute, file cmpath.Absolute) bool {
	fileSplits := file.Split()
	rootSplits := root.Split()
	if len(fileSplits) <= len(rootSplits) {
		return false
	}
	for i := range rootSplits {
		if fileSplits[i] != rootSplits[i] {
			return false
		}
	}

	return fileSplits[len(rootSplits)] == repo.SystemDir ||
		fileSplits[len(rootSplits)] == repo.ClusterDir ||
		fileSplits[len(rootSplits)] == repo.ClusterRegistryDir ||
		fileSplits[len(rootSplits)] == repo.NamespacesDir
}

// FilterHierarchyFiles filters out files that aren't in a top-level directory
// we care about.
// root and files are all absolute paths.
func FilterHierarchyFiles(root cmpath.Absolute, files []cmpath.Absolute) []cmpath.Absolute {
	var result []cmpath.Absolute
	for _, file := range files {
		if isHierarchyFile(root, file) {
			result = append(result, file)
		}
	}
	return result
}
