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

package finalizer

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	syncfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SingleObjectPredicate returns a Predicate which filters events by both
// name & namespace. The Predicate methods return true if the object matches.
//
// Use this for client-side filtering when you can't use server-side
// fieldSelectors, like when an informer is shared between controllers.
func SingleObjectPredicate(key client.ObjectKey) predicate.Predicate {
	fieldSelector := fields.AndSelectors(
		fields.OneTermEqualSelector("metadata.name", key.Name),
		fields.OneTermEqualSelector("metadata.namespace", key.Namespace),
	)

	return predicate.NewPredicateFuncs(func(obj client.Object) bool {
		uObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			panic(fmt.Sprintf("Predicate received unexpected object type %T", obj))
		}
		return fieldSelector.Matches(&syncfake.UnstructuredFields{
			Object: uObj,
		})
	})
}
