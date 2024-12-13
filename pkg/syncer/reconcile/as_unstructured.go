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

package reconcile

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AsUnstructured attempts to convert a client.Object to an
// *unstructured.Unstructured using the global core.Scheme.
func AsUnstructured(o client.Object) (*unstructured.Unstructured, status.Error) {
	uObj, err := kinds.ToUnstructured(o, core.Scheme)
	if err != nil {
		return nil, status.InternalErrorBuilder.Wrap(err).BuildWithResources(o)
	}
	return uObj, nil
}

// AsUnstructuredSanitized converts o to an Unstructured and removes problematic
// fields:
// - metadata.creationTimestamp
// - status
// - metadata.managedFields
//
// These fields must not be set in the source, so we can safely drop them from
// the current live manifest, because we won't ever need to be reverted.
//
// This allows the returned object to be used with Server-Side Apply without
// accidentally attempting to modify or take ownership of these fields.
//
// This is required because the existing typed objects don't use pointers and
// thus can't be set to nil, and the Go JSON formatter ignores omitempty on
// non-pointer structs. So even when empty, the fields are still set during
// serialization, which would cause SSA to try to delete the existing value.
// For more details, see https://www.sohamkamani.com/golang/2018-07-19-golang-omitempty/
func AsUnstructuredSanitized(o client.Object) (*unstructured.Unstructured, status.Error) {
	u, err := AsUnstructured(o)
	if err != nil {
		return nil, err
	}

	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "status")

	// This field is populated when the object is fetched from the cluster, so it
	// needs to be removed before the object is updated and sent to the Applier.
	// SSA does not accept objects with this field.
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	return u, nil
}
