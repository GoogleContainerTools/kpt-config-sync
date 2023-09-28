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
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAdjustContainerResources(t *testing.T) {
	// containerResources1 is compliant with Autopilot constraints
	containerResources1 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("412Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("412Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources2 is compliant with Autopilot constraints
	containerResources2 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources3 is NOT compliant with Autopilot constraints
	containerResources3 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources4 is NOT compliant with Autopilot constraints
	containerResources4 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources5 is NOT compliant with Autopilot constraints
	containerResources5 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("800m"),
				corev1.ResourceMemory: resource.MustParse("768Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources6 is compliant with Autopilot constraints
	containerResources6 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("740m"),
				corev1.ResourceMemory: resource.MustParse("668Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("740m"),
				corev1.ResourceMemory: resource.MustParse("668Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources7 is NOT compliant with Autopilot constraints
	containerResources7 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("312Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("300m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	adjust3To1, err := adjustmentAnnotation(containerResources3, containerResources1)
	if err != nil {
		t.Error(err)
	}
	adjust5To6, err := adjustmentAnnotation(containerResources5, containerResources6)
	if err != nil {
		t.Error(err)
	}

	testCases := []struct {
		name                       string
		isAutopilot                bool
		declaredContainerResources map[string]corev1.ResourceRequirements
		currentContainerResources  map[string]corev1.ResourceRequirements
		currentAnnotations         map[string]string
		adjusted                   bool
		expectedDeclaredDeployment *appsv1.Deployment
	}{
		{
			name:                       "No Autopilot, declared is same as current => declared not adjusted, current won't be updated",
			isAutopilot:                false,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources1,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			name:                       "No Autopilot, declared is different from current => declared not adjusted, current will be updated by the controller",
			isAutopilot:                false,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources2,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			name:                       "Autopilot enabled, not adjusted by Autopilot yet, declared is same as current => declared not adjusted, current won't be updated",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources1,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			// The declared resources can be higher or lower than the current.
			// If the declared resources are compliant with Autopilot constraints, no update.
			// Otherwise, Autopilot will adjust the resources, which will trigger a reconciliation.
			name:                       "Autopilot enabled, not adjusted by Autopilot yet, declared is different from current => declared not adjusted, current will be updated by the controller",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources2,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		// Mimic the case of increasing the resources.
		{
			name:                       "Autopilot enabled, adjusted by Autopilot, one or more declared resources are higher than adjusted => declared not adjusted, current will be updated by the controller",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources2,
			currentContainerResources:  containerResources1,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust3To1},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources2)),
		},
		// Autopilot further increases the resources to meet the constraints, so no more update is needed.
		{
			name:                       "Autopilot enabled, adjusted by Autopilot, all declared resources are higher than or equal to input, but all are lower than or equal to adjusted => declared adjusted to current to avoid discrepancy, current won't be updated",
			isAutopilot:                true,
			adjusted:                   true,
			declaredContainerResources: containerResources4,
			currentContainerResources:  containerResources1,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust3To1},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		// Mimic the case of decreasing the resource.
		{
			name:                       "Autopilot enabled, adjusted by Autopilot, one or more declared resources are lower than input => declared not adjusted, current will be updated by the controller",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources7,
			currentContainerResources:  containerResources6,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust5To6},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources7)),
		},
		{
			name:                       "Autopilot enabled, adjusted annotation is empty string => declared not adjusted, current won't be updated",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources7,
			currentContainerResources:  containerResources7,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: ""},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources7)),
		},
		{
			name:                       "Autopilot enabled, adjusted annotation is empty JSON => declared not adjusted, current won't be updated",
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources7,
			currentContainerResources:  containerResources7,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: "{}"},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources7)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			declared := fake.DeploymentObject()
			current := fake.DeploymentObject()

			for containerName, resources := range tc.declaredContainerResources {
				declared.Spec.Template.Spec.Containers = append(declared.Spec.Template.Spec.Containers, corev1.Container{Name: containerName, Resources: resources})
			}

			for containerName, resources := range tc.currentContainerResources {
				current.Spec.Template.Spec.Containers = append(current.Spec.Template.Spec.Containers, corev1.Container{Name: containerName, Resources: resources})
			}

			current.Annotations = map[string]string{}
			for key, value := range tc.currentAnnotations {
				current.Annotations[key] = value
			}

			got, err := adjustContainerResources(tc.isAutopilot, declared, current)
			if err != nil {
				t.Errorf("%s: got unexpected error: %v", tc.name, err)
			}
			if got != tc.adjusted {
				t.Errorf("%s: adjusted, got %t, want %t", tc.name, got, tc.adjusted)
			}
			if diff := cmp.Diff(declared.Spec.Template.Spec.Containers, tc.expectedDeclaredDeployment.Spec.Template.Spec.Containers, cmpopts.EquateEmpty(), cmpopts.SortSlices(
				func(x, y corev1.Container) bool { return x.Name < y.Name })); diff != "" {
				t.Errorf("%s: declared Deployment diff: %s", tc.name, diff)
			}
		})
	}
}

var declaredDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: # this field will be assigned dynamically by the reconciler-manager
  namespace: config-management-system
  labels:
    app: reconciler
    configmanagement.gke.io/system: "true"
    configmanagement.gke.io/arch: "csmr"
spec:
  minReadySeconds: 10
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: reconciler
      configsync.gke.io/deployment-name: "" # this field will be assigned dynamically by the reconciler-manager
  template:
    spec:
      containers:
      - name: otel-agent
        image: gcr.io/config-management-release/otelcontribcol:v0.54.0
        command:
        - /otelcol-contrib
        args:
        - "--config=/conf/otel-agent-config.yaml"
        - "--feature-gates=-exporter.googlecloud.OTLPDirect"
        resources:
          requests:
            cpu: 10m
            memory: 100Mi
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - NET_RAW
        ports:
        - containerPort: 55678 # Default OpenCensus receiver port.
          protocol: TCP
        - containerPort: 8888  # Metrics.
          protocol: TCP
        volumeMounts:
        - name: otel-agent-config-vol
          mountPath: /conf
        livenessProbe:
          httpGet:
            path: /
            port: 13133 # Health Check extension default port.
            scheme: HTTP
        readinessProbe:
          httpGet:
            path: /
            port: 13133 # Health Check extension default port.
            scheme: HTTP 
        imagePullPolicy: IfNotPresent
      dnsPolicy: ClusterFirst
      schedulerName: default-scheduler
      volumes:
      - name: repo
        emptyDir: {}
      - name: kube
        emptyDir: {}
      securityContext:
        fsGroup: 65533
        runAsUser: 1000
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
`
var currentDeployment = `
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    autopilot.gke.io/resource-adjustment: '{"input":{"containers":[{"limits":{"cpu":"1","ephemeral-storage":"1Gi","memory":"1Gi"},"requests":{"cpu":"10m","ephemeral-storage":"1Gi","memory":"100Mi"},"name":"otel-agent"}]},"output":{"containers":[{"limits":{"cpu":"10m","ephemeral-storage":"1Gi","memory":"100Mi"},"requests":{"cpu":"10m","ephemeral-storage":"1Gi","memory":"100Mi"},"name":"otel-agent"}]},"modified":true}'
  name: # this field will be assigned dynamically by the reconciler-manager
  namespace: config-management-system
  labels:
    app: reconciler
    configmanagement.gke.io/system: "true"
    configmanagement.gke.io/arch: "csmr"
