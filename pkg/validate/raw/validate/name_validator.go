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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
)

// Name verifies that the given FileObject has a valid name according to the
// following rules:
// - the object can not have an empty name
// - if the object is related to RBAC, its name must be a valid path segment
// - otherwise the object's name must be a valid DNS1123 subdomain
func Name(obj ast.FileObject) status.Error {
	if obj.GetName() == "" {
		return nonhierarchical.MissingObjectNameError(obj)
	}

	var errs []string
	if obj.GetObjectKind().GroupVersionKind().Group == rbacv1.SchemeGroupVersion.Group {
		// The APIServer has different metadata.name requirements for RBAC types.
		errs = rest.IsValidPathSegmentName(obj.GetName())
	} else {
		errs = validation.IsDNS1123Subdomain(obj.GetName())
	}
	if errs != nil {
		return nonhierarchical.InvalidMetadataNameError(obj)
	}

	return nil
}
