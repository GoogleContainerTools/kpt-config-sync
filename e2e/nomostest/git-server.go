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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testGitNamespace = "config-management-system-test"
const testGitServer = "test-git-server"
const testGitServerImage = testing.TestInfraArtifactRegistry + "/git-server:v1.0.0"
const testGitHTTPServer = "http-git-server"
const testGitHTTPServerImage = testing.TestInfraArtifactRegistry + "/http-git-server:v1.0.0-b3b4984cd"

func testGitServerSelector() map[string]string {
	// Note that maps are copied by reference into objects.
	// If this were just a variable, then concurrent usages by Clients may result
	// in concurrent map writes (and thus flaky test panics).
	return map[string]string{"app": testGitServer}
}

// installGitServer installs the git-server Pod, and returns a callback that
// waits for the Pod to become available.
//
// The git-server almost always comes up before 40 seconds, but we give it a
// full minute in the callback to be safe.
func installGitServer(nt *NT) func() error {
	nt.T.Helper()

	objs := gitServer()

	for _, o := range objs {
		err := nt.KubeClient.Create(o)
		if err != nil {
			nt.T.Fatalf("installing %v %s: %v", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), testGitServer, testGitNamespace)
	}
}

func gitServer() []client.Object {
	// Remember that we've already created the git-server's Namespace since the
	// SSH key must exist before we apply the Deployment.
	objs := []client.Object{
		gitService(),
		gitDeployment(),
	}
	return objs
}

func gitNamespace() *corev1.Namespace {
	return fake.NamespaceObject(testGitNamespace)
}

func gitService() *corev1.Service {
	service := fake.ServiceObject(
		core.Name(testGitServer),
		core.Namespace(testGitNamespace),
	)
	service.Spec.Selector = testGitServerSelector()
	service.Spec.Ports = []corev1.ServicePort{{Name: "ssh", Port: 22},
		{Name: "https", Port: 443}}
	return service
}

func gitDeployment() *appsv1.Deployment {
	deployment := fake.DeploymentObject(core.Name(testGitServer),
		core.Namespace(testGitNamespace),
		core.Labels(testGitServerSelector()),
	)
	gitGID := int64(1000)
	deployment.Spec = appsv1.DeploymentSpec{
		MinReadySeconds: 2,
		Strategy:        appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector:        &v1.LabelSelector{MatchLabels: testGitServerSelector()},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: testGitServerSelector(),
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "keys", VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: gitServerSecretName},
					}},
					{Name: "repos", VolumeSource: corev1.VolumeSource{EmptyDir: nil}},
					{Name: "ssl-cert", VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: gitServerCertSecretName},
					}},
				},
				Containers: []corev1.Container{
					{
						Name:  testGitServer,
						Image: testGitServerImage,
						Ports: []corev1.ContainerPort{{ContainerPort: 22}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "keys", MountPath: "/git-server/keys"},
							{Name: "repos", MountPath: "/git-server/repos"},
						},
						// Restart the container if 6 probes fail
						LivenessProbe: newTCPSocketProbe(22, 6),
						// Mark pod as unready if 2 probes fail
						ReadinessProbe: newTCPSocketProbe(22, 2),
					},
					{
						Name:  testGitHTTPServer,
						Image: testGitHTTPServerImage,
						Ports: []corev1.ContainerPort{{ContainerPort: 443}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "repos", MountPath: "/git-server/repos"},
							{Name: "ssl-cert", MountPath: "/etc/nginx/ssl"},
						},
						// Restart the container if 6 probes fail
						LivenessProbe: newTCPSocketProbe(443, 6),
						// Mark pod as unready if 2 probes fail
						ReadinessProbe: newTCPSocketProbe(443, 2),
					},
				},
				ImagePullSecrets: []corev1.LocalObjectReference{},
				SecurityContext: &corev1.PodSecurityContext{
					FSGroup: &gitGID,
				},
			},
		},
	}
	return deployment
}

func newTCPSocketProbe(port int, failureThreshold int32) *corev1.Probe {
	return &corev1.Probe{
		FailureThreshold:    failureThreshold,
		InitialDelaySeconds: 2,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		TimeoutSeconds:      1,
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(port),
			},
		},
	}
}

// InitGitRepos initializes the specified repositories in the test-git-server
func InitGitRepos(nt *NT, repos ...types.NamespacedName) {
	nt.T.Helper()

	pod, err := nt.KubeClient.GetDeploymentPod(testGitServer, testGitNamespace, nt.DefaultWaitTimeout)
	if err != nil {
		nt.T.Fatal(err)
	}
	podName := pod.Name

	for _, repo := range repos {
		nt.MustKubectl("exec", "-n", testGitNamespace, podName, "-c", testGitServer, "--",
			"git", "init", "--bare", "--shared", fmt.Sprintf("/git-server/repos/%s/%s", repo.Namespace, repo.Name))
		// We set receive.denyNonFastforwards to allow force pushes for legacy test support (bats).  In the future we may
		// need this support for testing GKE clusters since we will likely be re-using the cluster in that case.
		// Alternatively, we could also run "rm -rf /git-server/repos/*" to clear out the state of the git server and
		// re-initialize.
		nt.MustKubectl("exec", "-n", testGitNamespace, podName, "-c", testGitServer, "--",
			"git", "-C", fmt.Sprintf("/git-server/repos/%s", repo), "config", "receive.denyNonFastforwards", "false")
	}
}
