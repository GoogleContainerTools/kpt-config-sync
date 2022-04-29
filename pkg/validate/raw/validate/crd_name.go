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
	"fmt"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/clusterconfig"
)

// CRDName returns an error if the CRD's name does not match the Kubernetes
// specification:
// https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/
func CRDName(obj ast.FileObject) status.Error {
	if obj.GetObjectKind().GroupVersionKind().GroupKind() != kinds.CustomResourceDefinition() {
		return nil
	}

	crd, err := clusterconfig.AsCRD(obj.Unstructured)
	if err != nil {
		return err
	}
	expectedName := fmt.Sprintf("%s.%s", crd.Spec.Names.Plural, crd.Spec.Group)
	if crd.Name != expectedName {
		return nonhierarchical.InvalidCRDNameError(obj, expectedName)
	}

	return nil
}