spec:
  minReadySeconds: 10
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: reconciler
      configsync.gke.io/deployment-name: "" # this field will be assigned dynamically by the reconciler-manager
  template:
    spec:
      tolerations: # different from declared
      - key: "key1"
        operator: "Exists"
        effect: "NoSchedule"
      nodeSelector: # different from declared
        disktype: ssd
      containers:
      - name: otel-agent
        image: gcr.io/config-management-release/otelcontribcol:v0.54.0
        command:
        - /otelcol-contrib
        args:
        - "--config=/conf/otel-agent-config.yaml"
        - "--feature-gates=-exporter.googlecloud.OTLPDirect"
        resources:
          requests:
            cpu: 10m
            memory: 100Mi
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          capabilities:
            drop:
            - NET_RAW
        ports:
        - containerPort: 55678 # Default OpenCensus receiver port.
          protocol: TCP
        - containerPort: 8888  # Metrics.
          protocol: TCP
        volumeMounts:
        - name: otel-agent-config-vol
          mountPath: /conf
        livenessProbe:
          httpGet:
            path: /
            port: 13133 # Health Check extension default port.
            scheme: HTTP
          timeoutSeconds: 1 # different from declared 
          periodSeconds: 10 # different from declared
          successThreshold: 1 # different from declared
          failureThreshold: 3 # different from declared
        readinessProbe:
          httpGet:
            path: /
            port: 13133 # Health Check extension default port.
            scheme: HTTP
          timeoutSeconds: 1 # different from declared
          periodSeconds: 10 # different from declared
          successThreshold: 1 # different from declared
          failureThreshold: 3 # different from declared
        terminationMessagePath: "/dev/termination" # different from declared
        terminationMessagePolicy: FallbackToLogsOnError # different from declared
        imagePullPolicy: IfNotPresent
      restartPolicy: Never # different from declared
      terminationGracePeriodSeconds: 30 # different from declared
      dnsPolicy: ClusterFirst # different from declared
      schedulerName: default-scheduler # different from declared
      volumes:
      - name: repo
        emptyDir: {}
      - name: kube
        emptyDir: {}
      securityContext:
        fsGroup: 65533
        runAsUser: 1000
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
  revisionHistoryLimit: 15 # different from declared
  progressDeadlineSeconds: 300 # different from declared
