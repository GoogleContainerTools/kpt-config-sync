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
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AsUnstructured attempts to convert a client.Object to an
// *unstructured.Unstructured.
// TODO: This adds .status and .metadata.creationTimestamp to
//
//	everything. Evaluate every use, and convert to using AsUnstructuredSanitized
//	if possible.
func AsUnstructured(o client.Object) (*unstructured.Unstructured, status.Error) {
	if u, isUnstructured := o.(*unstructured.Unstructured); isUnstructured {
		// The path below returns a deep copy, so we want to make sure we return a
		// deep copy here as well (for symmetry and to avoid subtle bugs).
		return u.DeepCopy(), nil
	}

	jsn, err := json.Marshal(o)
	if err != nil {
		return nil, status.InternalErrorBuilder.Wrap(err).BuildWithResources(o)
	}

	u := &unstructured.Unstructured{}
	err = u.UnmarshalJSON(jsn)
	if err != nil {
		return nil, status.InternalErrorBuilder.Wrap(err).BuildWithResources(o)
	}
	return u, nil
}

// AsUnstructuredSanitized converts o to an Unstructured and removes problematic
// fields:
// - metadata.creationTimestamp
// - status
//
// There is no other way to do this without defining our own versions of the
// Kubernetes type definitions.
// Explanation of why: https://www.sohamkamani.com/golang/2018-07-19-golang-omitempty/
func AsUnstructuredSanitized(o client.Object) (*unstructured.Unstructured, status.Error) {
	u, err := AsUnstructured(o)
	if err != nil {
		return nil, err
	}

	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "status")
	return u, nil
}
