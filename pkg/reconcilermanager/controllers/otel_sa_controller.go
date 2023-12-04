// Copyright 2023 Google LLC
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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/status"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &OtelSAReconciler{}

const (
	defaultSAName = "default"
	// OtelSALoggerName defines the logger name for OtelSAReconciler
	OtelSALoggerName = "OtelSA"
)

// OtelSAReconciler reconciles the default service account under the config-management-monitoring namespace.
type OtelSAReconciler struct {
	loggingController

	clusterName string
	client      client.Client
	scheme      *runtime.Scheme
}

// NewOtelSAReconciler returns a new OtelSAReconciler.
func NewOtelSAReconciler(clusterName string, client client.Client, log logr.Logger, scheme *runtime.Scheme) *OtelSAReconciler {
	if clusterName == "" {
		clusterName = "unknown_cluster"
	}
	return &OtelSAReconciler{
		loggingController: loggingController{
			log: log,
		},
		clusterName: clusterName,
		client:      client,
		scheme:      scheme,
	}
}

// Reconcile reconciles the default service account under the config-management-monitoring namespace and updates the Deployment annotation.
// This triggers the `otel-collector` Deployment to restart in the event of an annotation update.
func (r *OtelSAReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = r.setLoggerValues(ctx, OtelSALoggerName, req.NamespacedName.String())

	if req.Name != defaultSAName {
		return controllerruntime.Result{}, nil
	}

	sa := &corev1.ServiceAccount{}
	if err := r.client.Get(ctx, req.NamespacedName, sa); err != nil {
		if apierrors.IsNotFound(err) {
			return controllerruntime.Result{}, nil
		}
		return controllerruntime.Result{}, status.APIServerErrorf(err, "failed to get service account %s", req.NamespacedName.String())
	}
	var v string
	if sa.GetAnnotations() != nil && len(sa.GetAnnotations()) > 0 {
		v = sa.GetAnnotations()[GCPSAAnnotationKey]
	}
	// Setting the `iam.gke.io/gcp-service-account` annotation on the default service account under the config-management-monitoring
	// namespace allows the `otel-collector` Deployment to impersonate a GCP service account to export metrics to Cloud Monitoring
	// and Cloud Monarch on a GKE cluster with Workload Identity eanbled.
	// On a cluster without Workload Identity, the annotation does not have any effects.
	// Therefore, we don't check whether the cluster has Workload Identity enabled before updating the Deployment annotation.
	if err := updateDeploymentAnnotation(ctx, r.client, GCPSAAnnotationKey, v); err != nil {
		r.logger(ctx).Error(err, "Failed to update Deployment",
			logFieldObjectRef, otelCollectorDeploymentRef(),
			logFieldObjectKind, "Deployment")
		return controllerruntime.Result{}, err
	}
	r.logger(ctx).Info("Deployment annotation patch successful",
		logFieldObjectRef, otelCollectorDeploymentRef(),
		logFieldObjectKind, "Deployment",
		GCPSAAnnotationKey, v)
	return controllerruntime.Result{}, nil
}

// Register otel Service Account controller with reconciler-manager.
func (r *OtelSAReconciler) Register(mgr controllerruntime.Manager) error {
	// Process create / update events for service accounts in the `config-management-monitoring` namespace.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetNamespace() == configmanagement.MonitoringNamespace
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetNamespace() == configmanagement.MonitoringNamespace
		},
	}
	return controllerruntime.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		For(&corev1.ServiceAccount{}).
		WithEventFilter(p).
		Complete(r)
}
