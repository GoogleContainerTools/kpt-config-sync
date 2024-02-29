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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectAsClientObject casts from runtime.Object to client.Object.
// This method ensures a consistent error message for this common operation.
func ObjectAsClientObject(rObj runtime.Object) (client.Object, error) {
	cObj, ok := rObj.(client.Object)
	if !ok {
		return nil, fmt.Errorf("unsupported resource type (%s): failed to cast to client.Object",
			ObjectSummary(rObj))
	}
	return cObj, nil
}

// ObjectAsClientObjectList casts from runtime.Object to client.ObjectList.
// This method ensures a consistent error message for this common operation.
func ObjectAsClientObjectList(rObj runtime.Object) (client.ObjectList, error) {
	cObj, ok := rObj.(client.ObjectList)
	if !ok {
		return nil, fmt.Errorf("unsupported resource type (%s): failed to cast to client.ObjectList",
			ObjectSummary(rObj))
	}
	return cObj, nil
}
