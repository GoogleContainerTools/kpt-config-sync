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

package fake

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/syncer/decode"
)

var _ decode.Decoder = &decoder{}

// decoder is a decoder used for testing.
type decoder struct {
	data map[schema.GroupVersionKind][]*unstructured.Unstructured
}

// NewDecoder returns a new decoder.
func NewDecoder(us []*unstructured.Unstructured) decode.Decoder {
	m := make(map[schema.GroupVersionKind][]*unstructured.Unstructured)
	for _, u := range us {
		gvk := u.GroupVersionKind()
		m[gvk] = append(m[gvk], u)
	}

	return &decoder{data: m}
}

// UpdateScheme does nothing.
func (d *decoder) UpdateScheme(_ map[schema.GroupVersionKind]bool) {
}

// DecodeResources returns fake data.
func (d *decoder) DecodeResources(_ []v1.GenericResources) (map[schema.GroupVersionKind][]*unstructured.Unstructured, error) {
	return d.data, nil
}
