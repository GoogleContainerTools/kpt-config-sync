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

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/auth"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &OtelReconciler{}

// OtelReconciler reconciles OpenTelemetry ConfigMaps.
type OtelReconciler struct {
	otelBaseController

	credentialProvider auth.CredentialProvider
}

// NewOtelReconciler returns a new OtelReconciler.
func NewOtelReconciler(client client.Client, log logr.Logger, credentialProvider auth.CredentialProvider) *OtelReconciler {
	return &OtelReconciler{
		otelBaseController: otelBaseController{
			LoggingController: NewLoggingController(log),
			client:            client,
		},
		credentialProvider: credentialProvider,
	}
}

// Reconcile the otel ConfigMap and update the Deployment annotation.
func (r *OtelReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = r.SetLoggerValues(ctx, "otel", req.NamespacedName.String())

	configMapDataHash, err := r.reconcileConfigMap(ctx, req)
	if err != nil {
		return controllerruntime.Result{}, err
	}

	if configMapDataHash == nil {
		return controllerruntime.Result{}, nil
	}
	base16Hash := fmt.Sprintf("%x", configMapDataHash)
	if err := r.updateDeploymentAnnotation(ctx, metadata.ConfigMapAnnotationKey, base16Hash); err != nil {
		return controllerruntime.Result{}, err
	}
	return controllerruntime.Result{}, nil
}

func otelCollectorDeploymentRef() client.ObjectKey {
	return client.ObjectKey{
		Name:      metrics.OtelCollectorName,
		Namespace: configmanagement.MonitoringNamespace,
	}
}

// reconcileConfigMap reconciles ConfigMaps declared in the `config-management-monitoring`
// namespace and returns its hash.
//
// If the reconciled ConfigMap is the standard `otel-collector` map, we check
// whether Application Default Credentials exist. If so, we create a new map with
// a collector config that includes both a Prometheus and a Googlecloud exporter.
func (r *OtelReconciler) reconcileConfigMap(ctx context.Context, req reconcile.Request) ([]byte, error) {
	// The otel-collector Deployment only reads from the `otel-collector` and
	// `otel-collector-custom` ConfigMaps, so we only reconcile these two maps.
	if req.Name != metrics.OtelCollectorName && req.Name != metrics.OtelCollectorCustomCM {
		return nil, nil
	}

	cm := &corev1.ConfigMap{}
	if err := r.client.Get(ctx, req.NamespacedName, cm); err != nil {
		if apierrors.IsNotFound(err) {
			// Returning a hash to ensure the otel-collector deployment is restarted anytime
			// a otel config map is removed. This protects us around a scenario where the user
			// adds a custom otel config map and later removes the config map.
			return hash(req.String())
		}
		return nil, status.APIServerErrorf(err, "failed to get ConfigMap: %s", req.NamespacedName)
	}
	if cm.Name == metrics.OtelCollectorName {
		return r.configureGooglecloudConfigMap(ctx)
	}
	return hash(cm)
}

// configureGooglecloudConfigMap creates or updates a map with a config that
// enables Googlecloud exporter if Application Default Credentials are present.
func (r *OtelReconciler) configureGooglecloudConfigMap(ctx context.Context) ([]byte, error) {
	// Only configure otel-collector to export metrics to Cloud Monitoring if
	// GCP credentials are configured on the reconciler-manager.
	// This assumes the otel-collector will be similarly configured.
	_, err := r.credentialProvider.Credentials()
	if err != nil {
		if auth.IsCredentialsNotFoundError(err) {
			// No injected credentials
			return nil, nil
		}
		return nil, err
	}

	cm := &corev1.ConfigMap{}
	cm.Name = metrics.OtelCollectorGooglecloud
	cm.Namespace = configmanagement.MonitoringNamespace

	key := client.ObjectKeyFromObject(cm)
	r.Logger(ctx).V(3).Info("Upserting managed object",
		logFieldObjectRef, key.String(),
		logFieldObjectKind, "ConfigMap")
	op, err := CreateOrUpdate(ctx, r.client, cm, func() error {
		cm.Labels = map[string]string{
			"app":                metrics.OpenTelemetry,
			"component":          metrics.OtelCollectorName,
			metadata.SystemLabel: "true",
			metadata.ArchLabel:   "csmr",
		}
		cm.Data = map[string]string{
			"otel-collector-config.yaml": metrics.CollectorConfigGooglecloud,
		}
		return nil
	})
	if err != nil {
		return nil, status.APIServerErrorf(err, "failed to upsert ConfigMap: %s", key)
	}
	if op != controllerutil.OperationResultNone {
		r.Logger(ctx).Info("Upserting managed object successful",
			logFieldObjectRef, key.String(),
			logFieldObjectKind, "ConfigMap",
			logFieldOperation, op)
		return hash(cm)
	}
	return nil, nil
}

// Register otel controller with reconciler-manager.
func (r *OtelReconciler) Register(mgr controllerruntime.Manager) error {
	// Process create / update events for resources in the `config-management-monitoring` namespace.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetNamespace() == configmanagement.MonitoringNamespace
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetNamespace() == configmanagement.MonitoringNamespace
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetNamespace() == configmanagement.MonitoringNamespace
		},
	}
	return controllerruntime.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		For(&corev1.ConfigMap{}).
		WithEventFilter(p).
		Complete(r)
}
