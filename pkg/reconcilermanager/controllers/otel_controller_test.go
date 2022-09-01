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

const (
	depAnnotationGooglecloud = "b22b2b7fedd85383855e396f667bd0c4"
	depAnnotationCustom      = "7021ec2418429875d452e1cd5455724a"
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
		core.ResourceVersion("1"),
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
		core.ResourceVersion("1"),
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
		core.ResourceVersion("1"),
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
	)
	cmCustom := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorCustomCM,
		map[string]string{"otel-collector-config.yaml": "custom"},
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
		core.ResourceVersion("1"),
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
