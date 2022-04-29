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

package nonhierarchical

import (
	"fmt"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InvalidCRDNameErrorCode is the error code for InvalidCRDNameError.
const InvalidCRDNameErrorCode = "1048"

var invalidCRDNameErrorBuilder = status.NewErrorBuilder(InvalidCRDNameErrorCode)

// InvalidCRDNameError reports a CRD with an invalid name in the repo.
func InvalidCRDNameError(resource client.Object, expected string) status.Error {
	return invalidCRDNameErrorBuilder.
		Sprintf("The CustomResourceDefinition `metadata.name` MUST be in the form: `<spec.names.plural>.<spec.group>`. "+
			"To fix, update those fields or change `metadata.name` to %q.",
			expected).
		BuildWithResources(resource)
}

// ValidateCRDName returns an error
func ValidateCRDName(o ast.FileObject) status.Error {
	if o.GetObjectKind().GroupVersionKind().GroupKind() != kinds.CustomResourceDefinition() {
		return nil
	}

	crd, err := clusterconfig.AsCRD(o.Unstructured)
	if err != nil {
		return err
	}
	expectedName := fmt.Sprintf("%s.%s", crd.Spec.Names.Plural, crd.Spec.Group)
	if crd.Name != expectedName {
		return InvalidCRDNameError(&o, expectedName)
	}

	return nil
}
