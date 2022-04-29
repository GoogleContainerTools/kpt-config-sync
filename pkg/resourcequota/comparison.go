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

package resourcequota

import (
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/resource"
)

// ResourceQuantityEqual allows the resource.Quantity to be checked for equality and allow the fields of
// ast.FileObject to be printed nicely when passed to cmp.Diff.
func ResourceQuantityEqual() cmp.Option {
	return cmp.Comparer(func(x, y resource.Quantity) bool {
		return x.Cmp(y) == 0
	})
}