`

func TestCompareDeploymentsToCreatePatchData(t *testing.T) {
	declared := yamlToDeployment(t, declaredDeployment)
	currentDeploymentUnstructured := yamlToUnstructured(t, currentDeployment)
	testCases := map[string]struct {
		declared      *appsv1.Deployment
		current       *unstructured.Unstructured
		isAutopilot   bool
		expectedSame  bool
		expectedPatch string
	}{
		"Deployment should preserve custom tolerations for non-Autopilot": {
			declared: func() *appsv1.Deployment {
				deploymentCopy := declared.DeepCopy()
				// tolerations are ignored in the equality check to allow drift.
				// Set a non-ignored field to assert dataToPatch (label)
				deploymentCopy.Labels["test-data"] = "true"
				// This test case ensures that custom tolerations are persisted in the
				// Deployment patch. Tolerations are not set in the default deployment,
				// but can be set by users by patching the reconciler-manager-cm ConfigMap.
				// A previous bug in the allowList logic caused tolerations to be dropped.
				deploymentCopy.Spec.Template.Spec.Tolerations = []corev1.Toleration{
					{
						Key: "foo",
					},
				}
				return deploymentCopy
			}(),
			current:       currentDeploymentUnstructured,
			isAutopilot:   false,
			expectedSame:  false,
			expectedPatch: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"creationTimestamp":null,"labels":{"app":"reconciler","configmanagement.gke.io/arch":"csmr","configmanagement.gke.io/system":"true","test-data":"true"},"namespace":"config-management-system"},"spec":{"minReadySeconds":10,"replicas":1,"selector":{"matchLabels":{"app":"reconciler","configsync.gke.io/deployment-name":""}},"strategy":{"type":"Recreate"},"template":{"metadata":{"creationTimestamp":null},"spec":{"containers":[{"args":["--config=/conf/otel-agent-config.yaml","--feature-gates=-exporter.googlecloud.OTLPDirect"],"command":["/otelcol-contrib"],"image":"gcr.io/config-management-release/otelcontribcol:v0.54.0","imagePullPolicy":"IfNotPresent","livenessProbe":{"httpGet":{"path":"/","port":13133,"scheme":"HTTP"}},"name":"otel-agent","ports":[{"containerPort":55678,"protocol":"TCP"},{"containerPort":8888,"protocol":"TCP"}],"readinessProbe":{"httpGet":{"path":"/","port":13133,"scheme":"HTTP"}},"resources":{"requests":{"cpu":"10m","memory":"100Mi"}},"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["NET_RAW"]},"readOnlyRootFilesystem":true},"volumeMounts":[{"mountPath":"/conf","name":"otel-agent-config-vol"}]}],"dnsPolicy":"ClusterFirst","schedulerName":"default-scheduler","securityContext":{"fsGroup":65533,"runAsNonRoot":true,"runAsUser":1000,"seccompProfile":{"type":"RuntimeDefault"}},"tolerations":[{"key":"foo"}],"volumes":[{"emptyDir":{},"name":"repo"},{"emptyDir":{},"name":"kube"}]}}},"status":{}}`,
		},
		"Deployment should preserve custom tolerations for Autopilot": {
			declared: func() *appsv1.Deployment {
				deploymentCopy := declared.DeepCopy()
				// tolerations are ignored in the equality check to allow drift.
				// Set a non-ignored field to assert dataToPatch (label)
				deploymentCopy.Labels["test-data"] = "true"
				// This test case ensures that custom tolerations are persisted in the
				// Deployment patch. Tolerations are not set in the default deployment,
				// but can be set by users by patching the reconciler-manager-cm ConfigMap.
				// A previous bug in the allowList logic caused tolerations to be dropped.
				deploymentCopy.Spec.Template.Spec.Tolerations = []corev1.Toleration{
					{
						Key: "foo",
					},
				}
				return deploymentCopy
			}(),
			current:       currentDeploymentUnstructured,
			isAutopilot:   true,
			expectedSame:  false,
			expectedPatch: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"creationTimestamp":null,"labels":{"app":"reconciler","configmanagement.gke.io/arch":"csmr","configmanagement.gke.io/system":"true","test-data":"true"},"namespace":"config-management-system"},"spec":{"minReadySeconds":10,"replicas":1,"selector":{"matchLabels":{"app":"reconciler","configsync.gke.io/deployment-name":""}},"strategy":{"type":"Recreate"},"template":{"metadata":{"creationTimestamp":null},"spec":{"containers":[{"args":["--config=/conf/otel-agent-config.yaml","--feature-gates=-exporter.googlecloud.OTLPDirect"],"command":["/otelcol-contrib"],"image":"gcr.io/config-management-release/otelcontribcol:v0.54.0","imagePullPolicy":"IfNotPresent","livenessProbe":{"httpGet":{"path":"/","port":13133,"scheme":"HTTP"}},"name":"otel-agent","ports":[{"containerPort":55678,"protocol":"TCP"},{"containerPort":8888,"protocol":"TCP"}],"readinessProbe":{"httpGet":{"path":"/","port":13133,"scheme":"HTTP"}},"resources":{"requests":{"cpu":"10m","memory":"100Mi"}},"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["NET_RAW"]},"readOnlyRootFilesystem":true},"volumeMounts":[{"mountPath":"/conf","name":"otel-agent-config-vol"}]}],"dnsPolicy":"ClusterFirst","schedulerName":"default-scheduler","securityContext":{"fsGroup":65533,"runAsNonRoot":true,"runAsUser":1000,"seccompProfile":{"type":"RuntimeDefault"}},"tolerations":[{"key":"foo"}],"volumes":[{"emptyDir":{},"name":"repo"},{"emptyDir":{},"name":"kube"}]}}},"status":{}}`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			testDeclared := tc.declared.DeepCopy()
			testCurrent := tc.current.DeepCopy()
			dep, err := compareDeploymentsToCreatePatchData(tc.isAutopilot, testDeclared, testCurrent, reconcilerManagerAllowList, core.Scheme)
			require.NoError(t, err)
			require.Equal(t, tc.expectedSame, dep.same)
			require.Equal(t, tc.expectedPatch, string(dep.dataToPatch))
		})
	}
}

func TestCompareDeploymentsToCreatePatchDataResourceLimits(t *testing.T) {
	declared := yamlToDeployment(t, declaredDeployment)
	currentDeploymentUnstructured := yamlToUnstructured(t, currentDeployment)
	testCases := map[string]struct {
		declared          *appsv1.Deployment
		current           *unstructured.Unstructured
		isAutopilot       bool
		overrideLimit     bool
		manualChangeLimit bool
		expectedSame      bool
	}{
		"non-Autopilot, no override limits, no manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       false,
			overrideLimit:     false,
			manualChangeLimit: false,
			expectedSame:      true,
		},
		"non-Autopilot, no override limits, manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       false,
			overrideLimit:     false,
			manualChangeLimit: true,
			// We do not define some containers' resource limits to make the resource burstable.
			// Config Sync only manages resource limits when a user
			// defines resource limits through the resource override API.
			// When a user resets resource limit overrides, the declared deployment will not have
			// resource limits defined. To make sure a user can always successfully
			// reset the resource limits override we must track differences in resource limits
			// between the declared and current deployment, even if no resource limits are defined
			// in the declared deployment.
			expectedSame: false,
		},
		"non-Autopilot, override limits, no manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       false,
			overrideLimit:     true,
			manualChangeLimit: false,
			expectedSame:      false,
		},
		"non-Autopilot, override limits, manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       false,
			overrideLimit:     true,
			manualChangeLimit: true,
			expectedSame:      false,
		},
		"Autopilot, no override limits, no manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       true,
			overrideLimit:     false,
			manualChangeLimit: false,
			expectedSame:      true,
		},
		"Autopilot, no override limits, manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       true,
			overrideLimit:     false,
			manualChangeLimit: true,
			expectedSame:      true,
		},
		"Autopilot, override limits, no manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       true,
			overrideLimit:     true,
			manualChangeLimit: false,
			expectedSame:      true,
		},
		"Autopilot, override limits, manual change limits": {
			declared:          declared,
			current:           currentDeploymentUnstructured,
			isAutopilot:       true,
			overrideLimit:     true,
			manualChangeLimit: true,
			expectedSame:      true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var testDeclared *appsv1.Deployment
			var testCurrent *unstructured.Unstructured
			if tc.overrideLimit {
				testDeclared = overrideResourceLimits(tc.declared)
			} else {
				testDeclared = tc.declared.DeepCopy()
			}
			if tc.manualChangeLimit {
				var err error
				testCurrent, err = manualChangeResourceLimits(tc.current)
				require.NoError(t, err)
			} else {
				testCurrent = tc.current.DeepCopy()
			}
			dep, err := compareDeploymentsToCreatePatchData(tc.isAutopilot, testDeclared, testCurrent, reconcilerManagerAllowList, core.Scheme)
			require.NoError(t, err)
			require.Equal(t, tc.expectedSame, dep.same)
		})
	}
}

