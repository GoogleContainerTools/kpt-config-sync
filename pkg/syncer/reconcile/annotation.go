/*
Copyright 2014 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file is mostly a direct copy of vendor/k8s.io/kubectl/pkg/util/apply.go
// The only difference is that it uses our own unique annotation instead of the
// standard kubectl one. It also contains two new functions which enable
// migration from the old annotation to the new one:
// updateConfigAnnotation and setOldAnnotation.

var metadataAccessor = meta.NewAccessor()

// updateConfigAnnotation ensures that the given resource is using the new
// declared-config annotation. If necessary, it initializes this annotation
// based upon the value of the old last-applied-config annotation.
func updateConfigAnnotation(resource *unstructured.Unstructured) error {
	annots, err := metadataAccessor.Annotations(resource)
	if err != nil {
		return err
	}

	if annots == nil {
		return nil
	}

	// First check for the new annotation.
	_, ok := annots[metadata.DeclaredConfigAnnotationKey]
	if ok {
		return nil
	}

	// Verify that this resource is actually managed by Config Sync.
	if _, ok = annots[metadata.ResourceManagementKey]; !ok {
		return nil
	}

	// Lastly, check to see if the old annotation is present. If so, we need to
	// copy its content into the new annotation.
	if old, ok := annots[corev1.LastAppliedConfigAnnotation]; ok {
		// Confusingly, we need to embed the old annotation itself into its own
		// content for the new annotation. This is basically the reverse of what
		// getModifiedConfiguration does where it explicitly avoids embedding the
		// annotation within itself.
		obj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, []byte(old))
		if err != nil {
			return err
		}
		if err = setOldAnnotation(obj, []byte(old)); err != nil {
			return err
		}

		// Now that the old annotation is embedded into its content, set all of that
		// as the content of the new annotation. The net result is that the patch
		// we calculate will prune the old annotation from the resource.
		content, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
		if err != nil {
			return err
		}
		if err = setOriginalConfiguration(resource, content); err != nil {
			return err
		}
	}

	return nil
}

// setOldAnnotation sets the given content as the value of the old annotation.
func setOldAnnotation(obj runtime.Object, content []byte) error {
	if len(content) < 1 {
		return nil
	}

	annots, err := metadataAccessor.Annotations(obj)
	if err != nil {
		return err
	}

	if annots == nil {
		annots = map[string]string{}
	}

	annots[corev1.LastAppliedConfigAnnotation] = string(content)
	return metadataAccessor.SetAnnotations(obj, annots)
}

// createApplyAnnotation gets the modified configuration of the object,
// without embedding it again, and then sets it on the object as the annotation.
func createApplyAnnotation(obj client.Object, codec runtime.Encoder) error {
	modified, err := getModifiedConfiguration(obj, false, codec)
	if err != nil {
		return err
	}
	return setOriginalConfiguration(obj, modified)
}

// setOriginalConfiguration sets the original configuration of the object
// as the annotation on the object for later use in computing a three way patch.
func setOriginalConfiguration(obj client.Object, original []byte) error {
	if len(original) < 1 {
		return nil
	}

	annots, err := metadataAccessor.Annotations(obj)
	if err != nil {
		return err
	}

	if annots == nil {
		annots = map[string]string{}
	}

	annots[metadata.DeclaredConfigAnnotationKey] = string(original)
	return metadataAccessor.SetAnnotations(obj, annots)
}

// getOriginalConfiguration retrieves the original configuration of the object
// from the annotation, or nil if no annotation was found.
func getOriginalConfiguration(obj client.Object) ([]byte, error) {
	annots, err := metadataAccessor.Annotations(obj)
	if err != nil {
		return nil, err
	}

	if annots == nil {
		return nil, nil
	}

	original, ok := annots[metadata.DeclaredConfigAnnotationKey]
	if !ok {
		return nil, nil
	}

	return []byte(original), nil
}

// getModifiedConfiguration retrieves the modified configuration of the object.
// If annotate is true, it embeds the result as an annotation in the modified
// configuration. If an object was read from the command input, it will use that
// version of the object. Otherwise, it will use the version from the server.
func getModifiedConfiguration(obj client.Object, annotate bool, codec runtime.Encoder) ([]byte, error) {
	// First serialize the object without the annotation to prevent recursion,
	// then add that serialization to it as the annotation and serialize it again.
	var modified []byte

	// Otherwise, use the server side version of the object.
	// Get the current annotations from the object.
	annots, err := metadataAccessor.Annotations(obj)
	if err != nil {
		return nil, err
	}

	if annots == nil {
		annots = map[string]string{}
	}

	original := annots[metadata.DeclaredConfigAnnotationKey]
	delete(annots, metadata.DeclaredConfigAnnotationKey)
	if err := metadataAccessor.SetAnnotations(obj, annots); err != nil {
		return nil, err
	}

	modified, err = runtime.Encode(codec, obj)
	if err != nil {
		return nil, err
	}

	if annotate {
		annots[metadata.DeclaredConfigAnnotationKey] = string(modified)
		if err := metadataAccessor.SetAnnotations(obj, annots); err != nil {
			return nil, err
		}

		modified, err = runtime.Encode(codec, obj)
		if err != nil {
			return nil, err
		}
	}

	// Restore the object to its original condition.
	annots[metadata.DeclaredConfigAnnotationKey] = original
	if err := metadataAccessor.SetAnnotations(obj, annots); err != nil {
		return nil, err
	}

	return modified, nil
}
