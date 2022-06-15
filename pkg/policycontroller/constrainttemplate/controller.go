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

// Package constrainttemplate includes a controller and reconciler for PolicyController constraint templates.
package constrainttemplate

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
	controllerName = "policycontroller-constrainttemplate-controller"
	// TemplatesGroup is the api group for gatekeeper constraint templates
	TemplatesGroup = "templates.gatekeeper.sh"
)

var (
	gk = schema.GroupKind{
		Group: TemplatesGroup,
		Kind:  "ConstraintTemplate",
	}

	// GVK is the GVK for gatekeeper ConstraintTemplates.
	GVK = gk.WithVersion("v1beta1")
)

// MatchesGK returns true if the given CRD defines the gatekeeper ConstraintTemplate.
func MatchesGK(crd *apiextensionsv1.CustomResourceDefinition) bool {
	return crd.Spec.Group == TemplatesGroup && crd.Spec.Names.Kind == gk.Kind
}

// AddController adds the ConstraintTemplate controller to the given Manager.
func AddController(mgr manager.Manager) error {
	klog.Info("Adding controller for ConstraintTemplates")
	r := newReconciler(mgr.GetClient())
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	ct := EmptyConstraintTemplate()
	return c.Watch(&source.Kind{Type: &ct}, &handler.EnqueueRequestForObject{})
}

// EmptyConstraintTemplate returns an empty ConstraintTemplate.
func EmptyConstraintTemplate() unstructured.Unstructured {
	ct := unstructured.Unstructured{}
	ct.SetGroupVersionKind(GVK)
	return ct
}
