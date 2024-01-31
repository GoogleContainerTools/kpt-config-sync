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

package testutils

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AppendFinalizer adds a finalizer to the object
func AppendFinalizer(obj client.Object, finalizer string) {
	finalizers := obj.GetFinalizers()
	finalizers = append(finalizers, finalizer)
	obj.SetFinalizers(finalizers)
}

// RemoveFinalizer removes a finalizer from the object.
// Returns whether the finalizer was removed.
func RemoveFinalizer(obj client.Object, removeFinalizer string) bool {
	finalizers := obj.GetFinalizers()
	var newFinalizers []string
	found := false
	for _, finalizer := range finalizers {
		if finalizer == removeFinalizer {
			found = true
		} else {
			newFinalizers = append(newFinalizers, finalizer)
		}
	}
	obj.SetFinalizers(newFinalizers)
	return found
}
