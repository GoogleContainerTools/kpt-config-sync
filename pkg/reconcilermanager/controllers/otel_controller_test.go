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
	"testing"

	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/google"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	depAnnotationGooglecloud = "c3b8f7c8647aa20b219d141081ed7f6f"
	// depAnnotationGooglecloud is the expected hash of the custom
	// otel-collector ConfigMap test artifact.
	// Used by TestOtelReconcilerCustom.
	depAnnotationCustom = "d166bfb4bea41bdc98b5b718e6c34b44"
	// depAnnotationCustomDeleted is the expected hash of the deleted custom
	// otel-collector ConfigMap test artifact.
	// Used by TestOtelReconcilerDeleteCustom.
	depAnnotationCustomDeleted = "271a8db08c5b57017546587f9b78864d"
)

func setupOtelReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *OtelReconciler) {
	t.Helper()

	fakeClient := syncerFake.NewClient(t, core.Scheme, objs...)
	testReconciler := NewOtelReconciler("",
		fakeClient,
		controllerruntime.Log.WithName("controllers").WithName("Otel"),
		fakeClient.Scheme(),
	)
	return fakeClient, testReconciler
}

func TestOtelReconciler(t *testing.T) {
	cm := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorName, metrics.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelReconciler(t, cm, fake.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(metrics.MonitoringNamespace)))

	getDefaultCredentials = func(ctx context.Context) (*google.Credentials, error) {
		return nil, errors.New("could not find default credentials")
	}

	// Test updating Configmap and Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
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
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorName, metrics.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelReconciler(t, cm, fake.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(metrics.MonitoringNamespace)))

	getDefaultCredentials = func(ctx context.Context) (*google.Credentials, error) {
		return &google.Credentials{
			ProjectID:   "test",
			TokenSource: nil,
			JSON:        nil,
		}, nil
	}

	// Test updating Configmap and Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantConfigMap := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorGooglecloud,
		map[string]string{"otel-collector-config.yaml": metrics.CollectorConfigGooglecloud},
		core.Labels(map[string]string{
			"app":                metrics.OpenTelemetry,
			"component":          metrics.OtelCollectorName,
			metadata.SystemLabel: "true",
			metadata.ArchLabel:   "csmr",
		}),
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)

	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationGooglecloud)

	asserter := testutil.NewAsserter(cmpopts.EquateEmpty())

	// compare ConfigMap
	cmKey := client.ObjectKeyFromObject(wantConfigMap)
	gotConfigMap := &corev1.ConfigMap{}
	err := fakeClient.Get(ctx, cmKey, gotConfigMap)
	require.NoError(t, err, "ConfigMap[%s] not found", cmKey)
	asserter.Equal(t, wantConfigMap, gotConfigMap, "ConfigMap")

	// compare Deployment annotation
	deployKey := client.ObjectKeyFromObject(wantDeployment)
	gotDeployment := &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)
	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	t.Log("ConfigMap and Deployment successfully updated")
}

func TestOtelReconcilerCustom(t *testing.T) {
	cm := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	cmCustom := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorCustomCM,
		map[string]string{"otel-collector-config.yaml": "custom"},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorCustomCM, metrics.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelReconciler(t, cm, cmCustom, fake.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(metrics.MonitoringNamespace)))

	getDefaultCredentials = func(ctx context.Context) (*google.Credentials, error) {
		return nil, nil
	}

	// Test updating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
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
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	cmCustom := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorCustomCM,
		map[string]string{"otel-collector-config.yaml": "custom"},
		core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
	)
	reqNamespacedName := namespacedName(metrics.OtelCollectorCustomCM, metrics.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelReconciler(t, cm, cmCustom, fake.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(metrics.MonitoringNamespace)))

	getDefaultCredentials = func(ctx context.Context) (*google.Credentials, error) {
		return nil, nil
	}

	// Test updating Deployment resources.
	ctx := context.Background()
	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	err := fakeClient.Delete(ctx, cmCustom)
	if err != nil {
		t.Fatalf("error deleteing custom config map, got error: %q, want error: nil", err)
	}

	if _, err := testReconciler.Reconcile(ctx, reqNamespacedName); err != nil {
		t.Fatalf("unexpected reconciliation error, got error: %q, want error: nil", err)
	}

	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
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
		fakeClient.Scheme(),
	)
	return fakeClient, testReconciler
}

const test1GSAEmail = "metric-writer@test1.iam.gserviceaccount.com"
const test2GSAEmail = "metric-writer@test2.iam.gserviceaccount.com"

func TestOtelSAReconciler(t *testing.T) {
	sa := fake.ServiceAccountObject(
		defaultSAName,
		core.Namespace(metrics.MonitoringNamespace),
		core.Annotation(GCPSAAnnotationKey, test1GSAEmail),
	)
	reqNamespacedName := namespacedName(defaultSAName, metrics.MonitoringNamespace)
	fakeClient, testReconciler := setupOtelSAReconciler(t, sa, fake.DeploymentObject(core.Name(metrics.OtelCollectorName), core.Namespace(metrics.MonitoringNamespace)))

	// Verify that the otel-collector Deployment does not have the GCPSAAnnotationKey annotation.
	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	ctx := context.Background()
	deployKey := client.ObjectKey{Namespace: metrics.MonitoringNamespace, Name: metrics.OtelCollectorName}
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
	wantDeployment = fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	wantDeployment.Spec.Template.Annotations = map[string]string{GCPSAAnnotationKey: test1GSAEmail}
	gotDeployment = &appsv1.Deployment{}
	err = fakeClient.Get(ctx, deployKey, gotDeployment)
	require.NoError(t, err, "Deployment[%s] not found", deployKey)

	asserter.Equal(t, wantDeployment.Spec.Template.Annotations, gotDeployment.Spec.Template.Annotations, "Deployment annotations")

	// Change the GCPSAAnnotationKey annotation
	core.SetAnnotation(sa, GCPSAAnnotationKey, test2GSAEmail)
	if err := fakeClient.Update(ctx, sa); err != nil {
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
	if err := fakeClient.Update(ctx, sa); err != nil {
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
