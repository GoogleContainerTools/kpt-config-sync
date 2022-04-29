// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package mutation

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/yaml"
)

const (
	Annotation = "config.kubernetes.io/apply-time-mutation"
)

func HasAnnotation(u *unstructured.Unstructured) bool {
	if u == nil {
		return false
	}
	_, found := u.GetAnnotations()[Annotation]
	return found
}

// ReadAnnotation returns the slice of substitutions parsed from the
// apply-time-mutation annotation within the supplied unstructured object.
func ReadAnnotation(obj *unstructured.Unstructured) (ApplyTimeMutation, error) {
	mutation := ApplyTimeMutation{}
	if obj == nil {
		return mutation, nil
	}
	mutationYaml, found := obj.GetAnnotations()[Annotation]
	if !found {
		return mutation, nil
	}
	if klog.V(5).Enabled() {
		klog.Infof("object (%v) has apply-time-mutation annotation:\n%s", ResourceReferenceFromUnstructured(obj), mutationYaml)
	}

	err := yaml.Unmarshal([]byte(mutationYaml), &mutation)
	if err != nil {
		return mutation, object.InvalidAnnotationError{
			Annotation: Annotation,
			Cause:      err,
		}
	}
	return mutation, nil
}

// WriteAnnotation updates the supplied unstructured object to add the
// apply-time-mutation annotation with a multi-line yaml value.
func WriteAnnotation(obj *unstructured.Unstructured, mutation ApplyTimeMutation) error {
	if obj == nil {
		return errors.New("object is nil")
	}
	if mutation.Equal(ApplyTimeMutation{}) {
		return errors.New("mutation is empty")
	}
	yamlBytes, err := yaml.Marshal(mutation)
	if err != nil {
		return fmt.Errorf("failed to format apply-time-mutation annotation: %v", err)
	}
	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	a[Annotation] = string(yamlBytes)
	obj.SetAnnotations(a)
	return nil
}
