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

// Package hnc adds additional HNC-understandable annotation and labels to namespaces managed by
// ACM.
package hnc

import (
	"fmt"
	"sort"
	"strings"

	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalDepthLabelErrorCode is the error code for IllegalDepthLabelError.
const IllegalDepthLabelErrorCode = "1057"

var illegalDepthLabelError = status.NewErrorBuilder(IllegalDepthLabelErrorCode)

// IllegalDepthLabelError represent a set of illegal label definitions.
func IllegalDepthLabelError(resource client.Object, labels []string) status.Error {
	sort.Strings(labels) // ensure deterministic label order
	labels2 := make([]string, len(labels))
	for i, label := range labels {
		labels2[i] = fmt.Sprintf("%q", label)
	}
	l := strings.Join(labels2, ", ")
	return illegalDepthLabelError.
		Sprintf("Configs MUST NOT declare labels ending with %q. "+
			"The config has disallowed labels: %s",
			metadata.DepthSuffix, l).
		BuildWithResources(resource)
}
