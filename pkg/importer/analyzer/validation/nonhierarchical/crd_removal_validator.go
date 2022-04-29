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
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnsupportedCRDRemovalErrorCode is the error code for UnsupportedCRDRemovalError
const UnsupportedCRDRemovalErrorCode = "1047"

var unsupportedCRDRemovalError = status.NewErrorBuilder(UnsupportedCRDRemovalErrorCode)

// UnsupportedCRDRemovalError reports than a CRD was removed, but its corresponding CRs weren't.
func UnsupportedCRDRemovalError(resource client.Object) status.Error {
	return unsupportedCRDRemovalError.
		Sprintf("Custom Resources MUST be removed in the same commit as their corresponding " +
			"CustomResourceDefinition. To fix, remove this Custom Resource or re-add the CRD.").
		BuildWithResources(resource)
}