func TestMountConfigMapValuesFiles(t *testing.T) {
	testCases := map[string]struct {
		input    []v1beta1.ValuesFileRef
		expected corev1.PodSpec
	}{
		"empty valuesFileRefs": {
			input:    nil,
			expected: corev1.PodSpec{Containers: []corev1.Container{{}}},
		},
		"one valuesFileRefs": {
			input: []v1beta1.ValuesFileRef{
				{
					Name:    "foo",
					DataKey: "values.yaml",
				},
			},
			expected: corev1.PodSpec{
				Containers: []corev1.Container{{
					VolumeMounts: []corev1.VolumeMount{{
						Name:      "valuesfile-vol-0",
						MountPath: "/etc/config/helm_values_file_path_0",
					}},
					Env: []corev1.EnvVar{{
						Name:  reconcilermanager.HelmValuesFilePaths,
						Value: filepath.Join("/etc/config/helm_values_file_path_0/foo/values.yaml"),
					}},
				}},
				Volumes: []corev1.Volume{
					{
						Name: "valuesfile-vol-0",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "foo",
								},
								Optional: pointer.Bool(true),
								Items: []corev1.KeyToPath{{
									Key:  "values.yaml",
									Path: "foo/values.yaml",
								}},
							},
						},
					},
				},
			},
		},
		"two valuesFileRefs, different ConfigMaps": {
			input: []v1beta1.ValuesFileRef{
				{
					Name:    "foo",
					DataKey: "values.yaml",
				},
				{
					Name:    "bar",
					DataKey: "values.yaml",
				},
			},
			expected: corev1.PodSpec{
				Containers: []corev1.Container{{
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "valuesfile-vol-0",
							MountPath: "/etc/config/helm_values_file_path_0",
						},
						{
							Name:      "valuesfile-vol-1",
							MountPath: "/etc/config/helm_values_file_path_1",
						},
					},
					Env: []corev1.EnvVar{{
						Name:  reconcilermanager.HelmValuesFilePaths,
						Value: filepath.Join("/etc/config/helm_values_file_path_0/foo/values.yaml") + "," + filepath.Join("/etc/config/helm_values_file_path_1/bar/values.yaml"),
					}},
				}},
				Volumes: []corev1.Volume{
					{
						Name: "valuesfile-vol-0",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "foo",
								},
								Optional: pointer.Bool(true),
								Items: []corev1.KeyToPath{{
									Key:  "values.yaml",
									Path: "foo/values.yaml",
								}},
							},
						},
					},
					{
						Name: "valuesfile-vol-1",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "bar",
								},
								Optional: pointer.Bool(true),
								Items: []corev1.KeyToPath{{
									Key:  "values.yaml",
									Path: "bar/values.yaml",
								}},
							},
						},
					},
				},
			},
		},
		"two valuesFileRefs, same ConfigMap, same key": {
			input: []v1beta1.ValuesFileRef{
				{
					Name:    "foo",
					DataKey: "values.yaml",
				},
				{
					Name:    "foo",
					DataKey: "values.yaml",
				},
			},
			expected: corev1.PodSpec{
				Containers: []corev1.Container{{
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "valuesfile-vol-0",
							MountPath: "/etc/config/helm_values_file_path_0",
						},
						{
							Name:      "valuesfile-vol-1",
							MountPath: "/etc/config/helm_values_file_path_1",
						},
					},
					Env: []corev1.EnvVar{{
						Name:  reconcilermanager.HelmValuesFilePaths,
						Value: filepath.Join("/etc/config/helm_values_file_path_0/foo/values.yaml") + "," + filepath.Join("/etc/config/helm_values_file_path_1/foo/values.yaml/"),
					}},
				}},
				Volumes: []corev1.Volume{
					{
						Name: "valuesfile-vol-0",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "foo",
								},
								Optional: pointer.Bool(true),
								Items: []corev1.KeyToPath{{
									Key:  "values.yaml",
									Path: "foo/values.yaml",
								}},
							},
						},
					},
					{
						Name: "valuesfile-vol-1",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "foo",
								},
								Optional: pointer.Bool(true),
								Items: []corev1.KeyToPath{{
									Key:  "values.yaml",
									Path: "foo/values.yaml",
								}},
							},
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			container := corev1.Container{}
			spec := corev1.PodSpec{Containers: []corev1.Container{container}}
			mountConfigMapValuesFiles(&spec, &spec.Containers[0], tc.input)
			require.Equal(t, tc.expected, spec)
		})
	}
}

