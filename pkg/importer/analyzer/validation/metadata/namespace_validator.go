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

package metadata

import (
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalMetadataNamespaceDeclarationErrorCode is the error code for IllegalNamespaceDeclarationError
const IllegalMetadataNamespaceDeclarationErrorCode = "1009"

var illegalMetadataNamespaceDeclarationError = status.NewErrorBuilder(IllegalMetadataNamespaceDeclarationErrorCode)

// IllegalMetadataNamespaceDeclarationError represents illegally declaring metadata.namespace
func IllegalMetadataNamespaceDeclarationError(resource client.Object, expectedNamespace string) status.Error {
	return illegalMetadataNamespaceDeclarationError.
		Sprintf("A config MUST either declare a `namespace` field exactly matching the directory "+
			"containing the config, %q, or leave the field blank:", expectedNamespace).
		BuildWithResources(resource)
}
