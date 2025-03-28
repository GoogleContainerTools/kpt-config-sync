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

package core

// Annotated is the interface defined by types with annotations. Note that
// some non-objects (such as PodTemplates) define annotations but are not objects.
type Annotated interface {
	GetAnnotations() map[string]string
	SetAnnotations(map[string]string)
}

// SetAnnotation sets the annotation on the passed annotated object to value.
// Returns true if the object was modified.
func SetAnnotation(obj Annotated, annotation, value string) bool {
	aMap := obj.GetAnnotations()
	if aMap != nil {
		if existing, found := aMap[annotation]; found && existing == value {
			return false
		}
	} else {
		aMap = make(map[string]string)
	}
	aMap[annotation] = value
	obj.SetAnnotations(aMap)
	return true
}

// GetAnnotation gets the annotation value on the passed annotated object for a given key.
func GetAnnotation(obj Annotated, annotation string) string {
	aMap := obj.GetAnnotations()
	if aMap == nil {
		return ""
	}
	value, found := aMap[annotation]
	if found {
		return value
	}
	return ""
}

// GetLabel gets the label value on the passed object for a given key.
func GetLabel(obj Labeled, label string) string {
	lMap := obj.GetLabels()
	if lMap == nil {
		return ""
	}
	value, found := lMap[label]
	if found {
		return value
	}
	return ""
}

// RemoveAnnotations removes the passed set of annotations from obj.
// Returns true if the object was modified.
func RemoveAnnotations(obj Annotated, annotations ...string) bool {
	aMap := obj.GetAnnotations()
	if len(aMap) == 0 {
		return false
	}
	updated := false
	for _, a := range annotations {
		if _, found := aMap[a]; found {
			delete(aMap, a)
			updated = true
		}
	}
	if updated {
		obj.SetAnnotations(aMap)
	}
	return updated
}

// RemoveLabels removes the passed set of labels from obj.
// Returns true if the object was modified.
func RemoveLabels(obj Labeled, labels ...string) bool {
	lMap := obj.GetLabels()
	if len(lMap) == 0 {
		return false
	}
	updated := false
	for _, label := range labels {
		if _, found := lMap[label]; found {
			delete(lMap, label)
			updated = true
		}
	}
	if updated {
		obj.SetLabels(lMap)
	}
	return updated
}

// AddAnnotations adds the specified annotations to the object.
// Returns true if the object was modified.
func AddAnnotations(obj Annotated, annotations map[string]string) bool {
	aMap := obj.GetAnnotations()
	if aMap == nil {
		aMap = make(map[string]string, len(annotations))
	}
	updated := false
	for key, value := range annotations {
		if existing, found := aMap[key]; !found || existing != value {
			aMap[key] = value
			updated = true
		}
	}
	if updated {
		obj.SetAnnotations(aMap)
	}
	return updated
}

// Labeled is the interface defined by types with labeled. Note that
// some non-objects (such as PodTemplates) define labels but are not objects.
type Labeled interface {
	GetLabels() map[string]string
	SetLabels(map[string]string)
}

// SetLabel sets label on obj to value.
// Returns true if the object was modified.
func SetLabel(obj Labeled, label, value string) bool {
	lMap := obj.GetLabels()
	if lMap != nil {
		if existing, found := lMap[label]; found && existing == value {
			return false
		}
	} else {
		lMap = make(map[string]string)
	}
	lMap[label] = value
	obj.SetLabels(lMap)
	return true
}

// AddLabels adds the specified labels to the object.
// Returns true if the object was modified.
func AddLabels(obj Labeled, labels map[string]string) bool {
	lMap := obj.GetLabels()
	if lMap == nil {
		lMap = make(map[string]string, len(labels))
	}
	updated := false
	for key, value := range labels {
		if existing, found := lMap[key]; !found || existing != value {
			lMap[key] = value
			updated = true
		}
	}
	if updated {
		obj.SetLabels(lMap)
	}
	return updated
}

// copyMap returns a copy of the passed map. Otherwise the Labels or Annotations maps will have two
// owners.
func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	for k, v := range m {
		result[k] = v
	}
	return result
}
