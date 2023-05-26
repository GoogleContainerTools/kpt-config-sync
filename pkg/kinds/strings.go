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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GVKToString returns a human readable format for GroupVersionKind:
// `GROUP/VERSION.KIND`
func GVKToString(gvk schema.GroupVersionKind) string {
	return fmt.Sprintf("%s.%s", gvk.GroupVersion(), gvk.Kind)
}

// ObjectSummary returns a human readable format for objects.
// Depending on the type and what's populated, the output may be one of the
// following:
// - `GROUP/VERSION.KIND(TYPE)[NAMESPACE/NAME]`
// - `GROUP/VERSION.KIND(TYPE)`
// - `(TYPE)`
func ObjectSummary(obj runtime.Object) string {
	gvk := obj.GetObjectKind().GroupVersionKind()
	gvkStr := ""
	if !gvk.Empty() {
		gvkStr = GVKToString(gvk)
	}
	if cObj, ok := obj.(client.Object); ok {
		keyStr := client.ObjectKeyFromObject(cObj).String()
		if keyStr != string(types.Separator) {
			return fmt.Sprintf("%s(%T)[%s]", gvkStr, obj, keyStr)
		}
	}
	return fmt.Sprintf("%s(%T)", gvkStr, obj)
}
