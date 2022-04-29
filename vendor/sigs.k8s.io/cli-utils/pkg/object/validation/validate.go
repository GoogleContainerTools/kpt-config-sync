// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package validation

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/cli-utils/pkg/multierror"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Validator contains functionality for validating a set of resources prior
// to being used by the Apply functionality. This imposes some constraint not
// always required, such as namespaced resources must have the namespace set.
type Validator struct {
	Mapper    meta.RESTMapper
	Collector *Collector
}

// Validate validates the provided resources. A RESTMapper will be used
// to fetch type information from the live cluster.
func (v *Validator) Validate(objs []*unstructured.Unstructured) {
	crds := findCRDs(objs)
	for _, obj := range objs {
		var objErrors []error
		if err := v.validateKind(obj); err != nil {
			objErrors = append(objErrors, err)
		}
		if err := v.validateName(obj); err != nil {
			objErrors = append(objErrors, err)
		}
		if err := v.validateNamespace(obj, crds); err != nil {
			objErrors = append(objErrors, err)
		}
		if len(objErrors) > 0 {
			// one error per object
			v.Collector.Collect(NewError(
				multierror.Wrap(objErrors...),
				object.UnstructuredToObjMetadata(obj),
			))
		}
	}
}

// findCRDs looks through the provided resources and returns a slice with
// the resources that are CRDs.
func findCRDs(us []*unstructured.Unstructured) []*unstructured.Unstructured {
	var crds []*unstructured.Unstructured
	for _, u := range us {
		if object.IsCRD(u) {
			crds = append(crds, u)
		}
	}
	return crds
}

// validateKind validates the value of the kind field of the resource.
func (v *Validator) validateKind(u *unstructured.Unstructured) error {
	if u.GetKind() == "" {
		return field.Required(field.NewPath("kind"), "kind is required")
	}
	return nil
}

// validateName validates the value of the name field of the resource.
func (v *Validator) validateName(u *unstructured.Unstructured) error {
	if u.GetName() == "" {
		return field.Required(field.NewPath("metadata", "name"), "name is required")
	}
	return nil
}

// validateNamespace validates the value of the namespace field of the resource.
func (v *Validator) validateNamespace(u *unstructured.Unstructured, crds []*unstructured.Unstructured) error {
	// skip namespace validation if kind is missing (avoid redundant error)
	if u.GetKind() == "" {
		return nil
	}
	scope, err := object.LookupResourceScope(u, crds, v.Mapper)
	if err != nil {
		return err
	}

	ns := u.GetNamespace()
	if scope == meta.RESTScopeNamespace && ns == "" {
		return field.Required(field.NewPath("metadata", "namespace"), "namespace is required")
	}
	if scope == meta.RESTScopeRoot && ns != "" {
		return field.Invalid(field.NewPath("metadata", "namespace"), ns, "namespace must be empty")
	}
	return nil
}
