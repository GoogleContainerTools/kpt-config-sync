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

package typeresolver

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func FakeResolver() *TypeResolver {
	return &TypeResolver{
		typeMapping: map[schema.GroupKind]schema.GroupVersionKind{
			{
				Group: "apps",
				Kind:  "Deployment",
			}: {
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
			{
				Group: "apps",
				Kind:  "StatefulSet",
			}: {
				Group:   "apps",
				Version: "v1",
				Kind:    "StatefulSet",
			},
			{
				Group: "apps",
				Kind:  "DaemonSet",
			}: {
				Group:   "apps",
				Version: "v1",
				Kind:    "DaemonSet",
			},
			{
				Group: "",
				Kind:  "ConfigMap",
			}: {
				Group:   "",
				Version: "v1",
				Kind:    "ConfigMap",
			},
		},
	}
}
