// Copyright 2024 Google LLC
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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
)

var (
	repoVolume            = corev1.Volume{Name: "repo"}
	kubeVolume            = corev1.Volume{Name: "kube"}
	otelAgentConfigVolume = corev1.Volume{
		Name: "otel-agent-config-vol",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "otel-agent",
				},
			},
		},
	}

	repoVolumeMount      = corev1.VolumeMount{Name: "repo", MountPath: "/repo"}
	gitCredsVolumeMount  = corev1.VolumeMount{Name: "git-creds", MountPath: "/etc/git-secret", ReadOnly: true}
	helmCredsVolumeMount = corev1.VolumeMount{Name: "helm-creds", MountPath: "/etc/helm-secret", ReadOnly: true}
	testMembership       = &hubv1.Membership{
		Spec: hubv1.MembershipSpec{
			// Configuring WorkloadIdentityPool and IdentityProvider to inject FWI creds
			WorkloadIdentityPool: "test-gke-dev.svc.id.goog",
			IdentityProvider:     "https://container.googleapis.com/v1/projects/test-gke-dev/locations/us-central1-c/clusters/fleet-workload-identity-test-cluster",
		},
	}
)

func secretVolume(volumeName, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func TestFilterVolumes(t *testing.T) {
	testCases := []struct {
		name             string
		sourceType       string
		authType         configsync.AuthType
		secretName       string
		caCertSecretName string
		membership       *hubv1.Membership
		existingVolumes  []corev1.Volume
		expectedVolumes  []corev1.Volume
	}{
		{
			name:       "git-creds volumes should be kept with the provided secretName when using ssh to sync from Git repo",
			sourceType: string(configsync.GitSource),
			authType:   configsync.AuthSSH,
			secretName: "ssh-key",
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("git-creds", "ssh-key"),
				otelAgentConfigVolume,
			},
		},
		{
			name:       "git-creds volumes should be removed when using an auth that doesn't need creds to sync from Git repo",
			sourceType: string(configsync.GitSource),
			authType:   configsync.AuthNone,
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				otelAgentConfigVolume,
			},
		},
		{
			name:       "helm-creds volumes should be kept with the provided secretName when using token to sync from Helm chart",
			sourceType: string(configsync.HelmSource),
			authType:   configsync.AuthToken,
			secretName: "token",
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "token"),
				otelAgentConfigVolume,
			},
		},
		{
			name:       "helm-creds volumes should be removed when using an auth that doesn't need creds to sync from Helm chart",
			sourceType: string(configsync.HelmSource),
			authType:   configsync.AuthNone,
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				otelAgentConfigVolume,
			},
		},
		{
			name:             "ca-cert should be added when provided",
			sourceType:       string(configsync.GitSource),
			authType:         configsync.AuthSSH,
			secretName:       "ssh-key",
			caCertSecretName: "ca-cert",
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("git-creds", "ssh-key"),
				caCertVolume("ca-cert"),
				otelAgentConfigVolume,
			},
		},
		{
			name:       "gcp-ksa should be added when using FWI",
			sourceType: string(configsync.OciSource),
			authType:   configsync.AuthK8sServiceAccount,
			membership: testMembership,
			existingVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				secretVolume("helm-creds", "helm-creds"),
				secretVolume("git-creds", "git-creds"),
				otelAgentConfigVolume,
			},
			expectedVolumes: []corev1.Volume{
				repoVolume,
				kubeVolume,
				gcpKSAVolume(testMembership),
				otelAgentConfigVolume,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := filterVolumes(tc.existingVolumes, tc.authType, tc.secretName, tc.caCertSecretName, tc.sourceType, tc.membership)
			assert.ElementsMatch(t, tc.expectedVolumes, actual)
		})
	}
}

func TestVolumeMounts(t *testing.T) {
	testCases := []struct {
		name                 string
		sourceType           string
		authType             configsync.AuthType
		secretName           string
		caCertSecretName     string
		existingVolumeMounts []corev1.VolumeMount
		expectedVolumeMounts []corev1.VolumeMount
	}{
		{
			name:       "git-creds volumeMounts should be kept when using ssh to sync from Git repo",
			sourceType: string(configsync.GitSource),
			authType:   configsync.AuthSSH,
			secretName: "ssh-key",
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
			},
		},
		{
			name:       "git-creds volumes should be removed when using an auth that doesn't need creds to sync from Git repo",
			sourceType: string(configsync.GitSource),
			authType:   configsync.AuthNone,
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
			},
		},
		{
			name:       "helm-creds volumes should be kept when using token to sync from Helm chart",
			sourceType: string(configsync.HelmSource),
			authType:   configsync.AuthToken,
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				helmCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				helmCredsVolumeMount,
			},
		},
		{
			name:       "helm-creds volumes should be removed when using an auth that doesn't need creds to sync from Helm chart",
			sourceType: string(configsync.HelmSource),
			authType:   configsync.AuthNone,
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				helmCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
			},
		},
		{
			name:             "ca-cert should be added when provided",
			sourceType:       string(configsync.GitSource),
			authType:         configsync.AuthSSH,
			secretName:       "ssh-key",
			caCertSecretName: "ca-cert",
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
				caCertVolumeMount(),
			},
		},
		{
			name:       "not a real use case, but just to validate redundant volumeMounts can be removed",
			sourceType: string(configsync.GitSource),
			authType:   configsync.AuthToken,
			existingVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
				helmCredsVolumeMount,
			},
			expectedVolumeMounts: []corev1.VolumeMount{
				repoVolumeMount,
				gitCredsVolumeMount,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := volumeMounts(tc.authType, tc.caCertSecretName, tc.sourceType, tc.existingVolumeMounts)
			assert.ElementsMatch(t, tc.expectedVolumeMounts, actual)
		})
	}
}
