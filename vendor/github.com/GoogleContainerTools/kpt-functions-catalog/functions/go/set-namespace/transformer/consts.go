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
)

const (
	fnConfigAPIVersion  = "fn.kpt.dev/v1alpha1"
	fnConfigKind        = "SetNamespace"
	dependsOnAnnotation = "config.kubernetes.io/depends-on"
	groupIdx            = 0
	namespaceIdx        = 2
	kindIdx             = 3
	nameIdx             = 4
)

var (
	// <group>/namespaces/<namespace>/<kind>/<name>
	namespacedResourcePattern = regexp.MustCompile(`\A([-.\w]*)/namespaces/([-.\w]*)/([-.\w]*)/([-.\w]*)\z`)
	dependsOnKeyPattern       = func(group, kind, name string) string {
		return fmt.Sprintf("%s/%s/%s", group, kind, name)
	}
)
