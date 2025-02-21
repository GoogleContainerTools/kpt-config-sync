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

package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// otelBaseController implements common functionality for otel controllers.
type otelBaseController struct {
	loggingController

	client client.Client
}

// updateDeploymentAnnotation updates the otel deployment's spec.template.annotation.
// This triggers the deployment to restart in the event of an annotation update.
func (r *otelBaseController) updateDeploymentAnnotation(ctx context.Context, annotationKey, annotationValue string) error {
	key := otelCollectorDeploymentRef()
	dep := &appsv1.Deployment{}
	dep.Name = key.Name
	dep.Namespace = key.Namespace

	if err := r.client.Get(ctx, key, dep); err != nil {
		return status.APIServerErrorf(err, "failed to get Deployment: %s", key)
	}

	existing := dep.DeepCopy()
	core.SetAnnotation(&dep.Spec.Template, annotationKey, annotationValue)

	// Avoid unnecessary API writes, if possible.
	if equality.Semantic.DeepEqual(existing, dep) {
		return nil
	}

	r.logger(ctx).V(3).Info("Patching object",
		logFieldObjectRef, key.String(),
		logFieldObjectKind, "Deployment",
		annotationKey, annotationValue)
	patch := client.MergeFrom(existing)
	err := r.client.Patch(ctx, dep, patch, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return status.APIServerErrorf(err, "failed to patch Deployment: %s", key)
	}
	r.logger(ctx).Info("Patching object successful",
		logFieldObjectRef, key.String(),
		logFieldObjectKind, "Deployment",
		annotationKey, annotationValue)
	return nil
}
