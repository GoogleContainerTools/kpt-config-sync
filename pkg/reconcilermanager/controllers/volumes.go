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

// defaultMode is the default permission of the `gcp-ksa` volume.
var defaultMode int32 = 0644

// expirationSeconds is the requested duration of validity of the service account token.
// As the token approaches expiration, the kubelet volume plugin will proactively rotate the service account token.
// It sets to 48 hours.
var expirationSeconds = int64((48 * time.Hour).Seconds())

// filterVolumes returns the volumes depending on different auth types.
// If authType is `none`, `gcenode`, or `gcpserviceaccount`, it won't mount the `git-creds` volume.
// If authType is `gcpserviceaccount` with fleet membership available, it also mounts a `gcp-ksa` volume.
func filterVolumes(existing []corev1.Volume, authType, secretName string, membership *hubv1.Membership) []corev1.Volume {
	var updatedVolumes []corev1.Volume

	for _, volume := range existing {
		if volume.Name == GitCredentialVolume {
			// Don't mount git-creds volume if auth is 'none', 'gcenode', or 'gcpserviceaccount'
			if SkipForAuth(authType) {
				continue
			}
			volume.Secret.SecretName = secretName
		}
		updatedVolumes = append(updatedVolumes, volume)
	}

	if authType == configsync.GitSecretGCPServiceAccount && membership != nil {
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
func volumeMounts(auth string, vm []corev1.VolumeMount) []corev1.VolumeMount {
	var volumeMount []corev1.VolumeMount
	for _, volume := range vm {
		if SkipForAuth(auth) && volume.Name == GitCredentialVolume {
			continue
		}
		volumeMount = append(volumeMount, volume)
	}
	sort.Slice(volumeMount[:], func(i, j int) bool {
		return volumeMount[i].Name < volumeMount[j].Name
	})
	return volumeMount
}
