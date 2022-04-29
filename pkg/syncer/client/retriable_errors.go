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

package client

import (
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResourceConflictCode is the code for API Server errors resulting from a
// mismatch between our cached set of objects and the cluster's..
const ResourceConflictCode = "2008"

var retriableConflictBuilder = status.NewErrorBuilder(ResourceConflictCode)

// ConflictCreateAlreadyExists means we tried to create an object which already
// exists.
func ConflictCreateAlreadyExists(err error, resource client.Object) status.Error {
	return retriableConflictBuilder.
		Wrap(err).
		Sprint("tried to create resource that already exists").
		BuildWithResources(resource)
}

// ConflictUpdateDoesNotExist means we tried to update an object which does not
// exist.
func ConflictUpdateDoesNotExist(err error, resource client.Object) status.Error {
	return retriableConflictBuilder.
		Wrap(err).
		Sprint("tried to update resource which does not exist").
		BuildWithResources(resource)
}

// ConflictUpdateOldVersion means we tried to update an object using an old
// version of the object.
func ConflictUpdateOldVersion(err error, resource client.Object) status.Error {
	return retriableConflictBuilder.
		Wrap(err).
		Sprintf("tried to update with stale version of resource").
		BuildWithResources(resource)
}
