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

package tree

import (
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"kpt.dev/configsync/pkg/validate/tree/hydrate"
	"kpt.dev/configsync/pkg/validate/tree/validate"
)

// Hierarchical performs validation and hydration for a structured hierarchical
// repo against the given Tree objects. Note that this will modify the Tree
// objects in-place.
func Hierarchical(objs *objects.Tree) status.MultiError {
	var errs status.MultiError
	// See the note about ordering in raw.Hierarchical().
	validators := []objects.TreeVisitor{
		validate.HierarchyConfig,
		validate.Inheritance,
		validate.NamespaceSelector,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	if errs != nil {
		return errs
	}

	// We perform inheritance first so that we copy all abstract objects into
	// their potential namespaces, and then we perform namespace selection to
	// filter out the copies which are not selected.
	hydrators := []objects.TreeVisitor{
		hydrate.Inheritance,
		hydrate.NamespaceSelectors,
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}
