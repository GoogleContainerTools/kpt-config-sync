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
	"errors"
	"fmt"
	"testing"

	goauth "cloud.google.com/go/auth"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/auth"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// These constants hard-code the hash of the ConfigMaps that configure the
// otel-collector. This value is used to check the `configsync.gke.io/configmap`
// annotation value, which is populated by the reconciler-manager to ensure that
// the otel-collector deployment is updated (and pods replaced) any time the
// ConfigMap changes. The hash will change any time the ConfigMap changes. So
// you'll need to update these values any time you make a deliberate change to
// the config or its ConfigMap metadata.
const (
	// depAnnotationGooglecloud is the expected hash of the GCP/GKE-specific
	// otel-collector ConfigMap.
	// See `CollectorConfigGooglecloud` in `pkg/metrics/otel.go`
	// Used by TestOtelReconcilerGooglecloud.
	depAnnotationGooglecloud = "e5cf31ab812961f26bb9307a5ed46a33"
	// depAnnotationGooglecloud is the expected hash of the custom
	// otel-collector ConfigMap test artifact.
	// Used by TestOtelReconcilerCustom.
	depAnnotationCustom = "d166bfb4bea41bdc98b5b718e6c34b44"
	// depAnnotationCustomDeleted is the expected hash of the deleted custom
	// otel-collector ConfigMap test artifact.
	// Used by TestOtelReconcilerDeleteCustom.
	depAnnotationCustomDeleted = "271a8db08c5b57017546587f9b78864d"
	configYAML                 = `apiVersion: v1
data:
  otel-collector-config.yaml: |
    exporters:
      googlecloud:
        metric:
          prefix: custom.googleapis.com/opencensus/config_sync/
          resource_filters:
          - prefix: cloud.account.id
          - prefix: cloud.availability.zone
          - prefix: cloud.platform
          - prefix: cloud.provider
          - prefix: k8s.pod.ip
          - prefix: k8s.pod.namespace
          - prefix: k8s.pod.uid
          - prefix: k8s.container.name
          - prefix: host.id
          - prefix: host.name
          - prefix: k8s.deployment.name
          - prefix: k8s.node.name
          skip_create_descriptor: true
        sending_queue:
          enabled: false
      googlecloud/kubernetes:
        metric:
          create_service_timeseries: true
          instrumentation_library_labels: false
          prefix: kubernetes.io/internal/addons/config_sync/
          service_resource_labels: false
          skip_create_descriptor: true
        sending_queue:
          enabled: false
      prometheus:
        endpoint: 0.0.0.0:8675
        namespace: config_sync
        resource_to_telemetry_conversion:
          enabled: true
    extensions:
      health_check:
        endpoint: 0.0.0.0:13133
    processors:
      batch: null
      filter/cloudmonitoring:
        metrics:
          include:
            match_type: strict
            metric_names:
            - reconciler_errors
            - apply_duration_seconds
            - reconcile_duration_seconds
            - rg_reconcile_duration_seconds
            - last_sync_timestamp
            - pipeline_error_observed
            - declared_resources
            - apply_operations_total
            - resource_fights_total
            - internal_errors_total
            - kcc_resource_count
            - resource_count
            - ready_resource_count
            - cluster_scoped_resource_count
            - resource_ns_count
            - api_duration_seconds
      filter/kubernetes:
        metrics:
          include:
            match_type: regexp
            metric_names:
            - kustomize.*
            - api_duration_seconds
            - reconciler_errors
            - pipeline_error_observed
            - reconcile_duration_seconds
            - rg_reconcile_duration_seconds
            - parser_duration_seconds
            - declared_resources
            - apply_operations_total
            - apply_duration_seconds
            - resource_fights_total
            - remediate_duration_seconds
            - resource_conflicts_total
            - internal_errors_total
            - kcc_resource_count
            - last_sync_timestamp
      metricstransform/cloudmonitoring:
        transforms:
        - action: update
          include: last_sync_timestamp
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - configsync.sync.kind
            - configsync.sync.name
            - configsync.sync.namespace
            - status
        - action: update
          include: declared_resources
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - configsync.sync.kind
            - configsync.sync.name
            - configsync.sync.namespace
        - action: update
          include: apply_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - configsync.sync.kind
            - configsync.sync.name
            - configsync.sync.namespace
            - status
      metricstransform/kubernetes:
        transforms:
        - action: update
          include: api_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
            - operation
        - action: update
          include: declared_resources
          new_name: current_declared_resources
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set: []
        - action: update
          include: kcc_resource_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - resourcegroup
        - action: update
          include: reconciler_errors
          new_name: last_reconciler_errors
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - component
            - errorclass
        - action: update
          include: reconcile_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
        - action: update
          include: rg_reconcile_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - stallreason
        - action: update
          include: last_sync_timestamp
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
        - action: update
          include: parser_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
            - source
            - trigger
        - action: update
          include: pipeline_error_observed
          new_name: last_pipeline_error_observed
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - name
            - component
            - reconciler
        - action: update
          include: apply_operations_total
          new_name: apply_operations_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - controller
            - operation
            - status
        - action: update
          include: apply_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
        - action: update
          include: resource_fights_total
          new_name: resource_fights_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - name
            - component
            - reconciler
        - action: update
          include: resource_conflicts_total
          new_name: resource_conflicts_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set: []
        - action: update
          include: internal_errors_total
          new_name: internal_errors_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set: []
        - action: update
          include: remediate_duration_seconds
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - status
        - action: update
          include: kustomize_field_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - field_name
        - action: update
          include: kustomize_deprecating_field_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - deprecating_field
        - action: update
          include: kustomize_simplification_adoption_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - simplification_field
        - action: update
          include: kustomize_builtin_transformers
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - k8s_metadata_transformer
        - action: update
          include: kustomize_helm_inflator_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - helm_inflator
        - action: update
          include: kustomize_base_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - base_source
        - action: update
          include: kustomize_patch_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - patch_field
        - action: update
          include: kustomize_ordered_top_tier_metrics
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set:
            - top_tier_field
        - action: update
          include: kustomize_resource_count
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set: []
        - action: update
          include: kustomize_build_latency
          operations:
          - action: aggregate_labels
            aggregation_type: max
            label_set: []
      resourcedetection:
        detectors:
        - env
        - gcp
    receivers:
      opencensus:
        endpoint: 0.0.0.0:55678
    service:
      extensions:
      - health_check
      pipelines:
        metrics/cloudmonitoring:
          exporters:
          - googlecloud
          processors:
          - batch
          - filter/cloudmonitoring
          - metricstransform/cloudmonitoring
          - resourcedetection
          receivers:
          - opencensus
        metrics/kubernetes:
          exporters:
          - googlecloud/kubernetes
          processors:
          - batch
          - filter/kubernetes
          - metricstransform/kubernetes
          - resourcedetection
          receivers:
          - opencensus
        metrics/prometheus:
          exporters:
          - prometheus
          processors:
          - batch
          receivers:
          - opencensus
kind: ConfigMap
metadata:
  labels:
    app: opentelemetry
    component: otel-collector
    configmanagement.gke.io/arch: csmr
    configmanagement.gke.io/system: "true"
  name: otel-collector-googlecloud
  namespace: config-management-monitoring
`
)

