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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/oauth2/google"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	depAnnotationGooglecloud = "dd0990bae84b4e0e7c9e5aabe184c5a1"
	depAnnotationCustom      = "9182661d55e260a55da649363c03c187"
)

func setupOtelReconciler(t *testing.T, objs ...client.Object) (*syncerFake.Client, *OtelReconciler) {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}

	fakeClient := syncerFake.NewClient(t, s, objs...)
	testReconciler := NewOtelReconciler("",
		fakeClient,
		controllerruntime.Log.WithName("controllers").WithName("Otel"),
		s,
	)
	return fakeClient, testReconciler
}

func TestOtelReconciler(t *testing.T) {
	cm := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
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

	// compare ConfigMap. Expect no change in the ConfigMap.
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(cm)], cm, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ConfigMap diff %s", diff)
	}

	// compare Deployment annotation. Expect no annotation.
	gotDeployment := fakeClient.Objects[core.IDOf(wantDeployment)].(*appsv1.Deployment)
	if diff := cmp.Diff(gotDeployment.Spec.Template.Annotations, wantDeployment.Spec.Template.Annotations, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Deployment diff %s", diff)
	}
	t.Log("ConfigMap and Deployment successfully updated")
}

func TestOtelReconcilerGooglecloud(t *testing.T) {
	cm := configMapWithData(
		metrics.MonitoringNamespace,
		metrics.OtelCollectorName,
		map[string]string{"otel-collector-config.yaml": ""},
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
	)

	wantDeployment := fake.DeploymentObject(
		core.Namespace(metrics.MonitoringNamespace),
		core.Name(metrics.OtelCollectorName),
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationGooglecloud)

	// compare ConfigMap
	if diff := cmp.Diff(fakeClient.Objects[core.IDOf(wantConfigMap)], wantConfigMap, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("ConfigMap diff %s", diff)
	}

	// compare Deployment annotation
	gotDeployment := fakeClient.Objects[core.IDOf(wantDeployment)].(*appsv1.Deployment)
	if diff := cmp.Diff(gotDeployment.Spec.Template.Annotations, wantDeployment.Spec.Template.Annotations, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Deployment diff %s", diff)
	}
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
	)
	core.SetAnnotation(&wantDeployment.Spec.Template, metadata.ConfigMapAnnotationKey, depAnnotationCustom)

	// compare Deployment annotation
	gotDeployment := fakeClient.Objects[core.IDOf(wantDeployment)].(*appsv1.Deployment)
	if diff := cmp.Diff(gotDeployment.Spec.Template.Annotations, wantDeployment.Spec.Template.Annotations, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("Deployment diff %s", diff)
	}
	t.Log("Deployment successfully updated")
}
