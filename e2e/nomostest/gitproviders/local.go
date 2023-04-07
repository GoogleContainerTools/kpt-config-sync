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

package gitproviders

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/portforwarder"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testshell"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	LocalGitNamespace = "config-management-system-test"
	// LocalGitServer is the name for the Service, Deployment, Role, RoleBinding, and PodSecurityPolicy
	LocalGitServer             = "test-git-server"
	LocalGitContainerName      = "test-git-server"
	LocalGitContainerImage     = "gcr.io/stolos-dev/git-server:v1.0.0"
	LocalGitHTTPContainerName  = "http-git-server"
	LocalGitHTTPContainerImage = "gcr.io/stolos-dev/http-git-server:v1.0.0"
	LocalGitAppLabelValue      = "test-git-server"
	LocalGitServerSecret       = "ssh-pub"
	LocalGitServerCertSecret   = "ssl-cert"
)

// LocalProvider refers to the test git-server running on the same test cluster.
type LocalProvider struct {
	PortForwarder *portforwarder.PortForwarder
	Shell         *testshell.TestShell
	KubeClient    *testkubeclient.KubeClient
	KubeWatcher   *testwatcher.Watcher
	PSPEnabled    bool
}

// Type returns the provider type.
func (l *LocalProvider) Type() string {
	return e2e.Local
}

// RemoteURL returns the Git URL for connecting to the test git-server.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalProvider) RemoteURL(name string) (string, error) {
	port, err := l.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ssh://git@localhost:%d/git-server/repos/%s", port, name), nil
}

// SyncURL returns a URL for Config Sync to sync from.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalProvider) SyncURL(name string) string {
	return fmt.Sprintf("git@%s.%s:/git-server/repos/%s",
		LocalGitServer, LocalGitNamespace, name)
}

// CreateRepository returns the local name as the remote repo name.
// It is a no-op for the test git-server because all repos are
// initialized at once in git-server.go.
func (l *LocalProvider) CreateRepository(name string) (string, error) {
	return name, nil
}

// DeleteRepositories is a no-op for the test git-server because the git-server
// will be deleted after the test.
func (l *LocalProvider) DeleteRepositories(...string) error {
	return nil
}

// DeleteObsoleteRepos is a no-op for the test git-server because the git-server
// will be deleted after the test.
func (l *LocalProvider) DeleteObsoleteRepos() error {
	return nil
}

// InitRepos initializes the specified repositories in the test-git-server
func (l *LocalProvider) InitRepos(repos []types.NamespacedName, retryTimeout time.Duration) error {
	pod, err := l.KubeClient.GetDeploymentPod(LocalGitServer, LocalGitNamespace, retryTimeout)
	if err != nil {
		return err
	}
	podName := pod.Name

	for _, repo := range repos {
		repoPath := fmt.Sprintf("/git-server/repos/%s", repo)
		_, err := l.Shell.Kubectl("exec", "-n", LocalGitNamespace, podName, "-c", LocalGitContainerName, "--",
			"git", "init", "--bare", "--shared", repoPath)
		if err != nil {
			return errors.Wrapf(err, "failed to init git repo: %s")
		}
		// We set receive.denyNonFastforwards to allow force pushes for legacy test support (bats).  In the future we may
		// need this support for testing GKE clusters since we will likely be re-using the cluster in that case.
		// Alternatively, we could also run "rm -rf /git-server/repos/*" to clear out the state of the git server and
		// re-initialize.
		_, err = l.Shell.Kubectl("exec", "-n", LocalGitNamespace, podName, "-c", LocalGitContainerName, "--",
			"git", "-C", repoPath, "config", "receive.denyNonFastforwards", "false")
		if err != nil {
			return errors.Wrapf(err, "failed to configure denyNonFastforwards on git repo: %s")
		}
	}
	return nil
}

// Install the git-server namespace and wait for it to become available.
func (l *LocalProvider) InstallNamespace() error {
	obj := fake.NamespaceObject(LocalGitNamespace)
	if err := l.KubeClient.Create(obj); err != nil {
		return errors.Wrapf(err, "installing git server namespace: %s", obj.Name)
	}
	err := l.KubeWatcher.WatchForCurrentStatus(kinds.Namespace(), obj.Name,
		obj.Namespace)
	if err != nil {
		return errors.Wrapf(err, "waiting for git server namespace: %s", obj.Name)
	}
	return nil
}

