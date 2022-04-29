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

package status

// EmptySourceErrorCode is the error code for an EmptySourceError.
const EmptySourceErrorCode = "2006"

// EmptySourceErrorBuilder is an ErrorBuilder for errors related to the repo's source of truth.
var EmptySourceErrorBuilder = NewErrorBuilder(EmptySourceErrorCode)

// EmptySourceError returns an EmptySourceError when the specified number of resources would have be deleted.
func EmptySourceError(current int, resourceType string) Error {
	return EmptySourceErrorBuilder.
		Sprintf("mounted git repo appears to contain no managed %s, which would delete %d existing %s from the cluster", resourceType, current, resourceType).
		Build()
}
