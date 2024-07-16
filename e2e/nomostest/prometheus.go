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

package nomostest

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prometheusNamespace                = "prometheus"
	prometheusServerDeploymentName     = "prometheus-deployment"
	prometheusServerPodName            = "prometheus"
	prometheusServerContainerName      = "prometheus"
	prometheusClusterRoleName          = "prometheus"
	prometheusConfigMapName            = "prometheus-server-conf"
	prometheusConfigFileName           = "prometheus.yml"
	prometheusServerPort               = 9090
	prometheusConfigSyncMetricPrefix   = "config_sync_"
	prometheusDistributionBucketSuffix = "_bucket"
	prometheusDistributionCountSuffix  = "_count"
	prometheusDistributionSumSuffix    = "_sum"
	prometheusQueryTimeout             = 10 * time.Second
)

// installPrometheus installs Prometheus on the test cluster and waits for it
// to be ready.
func installPrometheus(nt *NT) error {
	nt.T.Log("[SETUP] Installing Prometheus")
	objs := prometheusObjects()
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.KubeClient.Client.Scheme())
		if err != nil {
			return err
		}
		if err := nt.KubeClient.Apply(obj); err != nil {
			return err
		}
		tg.Go(func() error {
			return nt.Watcher.WatchForCurrentStatus(gvk, nn.Name, nn.Namespace)
		})
	}
	if err := tg.Wait(); err != nil {
		nt.describeNotRunningTestPods(prometheusNamespace)
		return fmt.Errorf("waiting for prometheus Deployment to become available: %w", err)
	}
	return nil
}

// uninstallPrometheus uninstalls Prometheus on the test cluster and waits for
// it to be ready.
func uninstallPrometheus(nt *NT) error {
	nt.T.Log("[CLEANUP] Uninstalling Prometheus")
	objs := prometheusObjects()
	return DeleteObjectsAndWait(nt, objs...)
}

func prometheusObjects() []client.Object {
	return []client.Object{
		fake.NamespaceObject(prometheusNamespace),
		prometheusClusterRole(),
		prometheusClusterRoleBinding(),
		prometheusConfigMap(),
		prometheusDeployment(),
	}
}

func prometheusClusterRole() client.Object {
	clusterRole := fake.ClusterRoleObject(core.Name(prometheusClusterRoleName))
	clusterRole.Rules = []rbacv1.PolicyRule{
		{
			Verbs:     []string{"get", "list", "watch"},
			APIGroups: []string{""},
			Resources: []string{
				"nodes",
				"nodes/proxy",
				"services",
				"endpoints",
				"pods",
			},
		},
		{
			Verbs:     []string{"get", "list", "watch"},
			APIGroups: []string{"extensions"},
			Resources: []string{"ingresses"},
		},
		{
			Verbs:           []string{"get"},
			NonResourceURLs: []string{"/metrics"},
		},
	}
	return clusterRole
}

func prometheusClusterRoleBinding() client.Object {
	clusterRole := fake.ClusterRoleBindingObject(core.Name(prometheusClusterRoleName))
	clusterRole.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     prometheusClusterRoleName,
	}
	clusterRole.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "default",
			Namespace: prometheusNamespace,
		},
	}
	return clusterRole
}

