// Copyright 2025 Google LLC
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
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// convertUnstructuredIntoObject wraps Scheme.Convert to add support for
// PartialObjectMetadata as an optional target type.
//
// Warning: This code converts through JSON and should only be used for testing.
func convertUnstructuredIntoObject(from *unstructured.Unstructured, to client.Object, scheme *runtime.Scheme) error {
	// Convert to the requested type and version (DeepCopyInto the input object).
	switch tObj := to.(type) {
	case *metav1.PartialObjectMetadata:
		return convertFromUnstructuredIntoV1MetadataObject(from, tObj, scheme)
	// TODO: add support for v1beta1.PartialObjectMetadata, as needed
	default:
		return scheme.Convert(from, to, nil)
	}
}

// convertUnstructuredListIntoObjectList wraps Scheme.Convert to add support for
// PartialObjectMetadataList as an optional target type.
//
// Warning: This code converts through JSON and should only be used for testing.
func convertUnstructuredListIntoObjectList(from *unstructured.UnstructuredList, to client.ObjectList, scheme *runtime.Scheme) error {
	// Convert to the requested type and version (DeepCopyInto the input object).
	switch tObj := to.(type) {
	case *metav1.PartialObjectMetadataList:
		return convertFromUnstructuredListIntoV1MetadataList(from, tObj, scheme)
	// TODO: add support for v1beta1.PartialObjectMetadataList, as needed
	default:
		return scheme.Convert(from, to, nil)
	}
}

// convertFromUnstructuredIntoV1MetadataObject converts from UnstructuredList
// to v1.PartialObjectMetadata.
//
// PartialObjectMetadata isn't registered with the Scheme and has no generated
// conversion code, so it has to be converted through JSON.
// This is effectively Convert + DeepCopyInto.
//
// Warning: This code converts through JSON and should only be used for testing.
func convertFromUnstructuredIntoV1MetadataObject(uObj *unstructured.Unstructured, mObj *metav1.PartialObjectMetadata, scheme *runtime.Scheme) error {
	// Validate matching GroupKinds
	if uObj.GroupVersionKind().GroupKind() != mObj.GroupVersionKind().GroupKind() {
		return fmt.Errorf("converting Unstructured (%s) to PartialObjectMetadata (%s): GroupKind mismatch",
			uObj.GroupVersionKind().GroupKind(), mObj.GroupVersionKind().GroupKind())
	}
	// Convert to desired version
	if uObj.GroupVersionKind().Version != mObj.GroupVersionKind().Version {
		vObj := &unstructured.Unstructured{}
		vObj.SetGroupVersionKind(mObj.GroupVersionKind())
		if err := scheme.Convert(uObj, vObj, nil); err != nil {
			return fmt.Errorf("converting Unstructured (%s) to Unstructured (%s): %w",
				uObj.GroupVersionKind(), vObj.GroupVersionKind(), err)
		}
		uObj = vObj
	}
	// Convert from Unstructured to PartialObjectMetadata the hard way
	jsonBytes, err := uObj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encoding Unstructured as json: %w", err)
	}
	if err := json.Unmarshal(jsonBytes, mObj); err != nil {
		return fmt.Errorf("decoding json as PartialObjectMetadata: %w", err)
	}
	return nil
}

// convertFromUnstructuredListIntoV1MetadataList converts from UnstructuredList
// to v1.PartialObjectMetadataList.
//
// PartialObjectMetadataList isn't registered with the Scheme and has no
// generated conversion code, so it has to be converted through JSON.
// This is effectively Convert + DeepCopyInto.
//
// Warning: This code converts through JSON and should only be used for testing.
func convertFromUnstructuredListIntoV1MetadataList(uList *unstructured.UnstructuredList, mList *metav1.PartialObjectMetadataList, scheme *runtime.Scheme) error {
	// Validate matching GroupKinds
	if uList.GroupVersionKind().GroupKind() != mList.GroupVersionKind().GroupKind() {
		return fmt.Errorf("converting UnstructuredList (%s) to PartialObjectMetadataList (%s): GroupKind mismatch",
			uList.GroupVersionKind().GroupKind(), mList.GroupVersionKind().GroupKind())
	}
	// Convert to desired version
	if uList.GroupVersionKind().Version != mList.GroupVersionKind().Version {
		vList := &unstructured.UnstructuredList{}
		vList.SetGroupVersionKind(mList.GroupVersionKind())
		if err := scheme.Convert(uList, vList, nil); err != nil {
			return fmt.Errorf("converting UnstructuredList (%s) to UnstructuredList (%s): %w",
				uList.GroupVersionKind(), vList.GroupVersionKind(), err)
		}
		uList = vList
	}
	// Convert from UnstructuredList to PartialObjectMetadataList the hard way
	jsonBytes, err := uList.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encoding UnstructuredList as json: %w", err)
	}
	if err := json.Unmarshal(jsonBytes, mList); err != nil {
		return fmt.Errorf("decoding json as PartialObjectMetadataList: %w", err)
	}
	return nil
}
