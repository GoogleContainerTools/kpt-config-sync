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
	"kpt.dev/configsync/pkg/validate/raw/validate"
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

func caCertVolume(caCertSecretName string) corev1.Volume {
	return corev1.Volume{
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
	}
}

func gcpKSAVolume(membership *hubv1.Membership) corev1.Volume {
	return corev1.Volume{
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
	}
}

// isCreds checks whether the volume/volumeMount name is `git-creds` or `helm-creds`.
func isCreds(name string) bool {
	return name == GitCredentialVolume || name == HelmCredentialVolume
}

// gitCredsRequired checks whether the git-creds volume/volumeMount is required
func gitCredsRequired(name string, sourceType configsync.SourceType, authType configsync.AuthType) bool {
	return name == GitCredentialVolume && sourceType == configsync.GitSource &&
		validate.AuthRequiresSecret(sourceType, authType)

}

// helmCredsRequired checks whether the helm-creds volume/volumeMount is required
func helmCredsRequired(name string, sourceType configsync.SourceType, authType configsync.AuthType) bool {
	return name == HelmCredentialVolume && sourceType == configsync.HelmSource &&
		validate.AuthRequiresSecret(sourceType, authType)
}

// filterVolumes returns a list of PodSpec.Volumes depending on different auth types:
//  1. Keep the existing `git-creds` volume ONLY when the source type is git and
//     the auth type is `ssh`, `token`, or `cookiefile`.
//  2. Keep the existing `helm-creds` volume ONLY when the source type is helm
//     and the auth type is `token`.
//  3. Add a new `ca-cert` volume ONLY when it is specified.
//  4. Add a new `gcp-ksa` volume ONLY when the auth type is `gcpserviceaccount`
//     or `ks8serviceaccount` with fleet membership available.
//  5. Keep any other existing volumes that are not listed above.
func filterVolumes(existing []corev1.Volume, authType configsync.AuthType, secretName, caCertSecretName, sourceType string, membership *hubv1.Membership) []corev1.Volume {
	var updatedVolumes []corev1.Volume
	source := configsync.SourceType(sourceType)
	for _, volume := range existing {
		switch {
		case !isCreds(volume.Name):
			// Keep any other existing volumes that are not credentials.
			updatedVolumes = append(updatedVolumes, volume)
		case gitCredsRequired(volume.Name, source, authType) ||
			helmCredsRequired(volume.Name, source, authType):
			// Keep existing creds volume only when required
			volume.Secret.SecretName = secretName
			updatedVolumes = append(updatedVolumes, volume)
		}
	}

	if useCACert(caCertSecretName) {
		updatedVolumes = append(updatedVolumes, caCertVolume(caCertSecretName))
	}
	if useFWIAuth(authType, membership) {
		updatedVolumes = append(updatedVolumes, gcpKSAVolume(membership))
	}
	return updatedVolumes
}

func caCertVolumeMount() corev1.VolumeMount {
	return corev1.VolumeMount{
		MountPath: CACertPath,
		Name:      CACertVolume,
		ReadOnly:  true,
	}
}

// volumeMounts returns a sorted list of container.VolumeMounts depending on different auth types:
//  1. Keep the existing `git-creds` volumeMount ONLY when the source type is git,
//     and the auth type is `ssh`, `token`, or `cookiefile`.
//  2. Keep the existing `helm-creds` volumeMount ONLY when the source type is
//     helm, and auth type is `token`.
//  3. Add a new `ca-cert` volumeMount ONLY when it is specified.
//  4. Keep any other existing volumeMount that are not listed above.
//
// Note: the `gcp-ksa` volumeMount is added by the `injectFWICredsToContainer` function.
func volumeMounts(auth configsync.AuthType, caCertSecretRef, sourceType string, existing []corev1.VolumeMount) []corev1.VolumeMount {
	var volumeMount []corev1.VolumeMount
	if useCACert(caCertSecretRef) {
		volumeMount = append(volumeMount, caCertVolumeMount())
	}
	source := configsync.SourceType(sourceType)
	for _, vm := range existing {
		// Keep any existing volumeMounts that are not credentials, and also
		// keep the credentials volumeMount when required
		if !isCreds(vm.Name) || gitCredsRequired(vm.Name, source, auth) ||
			helmCredsRequired(vm.Name, source, auth) {
			volumeMount = append(volumeMount, vm)
		}
	}
	sort.Slice(volumeMount[:], func(i, j int) bool {
		return volumeMount[i].Name < volumeMount[j].Name
	})
	return volumeMount
}
