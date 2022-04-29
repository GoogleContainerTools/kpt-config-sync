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

// Package policycontroller includes a meta-controller and reconciler for
// PolicyController resources.
package policycontroller

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName = "policycontroller-annotator"
)

// AddControllers sets up a meta controller which manages the watches on all
// Constraints and ConstraintTemplates.
func AddControllers(ctx context.Context, mgr manager.Manager) error {
	r, err := newReconciler(ctx, mgr)
	if err != nil {
		return err
	}

	pc, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// The meta-controller watches CRDs since each new ConstraintTemplate will
	// result in a new CRD for the corresponding type of Constraint.
	return pc.Watch(&source.Kind{Type: &apiextensionsv1.CustomResourceDefinition{}}, &handler.EnqueueRequestForObject{})
}
