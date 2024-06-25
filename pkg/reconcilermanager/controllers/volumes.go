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
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/metadata"
)

// GitCredentialVolume is the volume name of the git credentials.
const GitCredentialVolume = "git-creds"

// HelmCredentialVolume is the volume name of the git credentials.
const HelmCredentialVolume = "helm-creds"

// CACertVolume is the volume name of the CA certificate.
const CACertVolume = "ca-cert"

// CACertSecretKey is the name of the key in the Secret's data map whose value holds the CA cert
const CACertSecretKey = "cert"

// CACertPath is the path where the certificate is mounted.
const CACertPath = "/etc/ca-cert"

// defaultMode is the default permission of the `gcp-ksa` volume.
var defaultMode int32 = 0644

// expirationSeconds is the requested duration of validity of the service account token.
// As the token approaches expiration, the kubelet volume plugin will proactively rotate the service account token.
// It sets to 48 hours.
var expirationSeconds = int64((48 * time.Hour).Seconds())

// filterVolumes returns the volumes depending on different auth types.
// If authType is `none`, `gcenode`, or `gcpserviceaccount`, it won't mount the `git-creds` volume.
// If authType is `gcpserviceaccount` with fleet membership available, it also mounts a `gcp-ksa` volume.
func filterVolumes(existing []corev1.Volume, authType configsync.AuthType, secretName, caCertSecretName string, sourceType configsync.SourceType, membership *hubv1.Membership) []corev1.Volume {
	var updatedVolumes []corev1.Volume

	for _, volume := range existing {
		if volume.Name == GitCredentialVolume {
			// Don't mount git-creds volume if auth is 'none', 'gcenode', or 'gcpserviceaccount'
			if SkipForAuth(authType) || sourceType != configsync.GitSource {
				continue
			}
			volume.Secret.SecretName = secretName
		} else if volume.Name == HelmCredentialVolume {
			if SkipForAuth(authType) || sourceType != configsync.HelmSource {
				continue
			}
			volume.Secret.SecretName = secretName
		}
		updatedVolumes = append(updatedVolumes, volume)
	}

	if useCACert(caCertSecretName) {
		updatedVolumes = append(updatedVolumes, corev1.Volume{
			Name: CACertVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: caCertSecretName,
					Items: []corev1.KeyToPath{
						{
							Key:  CACertSecretKey,
							Path: CACertSecretKey,
						},
					},
					DefaultMode: &defaultMode,
				},
			},
		})
	}

	if useFWIAuth(authType, membership) {
		updatedVolumes = append(updatedVolumes, corev1.Volume{
			Name: gcpKSAVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience:          membership.Spec.WorkloadIdentityPool,
								ExpirationSeconds: &expirationSeconds,
								Path:              gsaTokenPath,
							},
						},
						{
							DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: googleApplicationCredentialsFile,
									FieldRef: &corev1.ObjectFieldSelector{
										APIVersion: "v1",
										FieldPath:  fmt.Sprintf("metadata.annotations['%s']", metadata.FleetWorkloadIdentityCredentials),
									},
								},
							}},
						},
					},
					DefaultMode: &defaultMode,
				},
			},
		})
	}

	return updatedVolumes
}

// volumeMounts returns a sorted list of VolumeMounts by filtering out git-creds
// VolumeMount when secret is 'none' or 'gcenode'.
func volumeMounts(auth configsync.AuthType, caCertSecretRef string, sourceType configsync.SourceType, vm []corev1.VolumeMount) []corev1.VolumeMount {
	var volumeMount []corev1.VolumeMount
	if useCACert(caCertSecretRef) {
		volumeMount = append(volumeMount, corev1.VolumeMount{
			MountPath: CACertPath,
			Name:      CACertVolume,
			ReadOnly:  true,
		})
	}
	for _, volume := range vm {
		if volume.Name == GitCredentialVolume && (SkipForAuth(auth) || sourceType != configsync.GitSource) {
			continue
		}
		if volume.Name == HelmCredentialVolume && (SkipForAuth(auth) || sourceType != configsync.HelmSource) {
			continue
		}
		volumeMount = append(volumeMount, volume)
	}
	sort.Slice(volumeMount[:], func(i, j int) bool {
		return volumeMount[i].Name < volumeMount[j].Name
	})
	return volumeMount
}
