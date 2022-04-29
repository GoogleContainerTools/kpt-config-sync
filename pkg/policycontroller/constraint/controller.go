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

// Package constraint includes a controller and reconciler for PolicyController constraints.
package constraint

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerPrefix = "policycontroller-constraint-controller-"
	// ConstraintsGroup is the api group for gatekeeper constraints
	ConstraintsGroup = "constraints.gatekeeper.sh"
)

var constraintGV = schema.GroupVersion{
	Group:   ConstraintsGroup,
	Version: "v1beta1",
}

// GVK returns the fully qualified GroupVersionKind of the given constraint kind.
func GVK(kind string) schema.GroupVersionKind {
	return constraintGV.WithKind(kind)
}

// MatchesGroup returns true if the given CRD seems to be in the constraints group.
func MatchesGroup(crd *apiextensionsv1.CustomResourceDefinition) bool {
	return crd.Spec.Group == ConstraintsGroup
}

// AddController adds a controller for the specified constraint kind to the given Manager.
func AddController(mgr manager.Manager, kind string) error {
	gvk := GVK(kind)
	r := newReconciler(mgr.GetClient(), gvk)

	klog.Infof("Adding controller for constraint: %s", gvk)
	controllerName := controllerPrefix + gvk.String()
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		klog.Errorf("Error creating %s: %v", controllerName, err)
		return err
	}

	resource := unstructured.Unstructured{}
	resource.SetGroupVersionKind(gvk)
	err = c.Watch(&source.Kind{Type: &resource}, &handler.EnqueueRequestForObject{})
	if err != nil {
		klog.Errorf("Error setting up watch for %s: %v", gvk.String(), err)
		return err
	}

	return nil
}
