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
package transformer

import (
	"fmt"
	"regexp"

	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn"
)

const (
	// Constants for FunctionConfig `SetNamespace`
	fnConfigGroup   = "fn.kpt.dev"
	fnConfigVersion = "v1alpha1"
	fnConfigKind    = "SetNamespace"
	// The ConfigMap name generated from variant constructor
	builtinConfigMapName = "kptfile.kpt.dev"
	dependsOnAnnotation  = "config.kubernetes.io/depends-on"
	namespaceIdx         = 2
)

var (
	// <group>/namespaces/<namespace>/<kind>/<name>
	namespacedResourcePattern = regexp.MustCompile(`\A([-.\w]*)/namespaces/([-.\w]*)/([-.\w]*)/([-.\w]*)\z`)
	nsScopedDependsOnFromId   = func(id *fn.ResourceIdentifier) string {
		return fmt.Sprintf("%v/namespaces/%v/%v/%v", id.Group, id.Namespace, id.Kind, id.Name)
	}
)
