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
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
)

const (
	// gceNodeAskpassPort is the port number of the askpass-sidecar container.
	gceNodeAskpassPort = 9102
	// gcpKSATokenDir specifies the mount path of the GCP KSA directory, including the token file and credentials file.
	gcpKSATokenDir = "/var/run/secrets/tokens/gcp-ksa"
	// googleApplicationCredentialsFile is the name of the Google application credentials file mounted in the container.
	googleApplicationCredentialsFile = "google-application-credentials.json"
	// googleApplicationCredentialsEnvKey is the name of the GOOGLE_APPLICATION_CREDENTIALS env variable in the askpass-sidecar container.
	googleApplicationCredentialsEnvKey = "GOOGLE_APPLICATION_CREDENTIALS"
	// gsaEmailEnvKey is the name of the GSA_EMAIL env variable in the askpass-sidecar container.
	gsaEmailEnvKey = "GSA_EMAIL"
	// gcpKSAVolumeName is the name of the volume used by the askpass-sidecar container.
	gcpKSAVolumeName = "gcp-ksa"
	// gsaTokenPath is the name of the GCP KSA token file mounted in the askpass-sidecar container.
	gsaTokenPath = "token"
)

// injectFWICredsToContainer injects container environment variable and
// volumeMount for FWI credentials to the container.
func injectFWICredsToContainer(cr *corev1.Container, inject bool) {
	if inject {
		cr.Env = append(cr.Env, corev1.EnvVar{
			Name:  googleApplicationCredentialsEnvKey,
			Value: filepath.Join(gcpKSATokenDir, googleApplicationCredentialsFile),
		})
		cr.VolumeMounts = append(cr.VolumeMounts, corev1.VolumeMount{
			Name:      gcpKSAVolumeName,
			ReadOnly:  true,
			MountPath: gcpKSATokenDir,
		})
	}
}

func gceNodeAskPassSidecarEnvs(gsaEmail string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  gsaEmailEnvKey,
			Value: gsaEmail,
		},
	}
}
