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
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalKindInNamespacesErrorCode is the error code for IllegalKindInNamespacesError
const IllegalKindInNamespacesErrorCode = "1038"

var illegalKindInNamespacesError = status.NewErrorBuilder(IllegalKindInNamespacesErrorCode)

// IllegalKindInNamespacesError reports that an object has been illegally defined in namespaces/
func IllegalKindInNamespacesError(resources ...client.Object) status.Error {
	return illegalKindInNamespacesError.
		Sprintf("Configs of the below Kind MUST not be declared in `%s`/:", repo.NamespacesDir).
		BuildWithResources(resources...)
}
