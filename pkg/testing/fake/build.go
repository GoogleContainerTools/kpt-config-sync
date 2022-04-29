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
	"encoding/json"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// defaultMutations are the standard Meta set on all fake objects. All can be overwritten with mutators.
//
// Annotations and Labels required when constructing any Object or else gomock will complain the nil
// and empty map are different. There is no other way to deal with this as the underlying
// implementations outside of our control handle empty vs nil maps inconsistently. Explicitly
// setting labels and annotations to empty map circumvents the issue.
var defaultMutations = []core.MetaMutator{
	core.Name("default-name"),
	core.Annotations(map[string]string{}),
	core.Labels(map[string]string{}),
}

func defaultMutate(object client.Object) {
	for _, m := range defaultMutations {
		m(object)
	}
}

func mutate(object client.Object, opts ...core.MetaMutator) {
	for _, m := range opts {
		m(object)
	}
}

// FileObject is a shorthand for converting to an ast.FileObject.
// path is the slash-delimited path from the POLICY_DIR root.
func FileObject(object client.Object, path string) ast.FileObject {
	if fo, isFileObject := object.(ast.FileObject); isFileObject {
		return fo
	}

	jsn, err := json.Marshal(object)
	if err != nil {
		// Something has gone horribly wrong in our test code; this should never fail.
		panic(err)
	}

	u := &unstructured.Unstructured{}
	err = u.UnmarshalJSON(jsn)
	if err != nil {
		// Something has gone horribly wrong in our test code; this should never fail.
		panic(err)
	}

	normalizeUnstructured(u)
	return ast.NewFileObject(u, cmpath.RelativeSlash(path))
}

func normalizeUnstructured(u *unstructured.Unstructured) {
	if ct := u.GetCreationTimestamp(); ct.IsZero() {
		delete(u.Object["metadata"].(map[string]interface{}), "creationTimestamp")
	}
	if u.GetAnnotations() == nil {
		u.SetAnnotations(map[string]string{})
	}
	if u.GetLabels() == nil {
		u.SetLabels(map[string]string{})
	}
}

// UnstructuredObject initializes an unstructured.Unstructured.
func UnstructuredObject(gvk schema.GroupVersionKind, opts ...core.MetaMutator) *unstructured.Unstructured {
	o := &unstructured.Unstructured{}
	o.GetObjectKind().SetGroupVersionKind(gvk)

	defaultMutate(o)
	mutate(o, opts...)
	return o
}

// Unstructured initializes an Unstructured.
func Unstructured(gvk schema.GroupVersionKind, opts ...core.MetaMutator) ast.FileObject {
	return UnstructuredAtPath(gvk, "namespaces/obj.yaml", opts...)
}

// UnstructuredAtPath returns an Unstructured with the specified gvk.
func UnstructuredAtPath(gvk schema.GroupVersionKind, path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(UnstructuredObject(gvk, opts...), path)
}
