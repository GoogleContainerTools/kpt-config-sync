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
	"reflect"

	traceapi "cloud.google.com/go/trace/apiv2"
	"github.com/go-logr/logr"
	"golang.org/x/oauth2/google"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &OtelReconciler{}

// OtelReconciler reconciles OpenTelemetry ConfigMaps.
type OtelReconciler struct {
	clusterName string
	client      client.Client
	log         logr.Logger
	scheme      *runtime.Scheme
}

// NewOtelReconciler returns a new OtelReconciler.
func NewOtelReconciler(clusterName string, client client.Client, log logr.Logger, scheme *runtime.Scheme) *OtelReconciler {
	if clusterName == "" {
		clusterName = "unknown_cluster"
	}
	return &OtelReconciler{
		clusterName: clusterName,
		client:      client,
		log:         log,
		scheme:      scheme,
	}
}

// Reconcile the otel ConfigMap and update the Deployment annotation.
func (r *OtelReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.log.WithValues("otel", req.NamespacedName)

	configMapDataHash, err := r.reconcileConfigMap(ctx, req)
	if err != nil {
		log.Error(err, "Failed to create/update ConfigMap")
		return controllerruntime.Result{}, err
	}
	err = r.updateDeploymentAnnotation(ctx, configMapDataHash)
	if err != nil {
		log.Error(err, "Failed to update Deployment")
		return controllerruntime.Result{}, err
	}
	return controllerruntime.Result{}, nil
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

	var cm corev1.ConfigMap
	if err := r.client.Get(ctx, req.NamespacedName, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
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
	creds, _ := getDefaultCredentials(ctx)
	if creds != nil && creds.ProjectID != "" {
		var cm corev1.ConfigMap
		cm.Name = metrics.OtelCollectorGooglecloud
		cm.Namespace = metrics.MonitoringNamespace

		op, err := controllerruntime.CreateOrUpdate(ctx, r.client, &cm, func() error {
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
			r.log.Info("ConfigMap successfully reconciled", operationSubjectName, cm.Name, executedOperation, op)
			return hash(cm)
		}
	}
	return nil, nil
}

// updateDeploymentAnnotation updates the otel deployment's spec.template.annotation
// with the otel ConfigMap hash. This triggers the deployment to restart in the
// event of a ConfigMap update.
func (r *OtelReconciler) updateDeploymentAnnotation(ctx context.Context, hash []byte) error {
	if hash == nil {
		return nil
	}

	var dep appsv1.Deployment
	dep.Name = metrics.OtelCollectorName
	dep.Namespace = metrics.MonitoringNamespace
	key := client.ObjectKeyFromObject(&dep)

	if err := r.client.Get(ctx, key, &dep); err != nil {
		return status.APIServerError(err, "failed to get otel Deployment")
	}

	existing := dep.DeepCopy()
	patch := client.MergeFrom(existing)

	// Mutate Annotation with the hash of configmap.data from the otel ConfigMap
	// creates/updates.
	core.SetAnnotation(&dep.Spec.Template, metadata.ConfigMapAnnotationKey, fmt.Sprintf("%x", hash))

	if reflect.DeepEqual(existing, dep) {
		return nil
	}

	if err := r.client.Patch(ctx, &dep, patch); err != nil {
		return err
	}
	r.log.Info("Deployment successfully updated", operationSubjectName, dep.Name)
	return nil
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
	}
	return controllerruntime.NewControllerManagedBy(mgr).
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
