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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// UnstructuredFields impliments fields.Fields to do field selection on any
// field in an unstructured object.
type UnstructuredFields struct {
	Object *unstructured.Unstructured
}

// Has returns whether the provided field exists.
func (uf *UnstructuredFields) Has(field string) (exists bool) {
	_, found, err := unstructured.NestedString(uf.Object.Object, uf.fields(field)...)
	return err == nil && found
}

// Get returns the value for the provided field.
func (uf *UnstructuredFields) Get(field string) (value string) {
	val, found, err := unstructured.NestedString(uf.Object.Object, uf.fields(field)...)
	if err != nil || !found {
		return ""
	}
	return val
}

func (uf *UnstructuredFields) fields(field string) []string {
	field = strings.TrimPrefix(field, ".")
	return strings.Split(field, ".")
}
