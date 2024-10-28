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

package nomostest

import (
	"fmt"
	"os"
	"path/filepath"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OCISignatureVerificationNamespace is the namespace where the OCI signature verification components are deployed.
const OCISignatureVerificationNamespace = "oci-image-verification"

// OCISignatureVerificationServerName is the name of the OCI signature verification server deployment and service.
const OCISignatureVerificationServerName = "oci-signature-verification-server"

// testOCISignatureVerificationSAName is the name of the service account used by the OCI signature verification server.
const testOCISignatureVerificationSAName = "oci-signature-verification-sa"

// testImageValidationWebhook is the name of the validating webhook for image verification.
const testImageValidationWebhook = "image-verification-webhook"

// testOCISignatureVerificationServerImage is the container image used for the OCI signature verification server.
const testOCISignatureVerificationServerImage = testing.TestInfraArtifactRepositoryAddress + "/oci-signature-verification-server:v1.0.0-9b00d631"

// SetupOCISignatureVerification sets up the OCI signature verification environment, including the namespace, service account,
// OCI signature verification server, and validating webhook configuration.
func SetupOCISignatureVerification(nt *NT) error {
	nt.T.Cleanup(func() {
		if err := TeardownOCISignatureVerification(nt); err != nil {
			nt.T.Error(err)
		}
		if nt.T.Failed() {
			nt.PodLogs(OCISignatureVerificationNamespace, OCISignatureVerificationServerName, "", false)
		}
	})
	nt.T.Logf("creating OCI signature verification namespace and service account")
	if err := nt.KubeClient.Create(testOCISignatureVerificationNS()); err != nil {
		return err
	}
	if err := nt.KubeClient.Create(testOCISignatureVerificationSA()); err != nil {
		return err
	}

	if err := auth(nt); err != nil {
		return err
	}

	if err := installOCISignatureVerificationServer(nt); err != nil {
		nt.describeNotRunningTestPods(OCISignatureVerificationNamespace)
		return fmt.Errorf("waiting for git-server Deployment to become available: %w", err)
	}
	if err := testOCISignatureVerificationValidatingWebhookConfiguration(nt); err != nil {
		return fmt.Errorf("applying OCI signature verification ValidatingWebhookConfiguration: %w", err)
	}
	return nil
}

// TeardownOCISignatureVerification removes the OCI signature verification environment,
// including the validating webhook configuration, server deployment, service, namespace, and related resources.
func TeardownOCISignatureVerification(nt *NT) error {
	nt.T.Log("Tearing down OCI signature verification environment")
	if err := nt.KubeClient.Delete(k8sobjects.AdmissionWebhookObject(testImageValidationWebhook)); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Logf("Error deleting ValidatingWebhookConfiguration: %v", err)
	}
	objs := testOCISignatureVerificationServer()
	for _, o := range objs {
		if err := nt.KubeClient.Delete(o); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Logf("Error deleting %T %s/%s: %v", o, o.GetNamespace(), o.GetName(), err)
		}
	}
	secrets := []string{cosignSecretName, privateCertSecretName(ImageVerificationServer)}
	for _, secretName := range secrets {
		if err := nt.KubeClient.Delete(k8sobjects.SecretObject(secretName, core.Namespace(OCISignatureVerificationNamespace))); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Logf("Error deleting Secret %s: %v", secretName, err)
		}
	}
	if err := nt.KubeClient.Delete(testOCISignatureVerificationSA()); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Logf("Error deleting ServiceAccount: %v", err)
	}
	if err := nt.KubeClient.Delete(testOCISignatureVerificationNS()); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Logf("Error deleting Namespace: %v", err)
	}

	nt.T.Log("OCI signature verification environment teardown complete")
	return nil
}

// auth handles authentication for Cosign and generates TLS certificates.
func auth(nt *NT) error {
	err := generateCosignKeyPair(nt)
	if err != nil {
		return err
	}

	err = createSecret(nt, OCISignatureVerificationNamespace, cosignSecretName, fmt.Sprintf("cosign.pub=%s", cosignPublicKeyPath(nt)))
	if err != nil {
		return err
	}

	serverDomains := []string{
		fmt.Sprintf("%s.%s", OCISignatureVerificationServerName, OCISignatureVerificationNamespace),
		fmt.Sprintf("%s.%s.svc", OCISignatureVerificationServerName, OCISignatureVerificationNamespace),
	}
	_, err = generateSSLKeys(nt, ImageVerificationServer, OCISignatureVerificationNamespace, serverDomains)
	if err != nil {
		return err
	}

	// Create secret for the public ca-cert in the oci-image-verification namespace
	// for the oci-signature-verification webhook server to verify certificate.
	sharedTmpDir := filepath.Join(os.TempDir(), NomosE2E, nt.ClusterName)
	sharedTestRegistrySSLDir := filepath.Join(sharedTmpDir, string(RegistrySyncSource), sslDirName)
	testRegistryCACertPath := filepath.Join(sharedTestRegistrySSLDir, caCertFile)
	if err := createSecret(nt, OCISignatureVerificationNamespace, PublicCertSecretName(RegistrySyncSource),
		fmt.Sprintf("cert=%s", testRegistryCACertPath)); err != nil {
		return err
	}

	return nil
}

