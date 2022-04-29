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

type finalValidator func(objs []ast.FileObject) status.MultiError

// Validation performs final validation checks against the given FileObjects.
// This should be called after all hydration steps are complete so that it can
// validate the final state of the repo.
func Validation(objs []ast.FileObject) status.MultiError {
	var errs status.MultiError
	// See the note about ordering in raw.Hierarchical().
	validators := []finalValidator{
		validate.DuplicateNames,
		validate.UnmanagedNamespaces,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	return errs
}