func overrideResourceLimits(dep *appsv1.Deployment) *appsv1.Deployment {
	updatedDeployment := dep.DeepCopy()
	resources := &updatedDeployment.Spec.Template.Spec.Containers[0].Resources
	resources.Limits = corev1.ResourceList{}
	resources.Limits[corev1.ResourceMemory] = resource.MustParse("300Mi")
	resources.Limits[corev1.ResourceCPU] = resource.MustParse("300m")

	return updatedDeployment
}

func manualChangeResourceLimits(unDep *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	updatedDeploymentUnstructured := unDep.DeepCopy()
	o, err := kinds.ToTypedObject(updatedDeploymentUnstructured, core.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to convert unstructured to typed object: %v", err)
	}
	dep, ok := o.(*appsv1.Deployment)
	if !ok {
		return nil, fmt.Errorf("failed to convert the Object to Deployment type")
	}
	resources := &dep.Spec.Template.Spec.Containers[0].Resources
	resources.Limits = corev1.ResourceList{}
	resources.Limits[corev1.ResourceMemory] = resource.MustParse("400Mi")
	resources.Limits[corev1.ResourceCPU] = resource.MustParse("400m")
	updatedDeploymentUnstructured, err = kinds.ToUnstructured(dep, core.Scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to convert Deployment Object to Unstructured: %v", err)
	}
	return updatedDeploymentUnstructured, nil
}

func addContainerWithResources(resources map[string]corev1.ResourceRequirements) core.MetaMutator {
	return func(obj client.Object) {
		o, ok := obj.(*appsv1.Deployment)
		if ok {
			for n, r := range resources {
				o.Spec.Template.Spec.Containers = append(o.Spec.Template.Spec.Containers, corev1.Container{Name: n, Resources: r})
			}
		}
	}
}

func adjustmentAnnotation(input, output map[string]corev1.ResourceRequirements) (string, error) {
	adjustment := util.ResourceMutation{
		Input:    mapToPodResources(input),
		Output:   mapToPodResources(output),
		Modified: true,
	}

	annotation, err := json.Marshal(adjustment)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the adjusted resources: %w", err)
	}
	return string(annotation), nil
}

func mapToPodResources(m map[string]corev1.ResourceRequirements) *util.PodResources {
	var containers []util.ContainerResources
	for name, resources := range m {
		containers = append(containers, util.ContainerResources{
			Name:                 name,
			ResourceRequirements: resources,
		})
	}
	return &util.PodResources{Containers: containers}
}
