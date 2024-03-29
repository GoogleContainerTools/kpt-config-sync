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

package syntax

import (
	"kpt.dev/configsync/pkg/importer/id"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalFieldsInConfigErrorCode is the error code for IllegalFieldsInConfigError
const IllegalFieldsInConfigErrorCode = "1045"

var illegalFieldsInConfigErrorBuilder = status.NewErrorBuilder(IllegalFieldsInConfigErrorCode)

// IllegalFieldsInConfigError reports that an object has an illegal field set.
func IllegalFieldsInConfigError(resource client.Object, field id.DisallowedField) status.Error {
	return illegalFieldsInConfigErrorBuilder.
		Sprintf("Configs with %[1]q specified are not allowed. "+
			"To fix, either remove the config or remove the %[1]q field in the config:",
			field).
		BuildWithResources(resource)
}