// installOCISignatureVerificationServer installs the pre-sync webhook server Pod, and returns a callback that
// waits for the Pod to become available.
func installOCISignatureVerificationServer(nt *NT) error {
	objs := testOCISignatureVerificationServer()

	for _, o := range objs {
		if err := nt.KubeClient.Apply(o); err != nil {
			return fmt.Errorf("applying %v %s: %w", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), OCISignatureVerificationServerName, OCISignatureVerificationNamespace)
}

func testOCISignatureVerificationServer() []client.Object {
	objs := []client.Object{
		testOCISignatureVerificationService(),
		testOCISignatureVerificationDeployment(),
	}
	return objs
}

func testOCISignatureVerificationNS() *corev1.Namespace {
	return k8sobjects.NamespaceObject(OCISignatureVerificationNamespace)
}

func testOCISignatureVerificationSA() *corev1.ServiceAccount {
	return k8sobjects.ServiceAccountObject(testOCISignatureVerificationSAName,
		core.Namespace(OCISignatureVerificationNamespace),
		core.Annotation(controllers.GCPSAAnnotationKey, fmt.Sprintf("%s@%s.iam.gserviceaccount.com",
			registryproviders.ArtifactRegistryReaderName, *e2e.GCPProject)))
}

func testOCISignatureVerificationService() *corev1.Service {
	service := k8sobjects.ServiceObject(
		core.Name(OCISignatureVerificationServerName),
		core.Namespace(OCISignatureVerificationNamespace),
	)
	service.Spec.Ports = []corev1.ServicePort{{Name: "port", Port: 8443}}
	service.Spec.Selector = map[string]string{"app": OCISignatureVerificationServerName}
	return service
}

func testOCISignatureVerificationDeployment() *appsv1.Deployment {
	deployment := k8sobjects.DeploymentObject(core.Name(OCISignatureVerificationServerName),
		core.Namespace(OCISignatureVerificationNamespace),
		core.Labels(map[string]string{"app": OCISignatureVerificationServerName}),
	)

	deployment.Spec = appsv1.DeploymentSpec{
		Selector: &v1.LabelSelector{
			MatchLabels: map[string]string{"app": OCISignatureVerificationServerName},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: map[string]string{"app": OCISignatureVerificationServerName},
				Annotations: map[string]string{
					"safeToEvictAnnotation": "false",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: testOCISignatureVerificationSAName,
				Volumes: []corev1.Volume{
					{
						Name: "ca-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: privateCertSecretName(ImageVerificationServer)},
						},
					},
					{
						Name: "cosign-key",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: cosignSecretName},
						},
					},
					{
						Name: "test-registry-ca-certs",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: PublicCertSecretName(RegistrySyncSource)},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:    "webhook-server",
						Image:   testOCISignatureVerificationServerImage,
						Command: []string{"/webhook-server"},
						Ports: []corev1.ContainerPort{
							{ContainerPort: 8443},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "ca-certs", MountPath: "/tls"},
							{Name: "cosign-key", MountPath: "/cosign-key"},
							{Name: "test-registry-ca-certs", MountPath: "/etc/ssl/ca-certs"},
						},
						Env: []corev1.EnvVar{
							{Name: reconcilermanager.OciCACert, Value: "/etc/ssl/ca-certs/cert"},
						},
						ImagePullPolicy: corev1.PullAlways,
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt32(8443),
								},
							},
							FailureThreshold: 6,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt32(8443),
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

func testOCISignatureVerificationValidatingWebhookConfiguration(nt *NT) error {
	caBundle, err := getCABundle(certPath(nt, ImageVerificationServer))
	if err != nil {
		return fmt.Errorf("getting CA Bundle: %v", err)
	}

	validatingWebhookConfiguration := k8sobjects.AdmissionWebhookObject(testImageValidationWebhook)

	validatingWebhookConfiguration.Webhooks = []admissionv1.ValidatingWebhook{
		{
			Name: "imageverification.webhook.com",
			ClientConfig: admissionv1.WebhookClientConfig{
				Service: &admissionv1.ServiceReference{
					Name:      OCISignatureVerificationServerName,
					Namespace: OCISignatureVerificationNamespace,
					Path:      ptr.To("/validate"),
					Port:      ptr.To(int32(8443)),
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

func getCABundle(certPath string) ([]byte, error) {
	fileContent, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	return fileContent, nil
}
