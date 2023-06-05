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

package kinds

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

// ToUnstructured converts a typed object into an Unstructured object.
// If already Unstructured, a deep copy is returned.
func ToUnstructured(obj runtime.Object, scheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	gvk, err := Lookup(obj, scheme)
	if err != nil {
		return nil, err
	}

	if uObj, isUnstructured := obj.(*unstructured.Unstructured); isUnstructured {
		// Already typed
		return uObj.DeepCopy(), nil
	}

	// Convert to Unstructured
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(gvk)
	klog.V(6).Infof("Converting from %T to %T", obj, uObj)
	err = scheme.Convert(obj, uObj, nil)
	if err != nil {
		return nil, err
	}
	// Conversion sometimes drops the GVK, so add it back in.
	uObj.SetGroupVersionKind(gvk)
	return uObj, nil
}

// ToTypedObject converts an Unstructured object into a typed object.
// If not Unstructured, a deep copy is returned.
func ToTypedObject(obj runtime.Object, scheme *runtime.Scheme) (runtime.Object, error) {
	gvk, err := Lookup(obj, scheme)
	if err != nil {
		return nil, err
	}

	tObj, err := NewObjectForGVK(gvk, scheme)
	if err != nil {
		return nil, err
	}

	klog.V(6).Infof("Converting from %T to %T", obj, tObj)
	err = scheme.Convert(obj, tObj, nil)
	if err != nil {
		return nil, err
	}
	// Conversion sometimes drops the GVK, so add it back in.
	tObj.GetObjectKind().SetGroupVersionKind(gvk)

	return tObj, nil
}

// ToTypedWithVersion converts the specified object to the specified apiVersion.
// If version and type already match, a deep copy is returned.
//
// When the internal version is registered, it's assumed that the internal
// version is the hub version through which other versions are converted.
// Otherwise, either the source or target version must be the hub version and
// direct conversion must be registered with the scheme.
func ToTypedWithVersion(obj runtime.Object, targetGVK schema.GroupVersionKind, scheme *runtime.Scheme) (runtime.Object, error) {
	gvk, err := Lookup(obj, scheme)
	if err != nil {
		return nil, err
	}

	if gvk == targetGVK {
		return ToTypedObject(obj, scheme)
	}

	internalGVK := targetGVK.GroupKind().WithVersion(runtime.APIVersionInternal)
	if gvk != internalGVK && targetGVK != internalGVK && scheme.Recognizes(internalGVK) {
		// Neither input nor output is internal, but internal is registered.
		// So assume internal is the hub version, and convert to that first.
		klog.V(6).Infof("Converting from %s to %s", ObjectSummary(obj), GVKToString(internalGVK))
		obj, err = scheme.ConvertToVersion(obj, internalGVK.GroupVersion())
		if err != nil {
			return nil, err
		}
	}

	klog.V(6).Infof("Converting from %s to %s", ObjectSummary(obj), GVKToString(targetGVK))
	versionedObj, err := scheme.ConvertToVersion(obj, targetGVK.GroupVersion())
	if err != nil {
		return nil, err
	}
	return versionedObj, nil
}

// ToUnstructuredWithVersion converts the specified object to the specified
// apiVersion.
// If version already matches, a deep copy is returned.
//
// Since scheme.Convert ignores the target version when converting unstructured
// objects, we have to do three converts:
// unstructured -> typed (internal version) -> typed (target version) -> unstructured.
//
// WARNING: Delegates to ConvertToVersion. See function comment for warning.
func ToUnstructuredWithVersion(obj runtime.Object, targetGVK schema.GroupVersionKind, scheme *runtime.Scheme) (*unstructured.Unstructured, error) {
	gvk, err := Lookup(obj, scheme)
	if err != nil {
		return nil, err
	}

	if uObj, isUnstructured := obj.(*unstructured.Unstructured); isUnstructured && gvk == targetGVK {
		// Already unstructured with the right version
		return uObj.DeepCopy(), nil
	}

	// Convert to typed
	tObj, err := ToTypedWithVersion(obj, targetGVK, scheme)
	if err != nil {
		return nil, err
	}

	return ToUnstructured(tObj, scheme)
}

// ToUnstructureds converts the objects to a list of Unstructured objects
func ToUnstructureds(scheme *runtime.Scheme, objs ...runtime.Object) ([]*unstructured.Unstructured, error) {
	result := make([]*unstructured.Unstructured, len(objs))
	var errs status.MultiError
	for i, obj := range objs {
		obj, err := ToUnstructured(obj, scheme)
		if err != nil {
			errs = status.Append(errs, err)
		}
		result[i] = obj
	}
	return result, errs
}
