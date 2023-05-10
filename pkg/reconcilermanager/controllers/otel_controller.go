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

	traceapi "cloud.google.com/go/trace/apiv2"
	"github.com/go-logr/logr"
	"golang.org/x/oauth2/google"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
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
	loggingController

	clusterName string
	client      client.Client
	scheme      *runtime.Scheme
}

// NewOtelReconciler returns a new OtelReconciler.
func NewOtelReconciler(clusterName string, client client.Client, log logr.Logger, scheme *runtime.Scheme) *OtelReconciler {
	if clusterName == "" {
		clusterName = "unknown_cluster"
	}
	return &OtelReconciler{
		loggingController: loggingController{
			log: log,
		},
		clusterName: clusterName,
		client:      client,
		scheme:      scheme,
	}
}

// Reconcile the otel ConfigMap and update the Deployment annotation.
func (r *OtelReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	ctx = r.setLoggerValues(ctx, "otel", req.NamespacedName.String())

	configMapDataHash, err := r.reconcileConfigMap(ctx, req)
	if err != nil {
		r.logger(ctx).Error(err, "Failed to create/update ConfigMap",
			logFieldObjectKind, "ConfigMap",
			logFieldObjectRef, req.NamespacedName.String())
		return controllerruntime.Result{}, err
	}

	if configMapDataHash == nil {
		return controllerruntime.Result{}, nil
	}
	base16Hash := fmt.Sprintf("%x", configMapDataHash)
	err = updateDeploymentAnnotation(ctx, r.client, metadata.ConfigMapAnnotationKey, base16Hash)
	if err != nil {
		r.logger(ctx).Error(err, "Failed to update Deployment",
			logFieldObjectRef, otelCollectorDeploymentRef(),
			logFieldObjectKind, "Deployment")
		return controllerruntime.Result{}, err
	}
	r.logger(ctx).Info("Deployment annotation patch successful",
		logFieldObjectRef, otelCollectorDeploymentRef(),
		logFieldObjectKind, "Deployment",
		metadata.ConfigMapAnnotationKey, base16Hash)
	return controllerruntime.Result{}, nil
}

func otelCollectorDeploymentRef() client.ObjectKey {
	return client.ObjectKey{
		Name:      metrics.OtelCollectorName,
		Namespace: metrics.MonitoringNamespace,
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
		return nil, status.APIServerErrorf(err, "failed to get otel ConfigMap %s", req.NamespacedName.String())
	}
	if cm.Name == metrics.OtelCollectorName {
		return r.configureGooglecloudConfigMap(ctx)
	}
	return hash(cm)
}

// configureGooglecloudConfigMap creates or updates a map with a config that
// enables Googlecloud exporter if Application Default Credentials are present.
func (r *OtelReconciler) configureGooglecloudConfigMap(ctx context.Context) ([]byte, error) {
	// Check that GCP credentials are injected
	creds, _ := getDefaultCredentials(ctx)
	if creds == nil || creds.ProjectID == "" {
		// No injected credentials
		return nil, nil
	}

	cm := &corev1.ConfigMap{}
	cm.Name = metrics.OtelCollectorGooglecloud
	cm.Namespace = metrics.MonitoringNamespace
	op, err := controllerruntime.CreateOrUpdate(ctx, r.client, cm, func() error {
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
		return nil, err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, client.ObjectKeyFromObject(cm).String(),
			logFieldObjectKind, "ConfigMap",
			logFieldOperation, op)
		return hash(cm)
	}
	return nil, nil
}

// updateDeploymentAnnotation updates the otel deployment's spec.template.annotation.
// This triggers the deployment to restart in the event of an annotation update.
func updateDeploymentAnnotation(ctx context.Context, c client.Client, annotationKey, annotationValue string) error {
	key := otelCollectorDeploymentRef()
	dep := &appsv1.Deployment{}
	dep.Name = key.Name
	dep.Namespace = key.Namespace

	if err := c.Get(ctx, key, dep); err != nil {
		return status.APIServerError(err, "failed to get otel Deployment")
	}

	existing := dep.DeepCopy()
	patch := client.MergeFrom(existing)

	core.SetAnnotation(&dep.Spec.Template, annotationKey, annotationValue)

	if equality.Semantic.DeepEqual(existing, dep) {
		return nil
	}

	return c.Patch(ctx, dep, patch)
}

// SetupWithManager registers otel controller with reconciler-manager.
func (r *OtelReconciler) SetupWithManager(mgr controllerruntime.Manager) error {
	// Process create / update events for resources in the `config-management-monitoring` namespace.
	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return e.Object.GetNamespace() == metrics.MonitoringNamespace
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetNamespace() == metrics.MonitoringNamespace
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetNamespace() == metrics.MonitoringNamespace
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

// getDefaultCredentials searches for "Application Default Credentials":
// https://developers.google.com/accounts/docs/application-default-credentials.
// It can be overridden during tests.
var getDefaultCredentials = func(ctx context.Context) (*google.Credentials, error) {
	return google.FindDefaultCredentials(ctx, traceapi.DefaultAuthScopes()...)
}
