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

// Package lifecycle defines the client-side lifecycle directives ACM honors.
package lifecycle

import (
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HasPreventDeletion returns true if the object has the LifecycleDeleteAnnotation
// and it is set to "detach".
func HasPreventDeletion(o client.Object) bool {
	deletion, hasDeletion := o.GetAnnotations()[common.LifecycleDeleteAnnotation]
	return hasDeletion && (deletion == common.PreventDeletion)
}
