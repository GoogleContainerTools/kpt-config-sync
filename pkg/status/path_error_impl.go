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
	"sort"
	"strings"

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/importer/id"
)

type pathErrorImpl struct {
	underlying Error
	paths      []id.Path
}

var _ PathError = pathErrorImpl{}

// Error implements error.
func (p pathErrorImpl) Error() string {
	return format(p)
}

// Is implements Error.
func (p pathErrorImpl) Is(target error) bool {
	return p.underlying.Is(target)
}

// Code implements Error.
func (p pathErrorImpl) Code() string {
	return p.underlying.Code()
}

// Body implements Error.
func (p pathErrorImpl) Body() string {
	return formatBody(p.underlying.Body(), "\n\n", formatPaths(p.paths))
}

// Errors implements MultiError.
func (p pathErrorImpl) Errors() []Error {
	return []Error{p}
}

// RelativePaths implements PathError.
func (p pathErrorImpl) RelativePaths() []id.Path {
	return p.paths
}

// Cause implements causer.
func (p pathErrorImpl) Cause() error {
	return p.underlying.Cause()
}

// ToCME implements Error.
func (p pathErrorImpl) ToCME() v1.ConfigManagementError {
	return fromPathError(p)
}

// ToCSE implements Error.
func (p pathErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	return cseFromPathError(p)
}

func formatPaths(paths []id.Path) string {
	pathStrs := make([]string, len(paths))
	for i, path := range paths {
		pathStrs[i] = "path: " + path.OSPath()
		if filepath.Ext(path.OSPath()) == "" {
			// Assume paths without extensions are directories. We don't support files without extensions,
			// so for now this is a safe assumption.
			pathStrs[i] += "/"
		}
	}
	// Ensure deterministic path printing order.
	sort.Strings(pathStrs)
	return strings.Join(pathStrs, "\n")
}
