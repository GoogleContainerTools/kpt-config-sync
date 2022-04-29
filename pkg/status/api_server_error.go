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

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APIServerErrorCode is the error code for a status Error originating from the kubernetes API server.
const APIServerErrorCode = "2002"

// apiServerErrorBuilder represents an error returned by the APIServer.
// This isn't exported to force the callers to be subject to the additional processing (e.g. detecting insufficient permissions).
var apiServerErrorBuilder = NewErrorBuilder(APIServerErrorCode).Sprint("APIServer error")

// InsufficientPermissionErrorCode is the error code when the reconciler has insufficient permissions to manage resources.
const InsufficientPermissionErrorCode = "2013"

// InsufficientPermissionErrorBuilder represents an error related to insufficient permissions returned by the APIServer.
var InsufficientPermissionErrorBuilder = NewErrorBuilder(InsufficientPermissionErrorCode).
	Sprint("Insufficient permission. To fix, make sure the reconciler has sufficient permissions.")

// APIServerError wraps an error returned by the APIServer.
func APIServerError(err error, message string, resources ...client.Object) Error {
	var errorBuilder ErrorBuilder
	if apierrors.IsForbidden(err) {
		errorBuilder = InsufficientPermissionErrorBuilder.Sprint(message).Wrap(err)
	} else {
		errorBuilder = apiServerErrorBuilder.Sprint(message).Wrap(err)
	}
	if len(resources) == 0 {
		return errorBuilder.Build()
	}
	return errorBuilder.BuildWithResources(resources...)
}

// APIServerErrorf wraps an error returned by the APIServer with a formatted message.
func APIServerErrorf(err error, format string, a ...interface{}) Error {
	if apierrors.IsForbidden(err) {
		return InsufficientPermissionErrorBuilder.Sprintf(format, a...).Wrap(err).Build()
	}
	return apiServerErrorBuilder.Sprintf(format, a...).Wrap(err).Build()
}
