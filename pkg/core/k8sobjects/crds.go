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

package k8sobjects

import (
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
)

// CRDV1Beta1ObjectForGVK returns a v1beta1.CustomResourceDefinition for the
// specified GroupVersionKind and ResourceScope.
func CRDV1Beta1ObjectForGVK(gvk schema.GroupVersionKind, scope apiextensionsv1beta1.ResourceScope, opts ...core.MetaMutator) *apiextensionsv1beta1.CustomResourceDefinition {
	obj := &apiextensionsv1beta1.CustomResourceDefinition{
		TypeMeta: ToTypeMeta(kinds.CustomResourceDefinitionV1Beta1()),
	}
	defaultMutate(obj)
	// To be valid, the name must be in the form: `<spec.names.plural>.<spec.group>`
	singularKind := strings.ToLower(gvk.Kind)
	pluralKind := singularKind + "s"
	obj.SetName(pluralKind + "." + gvk.Group)
	obj.Spec = apiextensionsv1beta1.CustomResourceDefinitionSpec{
		Group: gvk.Group,
		Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
			Kind:     gvk.Kind,
			Singular: singularKind,
			Plural:   pluralKind,
		},
		Scope: scope,
		// v1beta1 spec.version is converted to spec.versions for v1
		Version: gvk.Version,
	}
	mutate(obj, opts...)
	return obj
}

// CRDV1beta1UnstructuredForGVK returns an Unstructured object representing a
// v1beta1.CustomResourceDefinition for the specified GroupVersionKind and
// ResourceScope.
func CRDV1beta1UnstructuredForGVK(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope, opts ...core.MetaMutator) *unstructured.Unstructured {
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(kinds.CustomResourceDefinitionV1Beta1())
	defaultMutate(uObj)
	// To be valid, the name must be in the form: `<spec.names.plural>.<spec.group>`
	singularKind := strings.ToLower(gvk.Kind)
	pluralKind := singularKind + "s"
	uObj.SetName(pluralKind + "." + gvk.Group)
	uObj.Object["spec"] = map[string]interface{}{
		"group": gvk.Group,
		"names": map[string]interface{}{
			"kind":     gvk.Kind,
			"singular": singularKind,
			"plural":   pluralKind,
		},
		"scope": string(scope),
		// v1beta1 spec.version is converted to spec.versions for v1
		"version": gvk.Version,
	}
	mutate(uObj, opts...)
	normalizeUnstructured(uObj)
	return uObj
}

// CRDV1ObjectForGVK returns a v1.CustomResourceDefinition for the
// specified GroupVersionKind and ResourceScope.
func CRDV1ObjectForGVK(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope, opts ...core.MetaMutator) *apiextensionsv1.CustomResourceDefinition {
	obj := &apiextensionsv1.CustomResourceDefinition{
		// TODO: remove TypeMeta from typed objects
		TypeMeta: ToTypeMeta(kinds.CustomResourceDefinitionV1()),
	}
	defaultMutate(obj)
	// To be valid, the name must be in the form: `<spec.names.plural>.<spec.group>`
	singularKind := strings.ToLower(gvk.Kind)
	pluralKind := singularKind + "s"
	obj.SetName(pluralKind + "." + gvk.Group)
	obj.Spec = apiextensionsv1.CustomResourceDefinitionSpec{
		Group: gvk.Group,
		Names: apiextensionsv1.CustomResourceDefinitionNames{
			Kind:     gvk.Kind,
			Singular: singularKind,
			Plural:   pluralKind,
		},
		Scope: scope,
		// v1 uses spec.versions instead of spec.version from v1beta1
		Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
			{
				Name:    gvk.Version,
				Served:  true,
				Storage: true,
			},
		},
	}
	mutate(obj, opts...)
	return obj
}

// CRDV1UnstructuredForGVK returns an Unstructured object representing a
// v1.CustomResourceDefinition for the specified GroupVersionKind and
// ResourceScope.
func CRDV1UnstructuredForGVK(gvk schema.GroupVersionKind, scope apiextensionsv1.ResourceScope, opts ...core.MetaMutator) *unstructured.Unstructured {
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(kinds.CustomResourceDefinitionV1())
	defaultMutate(uObj)
	// To be valid, the name must be in the form: `<spec.names.plural>.<spec.group>`
	singularKind := strings.ToLower(gvk.Kind)
	pluralKind := singularKind + "s"
	uObj.SetName(pluralKind + "." + gvk.Group)
	uObj.Object["spec"] = map[string]interface{}{
		"group": gvk.Group,
		"names": map[string]interface{}{
			"kind":     gvk.Kind,
			"singular": singularKind,
			"plural":   pluralKind,
		},
		"scope": string(scope),
		// v1 uses spec.versions instead of spec.version from v1beta1
		"versions": []interface{}{
			map[string]interface{}{
				"name":    gvk.Version,
				"served":  true,
				"storage": true,
			},
		},
	}
	mutate(uObj, opts...)
	normalizeUnstructured(uObj)
	return uObj
}

// AnvilCRDv1AtPath returns a FileObject wrapping an Unstructured object
// representing a v1.CustomResourceDefinition for the Anvil resource.
func AnvilCRDv1AtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(CRDV1UnstructuredForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped, opts...), path)
}

// AnvilCRDv1beta1AtPath returns a FileObject wrapping an Unstructured object
// representing a v1beta1.CustomResourceDefinition for the Anvil resource.
func AnvilCRDv1beta1AtPath(path string, opts ...core.MetaMutator) ast.FileObject {
	return FileObject(CRDV1beta1UnstructuredForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped, opts...), path)
}
