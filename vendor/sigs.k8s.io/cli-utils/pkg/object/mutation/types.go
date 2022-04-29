// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// ApplyTimeMutation is a list of substitutions to perform in the target
// object before applying, after waiting for the source objects to be
// reconciled.
// This most notibly allows status fields to be substituted into spec fields.
type ApplyTimeMutation []FieldSubstitution

// Equal returns true if the substitutions are equivalent, ignoring order.
// Fulfills Equal interface from github.com/google/go-cmp
func (a ApplyTimeMutation) Equal(b ApplyTimeMutation) bool {
	if len(a) != len(b) {
		return false
	}

	mapA := make(map[FieldSubstitution]struct{}, len(a))
	for _, sub := range a {
		mapA[sub] = struct{}{}
	}
	mapB := make(map[FieldSubstitution]struct{}, len(b))
	for _, sub := range b {
		mapB[sub] = struct{}{}
	}
	if len(mapA) != len(mapB) {
		return false
	}
	for b := range mapB {
		if _, exists := mapA[b]; !exists {
			return false
		}
	}
	return true
}

// FieldSubstitution specifies a substitution that will be performed at
// apply-time. The source object field will be read and substituted into the
// target object field, replacing the token.
type FieldSubstitution struct {
	// SourceRef is a reference to the object that contains the source field.
	SourceRef ResourceReference `json:"sourceRef"`

	// SourcePath is a JSONPath reference to a field in the source object.
	// Example: "$.status.number"
	SourcePath string `json:"sourcePath"`

	// TargetPath is a JSONPath reference to a field in the target object.
	// Example: "$.spec.member"
	TargetPath string `json:"targetPath"`

	// Token is the substring to replace in the value of the target field.
	// If empty, the target field value will be set to the source field value.
	// Example: "${project-number}"
	// +optional
	Token string `json:"token,omitempty"`
}

// ResourceReference is a reference to a KRM resource by name and kind.
// One of APIVersion or Group is required.
// Group is generally preferred, to avoid needing to update the version in lock
// step with the referenced resource.
// If neither is provided, the empty group is used.
type ResourceReference struct {
	// Kind is a string value representing the REST resource this object represents.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`

	// APIVersion defines the versioned schema of this representation of an object.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
	// +optional
	APIVersion string `json:"apiVersion,omitempty"`

	// Group is accepted as a version-less alternative to APIVersion
	// More info: https://kubernetes.io/docs/reference/using-api/#api-groups
	// +optional
	Group string `json:"group,omitempty"`

	// Name of the object.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name,omitempty"`

	// Namespace is optional, defaults to the namespace of the target object.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ResourceReferenceFromUnstructured returns the object as a ResourceReference
func ResourceReferenceFromUnstructured(obj *unstructured.Unstructured) ResourceReference {
	return ResourceReference{
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
		Kind:       obj.GetKind(),
		APIVersion: obj.GetAPIVersion(),
	}
}

// ResourceReferenceFromObjMetadata returns the object as a ResourceReference
func ResourceReferenceFromObjMetadata(id object.ObjMetadata) ResourceReference {
	return ResourceReference{
		Name:      id.Name,
		Namespace: id.Namespace,
		Kind:      id.GroupKind.Kind,
		Group:     id.GroupKind.Group,
	}
}

// GroupVersionKind satisfies the ObjectKind interface for all objects that
// embed TypeMeta. Prefers Group over APIVersion.
func (r ResourceReference) GroupVersionKind() schema.GroupVersionKind {
	if r.Group != "" {
		return schema.GroupVersionKind{Group: r.Group, Kind: r.Kind}
	}
	return schema.FromAPIVersionAndKind(r.APIVersion, r.Kind)
}

// ToUnstructured returns the name, namespace, group, version, and kind of the
// ResourceReference, wrapped in a new Unstructured object.
// This is useful for performing operations with
// sigs.k8s.io/controller-runtime/pkg/client's unstructured Client.
func (r ResourceReference) ToUnstructured() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(r.Name)
	obj.SetNamespace(r.Namespace)
	obj.SetGroupVersionKind(r.GroupVersionKind())
	return obj
}

// ToUnstructured returns the name, namespace, group, and kind of the
// ResourceReference, wrapped in a new ObjMetadata object.
func (r ResourceReference) ToObjMetadata() object.ObjMetadata {
	return object.ObjMetadata{
		Namespace: r.Namespace,
		Name:      r.Name,
		GroupKind: r.GroupVersionKind().GroupKind(),
	}
}

// String returns the format GROUP[/VERSION][/namespaces/NAMESPACE]/KIND/NAME
func (r ResourceReference) String() string {
	group := r.Group
	if group == "" {
		group = r.APIVersion
	}
	if r.Namespace != "" {
		return fmt.Sprintf("%s/namespaces/%s/%s/%s", group, r.Namespace, r.Kind, r.Name)
	}
	return fmt.Sprintf("%s/%s/%s", group, r.Kind, r.Name)
}

// Equal returns true if the ResourceReference sets are equivalent, ignoring version.
// Fulfills Equal interface from github.com/google/go-cmp
func (r ResourceReference) Equal(b ResourceReference) bool {
	return r.GroupVersionKind().GroupKind() == b.GroupVersionKind().GroupKind() &&
		r.Name == b.Name &&
		r.Namespace == b.Namespace
}
