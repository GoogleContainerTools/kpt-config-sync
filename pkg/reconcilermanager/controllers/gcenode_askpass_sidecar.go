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
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
)

const (
	// The GCENode* values are interpolated in the prepareGCENodeSnippet function
	// Keep the image tag consistent with nomos-operator.
	gceNodeAskpassImageTag = "20220326001639"
	// GceNodeAskpassSidecarName is the container name of gcenode-askpass-sidecar.
	GceNodeAskpassSidecarName = "gcenode-askpass-sidecar"
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

func gceNodeAskPassContainerImage(name, tag string) string {
	return fmt.Sprintf("gcr.io/config-management-release/%v:%v", name, tag)
}

func configureGceNodeAskPass(cr *corev1.Container, gsaEmail string, injectFWICreds bool) {
	if injectFWICreds {
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
	cr.Env = append(cr.Env, corev1.EnvVar{
		Name:  gsaEmailEnvKey,
		Value: gsaEmail,
	})
	cr.Name = GceNodeAskpassSidecarName
	cr.Image = gceNodeAskPassContainerImage(GceNodeAskpassSidecarName, gceNodeAskpassImageTag)
	cr.Args = buildArgs(gceNodeAskpassPort)
	cr.SecurityContext = setSecurityContext()
	cr.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	cr.TerminationMessagePath = corev1.TerminationMessagePathDefault
	cr.ImagePullPolicy = corev1.PullIfNotPresent
}

func gceNodeAskPassSidecar(gsaEmail string, injectFWICreds bool) corev1.Container {
	var cr corev1.Container
	configureGceNodeAskPass(&cr, gsaEmail, injectFWICreds)
	return cr
}

func buildArgs(port int) []string {
	return []string{fmt.Sprintf("--port=%v", port), "--logtostderr"}
}

// setSecurityContext sets the security context for the gcenode-askpass-sidecar container.
// It drops the NET_RAW capability, disallows privilege escalation and read-only root filesystem.
func setSecurityContext() *corev1.SecurityContext {
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := false
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: &allowPrivilegeEscalation,
		ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"NET_RAW"},
		},
	}
}
