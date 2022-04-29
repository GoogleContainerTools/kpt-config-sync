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

import "sigs.k8s.io/controller-runtime/pkg/client"

// ResourceErrorCode is the error code for a generic ResourceError.
const ResourceErrorCode = "2010"

// ResourceError defines a status error related to one or more k8s resources.
type ResourceError interface {
	Error
	Resources() []client.Object
}

// ResourceErrorBuilder almost always results from an API server call involving one or more resources.
var ResourceErrorBuilder = NewErrorBuilder(ResourceErrorCode)

// ResourceWrap returns a ResourceError wrapping the given error and Resources.
func ResourceWrap(err error, msg string, resources ...client.Object) Error {
	if err == nil {
		return nil
	}
	return ResourceErrorBuilder.Sprint(msg).Wrap(err).BuildWithResources(resources...)
}
