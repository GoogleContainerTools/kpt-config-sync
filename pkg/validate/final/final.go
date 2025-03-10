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

package final

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/final/validate"
)

// Options used to configure the Validate function
type Options struct {
	// MaxObjectCount is the maximum number of objects allowed in a single
	// inventory. Validation is skipped when less than 1.
	MaxObjectCount int
}

type validator func(objs []ast.FileObject) status.MultiError

// Validate performs final validation checks against the given FileObjects.
// This should be called after all hydration steps are complete so that it can
// validate the final state of the repo.
func Validate(objs []ast.FileObject, opts Options) status.MultiError {
	var errs status.MultiError
	// See the note about ordering in raw.Hierarchical().
	validators := []validator{
		validate.DuplicateNames,
		validate.UnmanagedNamespaces,
		validate.MaxObjectCount(opts.MaxObjectCount),
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	return errs
}