func setupOtelReconciler(t *testing.T, credentialprovider auth.CredentialProvider, objs ...client.Object) (*syncerFake.Client, *OtelReconciler) {
	t.Helper()

	fakeClient := syncerFake.NewClient(t, core.Scheme, objs...)
	testReconciler := NewOtelReconciler(
		fakeClient,
		controllerruntime.Log.WithName("controllers").WithName("Otel"),
		credentialprovider,
	)
	return fakeClient, testReconciler
}

func TestOtelReconciler(t *testing.T) {
	cm := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorName, configmanagement.MonitoringNamespace)
	credentialProvider := &auth.FakeCredentialProvider{
		CredentialsError: errors.New("credentials: could not find default credentials. Extra unused text."),
	}
	fakeClient, testReconciler := setupOtelReconciler(t, credentialProvider, cm,
		k8sobjects.DeploymentObject(core.Name(metrics.OtelCollectorName),
			core.Namespace(configmanagement.MonitoringNamespace)))

	// Test updating Configmap and Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())

	// compare ConfigMap
	cmKey := client.ObjectKeyFromObject(cm)
	gotConfigMap := &corev1.ConfigMap{}
	err := fakeClient.Get(ctx, cmKey, gotConfigMap)
	require.NoError(t, err, "ConfigMap[%s] not found", cmKey)
	asserter.Equal(t, cm, gotConfigMap, "ConfigMap")

	// compare Deployment annotation
	deployKey := client.ObjectKeyFromObject(cm)
	gotDeployment := &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	t.Log("ConfigMap and Deployment successfully updated")
}

