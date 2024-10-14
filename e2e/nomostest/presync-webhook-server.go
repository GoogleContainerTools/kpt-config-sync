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

package nomostest

import (
	"fmt"
	"os"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testPreSyncNamespace = "config-management-pre-sync"
const testOCISignatureVerificationServer = "oci-signature-verification-server"

const testPreSyncSA = "pre-sync-sa"

const testImageValidationWebhook = "image-verification-webhook"

const testOCISignatureVerificationServerImage = testing.TestInfraArtifactRepositoryAddress + "/oci-signature-verification-server:v1.0.0-1252b2d8"

// SetupPreSync sets up the pre-sync environment, including the namespace, service account,
// OCI signature verification server, and validating webhook configuration.
func SetupPreSync(nt *NT) error {
	nt.T.Logf("creating pre-sync namesapce")
	if err := nt.KubeClient.Create(preSyncNamespace()); err != nil {
		return err
	}
	if err := nt.KubeClient.Create(PreSyncServiceAccount()); err != nil {
		return err
	}

	if err := auth(nt); err != nil {
		return err
	}

	if err := installOCISignatureVerificationServer(nt); err != nil {
		nt.describeNotRunningTestPods(testPreSyncNamespace)
		return fmt.Errorf("waiting for git-server Deployment to become available: %w", err)
	}
	if err := applyOCISignatureVerificationValidatingWebhookConfiguration(nt); err != nil {
		return fmt.Errorf("applying OCI signature verification ValidatingWebhookConfiguration: %w", err)
	}
	return nil
}

// auth handles authentication for Cosign and generates TLS certificates.
func auth(nt *NT) error {
	err := generateCosignKeyPair(nt)
	if err != nil {
		return err
	}

	err = createKubernetesSecret(nt, cosignSecretName, testPreSyncNamespace, "generic", "--from-file", cosignPublicKeyPath(nt))
	if err != nil {
		return err
	}

	err = generateTLSKeyPair(nt)
	if err != nil {
		return err
	}

	err = createKubernetesSecret(nt, OCISignatureVerificationSecretName, testPreSyncNamespace, "tls", fmt.Sprintf("--cert=%s", tlsCertPath(nt)), fmt.Sprintf("--key=%s", tlsKeyPath(nt)))
	if err != nil {
		return err
	}

	return nil
}

// installOCISignatureVerificationServer installs the pre-sync webhook server Pod, and returns a callback that
// waits for the Pod to become available.
func installOCISignatureVerificationServer(nt *NT) error {
	objs := OCISignatureVerificationServer()

	for _, o := range objs {
		if err := nt.KubeClient.Apply(o); err != nil {
			return fmt.Errorf("applying %v %s: %w", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), testOCISignatureVerificationServer, testPreSyncNamespace)
}

func OCISignatureVerificationServer() []client.Object {
	objs := []client.Object{
		OCISignatureVerificationService(),
		OCISignatureVerificationDeployment(),
	}
	return objs
}

func preSyncNamespace() *corev1.Namespace {
	return k8sobjects.NamespaceObject(testPreSyncNamespace)
}

func PreSyncServiceAccount() *corev1.ServiceAccount {
	return k8sobjects.ServiceAccountObject(testPreSyncSA,
		core.Namespace(testPreSyncNamespace),
		core.Annotation(controllers.GCPSAAnnotationKey, fmt.Sprintf("%s@%s.iam.gserviceaccount.com",
			registryproviders.ArtifactRegistryReaderName, *e2e.GCPProject)))
}

func OCISignatureVerificationService() *corev1.Service {
	service := k8sobjects.ServiceObject(
		core.Name(testOCISignatureVerificationServer),
		core.Namespace(testPreSyncNamespace),
	)
	service.Spec.Ports = []corev1.ServicePort{{Name: "port", Port: 443}}
	service.Spec.Selector = map[string]string{"app": testOCISignatureVerificationServer}
	return service
}

func OCISignatureVerificationDeployment() *appsv1.Deployment {
	deployment := k8sobjects.DeploymentObject(core.Name(testOCISignatureVerificationServer),
		core.Namespace(testPreSyncNamespace),
		core.Labels(map[string]string{"app": testOCISignatureVerificationServer}),
	)

	deployment.Spec = appsv1.DeploymentSpec{
		Selector: &v1.LabelSelector{
			MatchLabels: map[string]string{"app": testOCISignatureVerificationServer},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: map[string]string{"app": testOCISignatureVerificationServer},
				Annotations: map[string]string{
					"safeToEvictAnnotation": "false",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: testPreSyncSA,
				Volumes: []corev1.Volume{
					{
						Name: "ca-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: OCISignatureVerificationSecretName},
						},
					},
					{
						Name: "cosign-key",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: cosignSecretName},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:    "webhook-server",
						Image:   testOCISignatureVerificationServerImage,
						Command: []string{"/webhook-server"},
						Ports: []corev1.ContainerPort{
							{ContainerPort: 443},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "ca-certs", MountPath: "/tls"},
							{Name: "cosign-key", MountPath: "/cosign-key"},
						},
						ImagePullPolicy: corev1.PullAlways,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt32(443),
								},
							},
							FailureThreshold: 6,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt(443),
								},
							},
							FailureThreshold: 2,
						},
					},
				},
				ImagePullSecrets: []corev1.LocalObjectReference{},
			},
		},
		Strategy:        appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		MinReadySeconds: 2,
	}
	return deployment
}

func applyOCISignatureVerificationValidatingWebhookConfiguration(nt *NT) error {
	caBundle, err := getCABundle(tlsCertPath(nt))
	if err != nil {
		return fmt.Errorf("getting CA Bundle: %v", err)
	}

	validatingWebhookConfiguration := k8sobjects.AdmissionWebhookObject(testImageValidationWebhook)

	validatingWebhookConfiguration.Webhooks = []admissionv1.ValidatingWebhook{
		{
			Name: "imageverification.webhook.com",
			ClientConfig: admissionv1.WebhookClientConfig{
				Service: &admissionv1.ServiceReference{
					Name:      testOCISignatureVerificationServer,
					Namespace: testPreSyncNamespace,
					Path:      ptr.To("/validate"),
					Port:      ptr.To(int32(443)),
				},
				CABundle: caBundle,
			},
			Rules: []admissionv1.RuleWithOperations{
				{
					Operations: []admissionv1.OperationType{admissionv1.Update},
					Rule: admissionv1.Rule{
						APIGroups:   []string{"configsync.gke.io"},
						APIVersions: []string{"v1beta1", "v1alpha1"},
						Resources:   []string{"rootsyncs", "reposyncs"},
						Scope:       (*admissionv1.ScopeType)(ptr.To("*")),
					},
				},
			},
			AdmissionReviewVersions: []string{"v1", "v1beta1"},
			SideEffects:             ptr.To(admissionv1.SideEffectClassNone),
		},
	}

	if err := nt.KubeClient.Apply(validatingWebhookConfiguration); err != nil {
		return fmt.Errorf("applying ValidatingWebhookConfiguration %v: %w", testImageValidationWebhook, err)
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.ValidatingWebhookConfiguration(), testImageValidationWebhook, "")
}

func getCABundle(tlsKeyPath string) ([]byte, error) {
	fileContent, err := os.ReadFile(tlsKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	return fileContent, nil
}
