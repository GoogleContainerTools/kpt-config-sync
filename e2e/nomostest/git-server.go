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
	"encoding/json"
	"fmt"

	"k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/pkg/metrics"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const testGitNamespace = "config-management-system-test"
const testGitServer = "test-git-server"
const testGitServerImage = "gcr.io/stolos-dev/git-server:v1.0.0"
const testGitHTTPServer = "http-git-server"
const testGitHTTPServerImage = "gcr.io/stolos-dev/http-git-server:v1.0.0"

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
		err := nt.Create(o)
		if err != nil {
			nt.T.Fatalf("installing %v %s", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()})
		}
	}

	return func() error {
		// In CI, 2% of the time this takes longer than 60 seconds, so 120 seconds
		// seems like a reasonable amount of time to wait before erroring out.
		took, err := Retry(nt.DefaultWaitTimeout, func() error {
			return nt.Validate(testGitServer, testGitNamespace,
				&appsv1.Deployment{}, isAvailableDeployment)
		})
		if err != nil {
			return err
		}
		nt.T.Logf("took %v to wait for git-server to come up", took)
		return nil
	}
}

// isAvailableDeployment ensures all of the passed Deployment's replicas are
// available.
func isAvailableDeployment(o client.Object) error {
	d, ok := o.(*appsv1.Deployment)
	if !ok {
		return WrongTypeErr(o, d)
	}

	// The desired number of replicas defaults to 1 if unspecified.
	var want int32 = 1
	if d.Spec.Replicas != nil {
		want = *d.Spec.Replicas
	}

	available := d.Status.AvailableReplicas
	if available != want {
		// Display the full state of the malfunctioning Deployment to aid in debugging.
		jsn, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			return err
		}
		return fmt.Errorf("%w for deployment/%s in namespace %s: got %d available replicas, want %d\n\n%s",
			ErrFailedPredicate, d.GetName(), d.GetNamespace(), available, want, string(jsn))
	}

	// otel_controller.go:configureGooglecloudConfigMap creates or updates `otel-collector-googlecloud` configmap
	// which causes the deployment to be updated. We need to make sure the updated deployment is available.
	if *e2e.TestCluster == e2e.GKE && d.Name == metrics.OtelCollectorName && d.ObjectMeta.Generation < 2 {
		return fmt.Errorf("deployment/%s in namespace %s should be updated after creation", d.Name, d.Namespace)
	}
	return nil
}

func gitServer() []client.Object {
	// Remember that we've already created the git-server's Namespace since the
	// SSH key must exist before we apply the Deployment.
	return []client.Object{
		gitPodSecurityPolicy(),
		gitRole(),
		gitRoleBinding(),
		gitService(),
		gitDeployment(),
	}
}

func gitNamespace() *corev1.Namespace {
	return fake.NamespaceObject(testGitNamespace)
}

func gitPodSecurityPolicy() *v1beta1.PodSecurityPolicy {
	psp := &v1beta1.PodSecurityPolicy{}
	psp.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy",
		Version: "v1beta1",
		Kind:    "PodSecurityPolicy",
	})
	psp.SetName(testGitServer)
	psp.Spec.Privileged = false
	psp.Spec.Volumes = []v1beta1.FSType{
		"*",
	}
	psp.Spec.RunAsUser.Rule = v1beta1.RunAsUserStrategyRunAsAny
	psp.Spec.SELinux.Rule = v1beta1.SELinuxStrategyRunAsAny
	psp.Spec.SupplementalGroups.Rule = v1beta1.SupplementalGroupsStrategyRunAsAny
	psp.Spec.FSGroup.Rule = v1beta1.FSGroupStrategyRunAsAny
	return psp
}

func gitRole() *rbacv1.Role {
	role := fake.RoleObject(
		core.Name(testGitServer),
		core.Namespace(testGitNamespace),
	)
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{testGitServer},
			Verbs:         []string{"use"},
		},
	}
	return role
}

func gitRoleBinding() *rbacv1.RoleBinding {
	rolebinding := fake.RoleBindingObject(
		core.Name(testGitServer),
		core.Namespace(testGitNamespace),
	)
	rolebinding.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     testGitServer,
	}
	rolebinding.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Namespace: testGitNamespace,
			Name:      "default",
		},
	}
	return rolebinding
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
						Secret: &corev1.SecretVolumeSource{SecretName: gitServerSecret},
					}},
					{Name: "repos", VolumeSource: corev1.VolumeSource{EmptyDir: nil}},
					{Name: "ssl-cert", VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: gitServerCertSecret},
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
					},
					{
						Name:  testGitHTTPServer,
						Image: testGitHTTPServerImage,
						Ports: []corev1.ContainerPort{{ContainerPort: 443}},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "repos", MountPath: "/git-server/repos"},
							{Name: "ssl-cert", MountPath: "/etc/nginx/ssl"},
						},
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

// portForwardGitServer forwards the git-server deployment to a port.
// Returns the localhost port which forwards to the git-server Pod.
func portForwardGitServer(nt *NT, repos ...types.NamespacedName) int {
	nt.T.Helper()

	podName := InitGitRepos(nt, repos...)

	if nt.gitRepoPort == 0 {
		port, err := nt.ForwardToFreePort(testGitNamespace, podName, ":22")
		if err != nil {
			nt.T.Fatal(err)
		}
		return port
	}
	return nt.gitRepoPort
}

// InitGitRepos initializes the repositories in the testing git-server and returns the pod names.
func InitGitRepos(nt *NT, repos ...types.NamespacedName) string {
	// This logic is not robust to the git-server pod being killed/restarted,
	// but this is a rare occurrence.
	// Consider if it is worth getting the Pod name again if port forwarding fails.
	podList := &corev1.PodList{}
	err := nt.List(podList, client.InNamespace(testGitNamespace))
	if err != nil {
		nt.T.Fatal(err)
	}
	if nPods := len(podList.Items); nPods != 1 {
		podsJSON, err := json.MarshalIndent(podList, "", "  ")
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.T.Log(string(podsJSON))
		nt.T.Fatalf("got len(podList.Items) = %d, want 1", nPods)
	}

	podName := podList.Items[0].Name

	for _, repo := range repos {
		nt.MustKubectl("exec", "-n", testGitNamespace, podName, "--",
			"git", "init", "--bare", "--shared", fmt.Sprintf("/git-server/repos/%s/%s", repo.Namespace, repo.Name))
		// We set receive.denyNonFastforwards to allow force pushes for legacy test support (bats).  In the future we may
		// need this support for testing GKE clusters since we will likely be re-using the cluster in that case.
		// Alternatively, we could also run "rm -rf /git-server/repos/*" to clear out the state of the git server and
		// re-initialize.
		nt.MustKubectl("exec", "-n", testGitNamespace, podName, "--",
			"git", "-C", fmt.Sprintf("/git-server/repos/%s", repo), "config", "receive.denyNonFastforwards", "false")
	}
	return podName
}