func prometheusConfigMap() client.Object {
	configMap := fake.ConfigMapObject(core.Name(prometheusConfigMapName),
		core.Namespace(prometheusNamespace),
		core.Labels(map[string]string{"name": prometheusConfigMapName}))
	configMap.Data = map[string]string{
		"prometheus.rules": `
groups: []
`,
		prometheusConfigFileName: `
global:
  scrape_interval: 5s
  evaluation_interval: 5s

rule_files:
  - /etc/prometheus/prometheus.rules

# Disable alerting.
alerting: {}

scrape_configs:
  # Discover pod metrics if the pod is annotated with prometheus.io/scrape and prometheus.io/port
  - job_name: 'kubernetes-pods'
    kubernetes_sd_configs:
    - role: pod
    metric_relabel_configs:
    - source_labels: [__name__]
      # The metric stores 220k+ data series on AutoGKE 1.26 causing Prometheus OOMKilled.
      # Drop the expensive metric after the scrape has happened, but before
      # the data is ingested by the storage system.
      # For more details, please check b/283001264.
      regex: 'cilium_k8s_client_api_latency_time_seconds.*'
      action: drop
    relabel_configs:
    - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_scrape]
      action: keep
      regex: true
    - source_labels: [__meta_kubernetes_pod_annotation_prometheus_io_path]
      action: replace
      target_label: __metrics_path__
      regex: (.+)
    - source_labels: [__address__, __meta_kubernetes_pod_annotation_prometheus_io_port]
      action: replace
      regex: ([^:]+)(?::\d+)?;(\d+)
      replacement: $1:$2
      target_label: __address__
    - action: labelmap
      regex: __meta_kubernetes_pod_label_(.+)
    - source_labels: [__meta_kubernetes_namespace]
      action: replace
      target_label: kubernetes_namespace
    - source_labels: [__meta_kubernetes_pod_name]
      action: replace
      target_label: kubernetes_pod_name

  # Discover service metrics if the service endpoint is annotated with prometheus.io/scrape and prometheus.io/port
  - job_name: 'kubernetes-service-endpoints'
    kubernetes_sd_configs:
    - role: endpoints
    metric_relabel_configs:
    - source_labels: [__name__]
      # The metric stores 220k+ data series on AutoGKE 1.26 causing Prometheus OOMKilled.
      # Drop the expensive metric after the scrape has happened, but before
      # the data is ingested by the storage system.
      # For more details, please check b/283001264.
      regex: 'cilium_k8s_client_api_latency_time_seconds.*'
      action: drop
    relabel_configs:
    - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scrape]
      action: keep
      regex: true
    - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scheme]
      action: replace
      target_label: __scheme__
      regex: (https?)
    - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_path]
      action: replace
      target_label: __metrics_path__
      regex: (.+)
    - source_labels: [__address__, __meta_kubernetes_service_annotation_prometheus_io_port]
      action: replace
      target_label: __address__
      regex: ([^:]+)(?::\d+)?;(\d+)
      replacement: $1:$2
    - action: labelmap
      regex: __meta_kubernetes_service_label_(.+)
    - source_labels: [__meta_kubernetes_namespace]
      action: replace
      target_label: kubernetes_namespace
    - source_labels: [__meta_kubernetes_service_name]
      action: replace
      target_label: kubernetes_name
`,
	}
	return configMap
}

func prometheusServerSelector() map[string]string {
	return map[string]string{"app": "prometheus-server"}
}

func prometheusDeployment() client.Object {
	deployment := fake.DeploymentObject(core.Name(prometheusServerDeploymentName),
		core.Namespace(prometheusNamespace),
		core.Labels(prometheusServerSelector()),
	)

	configVolume := "prometheus-config-volume"
	configMountPath := "/etc/prometheus"
	configMapMode := int32(0644)
	storageVolume := "prometheus-storage-volume"
	storageMountPath := "/prometheus"

	deployment.Spec = appsv1.DeploymentSpec{
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector: &metav1.LabelSelector{MatchLabels: prometheusServerSelector()},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: prometheusServerSelector(),
				Annotations: map[string]string{
					safeToEvictAnnotation: "false",
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{ // Volume for storing prometheus config
						Name: configVolume,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								DefaultMode: &configMapMode,
								LocalObjectReference: corev1.LocalObjectReference{
									Name: prometheusConfigMapName,
								},
							},
						},
					},
					{ // EmptyDir storage volume
						Name: storageVolume,
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Image: testing.PrometheusImage,
						Name:  prometheusServerContainerName,
						Args: []string{
							fmt.Sprintf("--config.file=%s/%s", configMountPath, prometheusConfigFileName),
							fmt.Sprintf("--storage.tsdb.path=%s/", storageMountPath),
							"--storage.tsdb.no-lockfile",
							// Keep 30 minutes of data. As we are backed by an emptyDir volume,
							// this will count towards the containers memory usage.
							"--storage.tsdb.retention.time=30m",
							"--storage.tsdb.wal-compression",
							// Limit the maximum number of bytes of storage blocks to retain.
							// The oldest data will be removed first.
							"--storage.tsdb.retention.size=1GB",
							// Effectively disable compaction and make blocks short enough so
							// that our retention window can be kept in practice.
							"--storage.tsdb.min-block-duration=10m",
							"--storage.tsdb.max-block-duration=10m",
							fmt.Sprintf("--web.listen-address=:%d", prometheusServerPort),
							"--web.enable-lifecycle",
							"--web.enable-admin-api",
							"--web.route-prefix=/",
						},
						Ports: []corev1.ContainerPort{
							{ContainerPort: prometheusServerPort},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("1.5Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("1.5Gi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: configVolume, MountPath: configMountPath + "/"},
							{Name: storageVolume, MountPath: storageMountPath + "/"},
						},
					},
				},
			},
		},
	}
	return deployment
}
