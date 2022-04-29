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

package status

import (
	"path/filepath"

	"kpt.dev/configsync/pkg/importer/id"
)

// PathErrorCode is the error code for a generic PathError.
const PathErrorCode = "2001"

var pathError = NewErrorBuilder(PathErrorCode)

type path struct {
	slashPath string
}

var _ id.Path = path{}

// OSPath implements id.Path.
func (p path) OSPath() string {
	return filepath.FromSlash(p.slashPath)
}

// SlashPath implements id.Path.
func (p path) SlashPath() string {
	return p.slashPath
}

// PathWrapError returns a PathError wrapping an error one or more relative paths.
func PathWrapError(err error, slashPaths ...string) Error {
	if err == nil {
		return nil
	}
	paths := make([]id.Path, len(slashPaths))
	for i, p := range slashPaths {
		paths[i] = path{slashPath: p}
	}
	return pathError.Wrap(err).BuildWithPaths(paths...)
}