func TestOtelReconcilerGooglecloud(t *testing.T) {
	cm := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorName, configmanagement.MonitoringNamespace)
	credentialProvider := &auth.FakeCredentialProvider{
		CredentialsOut: goauth.NewCredentials(&goauth.CredentialsOptions{
			ProjectIDProvider: goauth.CredentialsPropertyFunc(func(_ context.Context) (string, error) {
				return "test", nil
			}),
		}),
	}
	fakeClient, testReconciler := setupOtelReconciler(t, credentialProvider, cm,
		k8sobjects.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(configmanagement.MonitoringNamespace)))

	// Test updating Configmap and Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantConfigMap := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorGooglecloud,
		map[string]string{"otel-collector-config.yaml": configYAML},
		core.Labels(map[string]string{
			"app":                metrics.OpenTelemetry,
			"component":          metrics.OtelCollectorName,
			metadata.SystemLabel: "true",
			metadata.ArchLabel:   "csmr",
		}),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)

	wantDeployment := k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationGooglecloud)

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())

	// compare ConfigMap
	cmKey := client.ObjectKeyFromObject(wantConfigMap)
	gotConfigMap := &corev1.ConfigMap{}
	err := fakeClient.Get(ctx, cmKey, gotConfigMap)
	require.NoError(t, err, "ConfigMap[%s] not found", cmKey)

	// Compare the YAML content as strings
	gotYAML := gotConfigMapToYAML(gotConfigMap)
	if diff := cmp.Diff(configYAML, gotYAML); diff != "" {
		fmt.Printf("Full otel-collector-googlecloud YAML string to update configYAML with:\n%s", gotYAML)
		t.Fatalf("ConfigMap YAML does not match (-expected +got):\n%s", diff)
	}

	// compare Deployment annotation
	deployKey := client.ObjectKeyFromObject(wantDeployment)
	gotDeployment := &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	t.Log("ConfigMap and Deployment successfully updated")
}

// Helper to convert a ConfigMap object to YAML string for comparison
func gotConfigMapToYAML(cm *corev1.ConfigMap) string {
	cmForMarshal := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      cm.ObjectMeta.Name,
			"namespace": cm.ObjectMeta.Namespace,
			"labels":    cm.ObjectMeta.Labels,
		},
		"data": cm.Data,
	}
	out, _ := yaml.Marshal(cmForMarshal)
	return string(out)
}

func TestOtelReconcilerCustom(t *testing.T) {
	cm := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	cmCustom := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorCustomCM,
		map[string]string{"otel-collector-config.yaml": "custom"},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorCustomCM, configmanagement.MonitoringNamespace)
	credentialProvider := &auth.FakeCredentialProvider{
		// Error: errors.New("could not find default credentials"),
	}
	fakeClient, testReconciler := setupOtelReconciler(t, credentialProvider, cm,
		cmCustom, k8sobjects.DeploymentObject(core.Name(metrics.OtelCollectorName),
			core.Namespace(configmanagement.MonitoringNamespace)))

	// Test updating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationCustom)

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())

	// compare ConfigMap
	cmKey := client.ObjectKeyFromObject(cm)
	gotConfigMap := &corev1.ConfigMap{}
	err := fakeClient.Get(ctx, cmKey, gotConfigMap)
	require.NoError(t, err, "ConfigMap[%s] not found", cmKey)
	asserter.Equal(t, cm, gotConfigMap, "ConfigMap")

	// compare Deployment annotation
	deployKey := client.ObjectKeyFromObject(wantDeployment)
	gotDeployment := &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	t.Log("Deployment successfully updated")
}

