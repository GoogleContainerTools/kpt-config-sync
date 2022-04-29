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

package validate

import (
	"kpt.dev/configsync/pkg/importer/analyzer/validation/syntax"
	"kpt.dev/configsync/pkg/importer/id"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// DisallowedFields verifies if the given Raw objects contain any fields which
// are not allowed to be declared in Git.
func DisallowedFields(objs *objects.Raw) status.MultiError {
	var errs status.MultiError
	for _, obj := range objs.Objects {
		if len(obj.GetOwnerReferences()) > 0 {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.OwnerReference))
		}
		if obj.GetSelfLink() != "" {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.SelfLink))
		}
		if obj.GetUID() != "" {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.UID))
		}
		if obj.GetResourceVersion() != "" {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.ResourceVersion))
		}
		if obj.GetGeneration() != 0 {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.Generation))
		}
		if !obj.GetCreationTimestamp().Time.IsZero() {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.CreationTimestamp))
		}
		if obj.GetDeletionTimestamp() != nil {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.DeletionTimestamp))
		}
		if obj.GetDeletionGracePeriodSeconds() != nil {
			errs = status.Append(errs, syntax.IllegalFieldsInConfigError(obj, id.DeletionGracePeriodSeconds))
		}
	}
	return errs
}
