// Copyright 2023 Google LLC
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

package kinds

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewObjectForGVK creates a new runtime.Object using the type registered to
// the scheme for the specified GroupVersionKind.
// This is a wrapper around scheme.New to provide a consistent error message.
func NewObjectForGVK(gvk schema.GroupVersionKind, scheme *runtime.Scheme) (runtime.Object, error) {
	rObj, err := scheme.New(gvk)
	if err != nil {
		return nil, fmt.Errorf("unsupported resource type (%s): %w", GVKToString(gvk), err)
	}
	return rObj, nil
}

// NewClientObjectForGVK creates a new client.Object using the type registered
// to the scheme for the specified GroupVersionKind.
//
// In practice, most runtime.Object are client.Object.
// However, some objects are not, namely objects used for config files that are
// not persisted by the Kubernetes API server.
// This method makes this common operation easier and ensures a consistent
// error message.
func NewClientObjectForGVK(gvk schema.GroupVersionKind, scheme *runtime.Scheme) (client.Object, error) {
	rObj, err := NewObjectForGVK(gvk, scheme)
	if err != nil {
		return nil, err
	}
	return ObjectAsClientObject(rObj)
}