// Install the git-server deployment and wait for it to become available.
func (l *LocalProvider) Install() error {
	for _, obj := range l.serverObjects() {
		if err := l.KubeClient.Create(obj); err != nil {
			return errors.Wrapf(err, "installing git server %T %v %s",
				obj.GetObjectKind().GroupVersionKind().Kind,
				client.ObjectKeyFromObject(obj))
		}
	}
	err := l.KubeWatcher.WatchForCurrentStatus(kinds.Deployment(),
		LocalGitServer, LocalGitNamespace)
	if err != nil {
		return errors.Wrapf(err, "waiting for git server deployment: %s/%s",
			LocalGitServer, LocalGitNamespace)
	}
	return nil
}

func (l *LocalProvider) serverObjects() []client.Object {
	// Remember that we've already created the git-server's Namespace since the
	// SSH key must exist before we apply the Deployment.
	objs := []client.Object{
		gitService(),
		gitDeployment(),
	}
	if l.PSPEnabled {
		objs = append(objs, []client.Object{
			gitPodSecurityPolicy(),
			gitRole(),
			gitRoleBinding(),
		}...)
	}
	return objs
}

func gitPodSecurityPolicy() *v1beta1.PodSecurityPolicy {
	psp := &v1beta1.PodSecurityPolicy{}
	psp.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "policy",
		Version: "v1beta1",
		Kind:    "PodSecurityPolicy",
	})
	psp.SetName(LocalGitServer)
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
		core.Name(LocalGitServer),
		core.Namespace(LocalGitNamespace),
	)
	role.Rules = []rbacv1.PolicyRule{
		{
			APIGroups:     []string{"policy"},
			Resources:     []string{"podsecuritypolicies"},
			ResourceNames: []string{LocalGitServer},
			Verbs:         []string{"use"},
		},
	}
	return role
}

func gitRoleBinding() *rbacv1.RoleBinding {
	rolebinding := fake.RoleBindingObject(
		core.Name(LocalGitServer),
		core.Namespace(LocalGitNamespace),
	)
	rolebinding.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     LocalGitServer,
	}
	rolebinding.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Namespace: LocalGitNamespace,
			Name:      "default",
		},
	}
	return rolebinding
}

func gitService() *corev1.Service {
	service := fake.ServiceObject(
		core.Name(LocalGitServer),
		core.Namespace(LocalGitNamespace),
	)
	service.Spec.Selector = testGitServerSelector()
	service.Spec.Ports = []corev1.ServicePort{{Name: "ssh", Port: 22},
		{Name: "https", Port: 443}}
	return service
}

func gitDeployment() *appsv1.Deployment {
	deployment := fake.DeploymentObject(core.Name(LocalGitServer),
		core.Namespace(LocalGitNamespace),
		core.Labels(testGitServerSelector()),
	)
	gitGID := int64(1000)
	deployment.Spec = appsv1.DeploymentSpec{
		MinReadySeconds: 2,
		Strategy:        appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector:        &metav1.LabelSelector{MatchLabels: testGitServerSelector()},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: testGitServerSelector(),
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{Name: "keys", VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: LocalGitServerSecret},
					}},
					{Name: "repos", VolumeSource: corev1.VolumeSource{EmptyDir: nil}},
					{Name: "ssl-cert", VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{SecretName: LocalGitServerCertSecret},
					}},
				},
				Containers: []corev1.Container{
					{
						Name:  LocalGitContainerName,
						Image: LocalGitContainerImage,
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
						Name:  LocalGitHTTPContainerName,
						Image: LocalGitHTTPContainerImage,
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

func testGitServerSelector() map[string]string {
	// Note that maps are copied by reference into objects.
	// If this were just a variable, then concurrent usages by Clients may result
	// in concurrent map writes (and thus flaky test panics).
	return map[string]string{"app": LocalGitAppLabelValue}
}