func TestOtelReconcilerDeleteCustom(t *testing.T) {
	cm := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	cmCustom := configMapWithData(
		configmanagement.MonitoringNamespace,
		metrics.OtelCollectorCustomCM,
		map[string]string{"otel-collector-config.yaml": "custom"},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorCustomCM, configmanagement.MonitoringNamespace)
	credentialProvider := &auth.FakeCredentialProvider{
		// Error: errors.New("could not find default credentials"),
	}
	fakeClient, testReconciler := setupOtelReconciler(t, credentialProvider, cm,
		cmCustom, k8sobjects.DeploymentObject(core.Name(metrics.OtelCollectorName),
			core.Namespace(configmanagement.MonitoringNamespace)))

	// Test updating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	err := fakeClient.Delete(ctx, cmCustom)
	if err != nil {
		t.Fatalf("error deleting custom config map, got error: %q, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationCustomDeleted)

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())

	// compare ConfigMap
	cmKey := client.ObjectKeyFromObject(cm)
	gotConfigMap := &corev1.ConfigMap{}
	err = fakeClient.Get(ctx, cmKey, gotConfigMap)
	require.NoError(t, err, "ConfigMap[%s] not found", cmKey)
	asserter.Equal(t, cm, gotConfigMap, "ConfigMap")

	// compare Deployment annotation
	deployKey := client.ObjectKeyFromObject(wantDeployment)
	gotDeployment := &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	t.Log("Deployment successfully updated")
}

func setupOtelSAReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *OtelSAReconciler) {
	t.Helper()

	fakeClient := syncerFake.NewClient(t, core.Scheme, objs...)
	testReconciler := NewOtelSAReconciler("",
		fakeClient,
		controllerruntime.Log.WithName("controllers").WithName(OtelSALoggerName),
	)
	return fakeClient, testReconciler
}

const test1GSAEmail = "metric-writer@test1.iam.gserviceaccount.com"
const test2GSAEmail = "metric-writer@test2.iam.gserviceaccount.com"

func TestOtelSAReconciler(t *testing.T) {
	sa := k8sobjects.ServiceAccountObject(
		defaultSAName,
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Annotation(GCPSAAnnotationKey, test1GSAEmail),
	)
	reqNamespacedName := namespacedName(defaultSAName, configmanagement.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelSAReconciler(t, sa, k8sobjects.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(configmanagement.MonitoringNamespace)))

	// Verify that the otel-collector Deployment does not have the GCPSAAnnotationKey annotation.
	wantDeployment := k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	ctx := context.Background()
	deployKey := client.ObjectKey{Namespace: configmanagement.MonitoringNamespace, Name: metrics.OtelCollectorName}
	gotDeployment := &appsv1.Deployment{}
	err := fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	// Reconcile the default service account under the config-management-monitoring namespace.
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	// Verify that the otel-collector Deployment has the GCPSAAnnotationKey annotation.
	wantDeployment = k8sobjects.DeploymentObject(
		core.Namespace(configmanagement.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	wantDeployment.Spec.Template.Annotations = map[string]string{GCPSAAnnotationKey: test1GSAEmail}
	gotDeployment = &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)

	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	// Change the GCPSAAnnotationKey annotation
	core.SetAnnotation(sa, GCPSAAnnotationKey, test2GSAEmail)
	if err := fakeClient.Update(ctx, sa, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		t.Fatalf("failed to update the default service account: %v", err)
	}
	// Reconcile the default service account under the config-management-monitoring namespace.
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantDeployment.Spec.Template.Annotations = map[string]string{GCPSAAnnotationKey: test2GSAEmail}
	gotDeployment = &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	// Resets the annotations of the default SA
	sa.SetAnnotations(map[string]string{})
	if err := fakeClient.Update(ctx, sa, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		t.Fatalf("failed to update the default service account: %v", err)
	}
	// Reconcile the default service account under the config-management-monitoring namespace.
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}
	wantDeployment.Spec.Template.Annotations = map[string]string{GCPSAAnnotationKey: ""}
	gotDeployment = &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")
}
