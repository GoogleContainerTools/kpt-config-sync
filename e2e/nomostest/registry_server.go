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

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RegistryUsername      = "user"
	RegistryPassword      = "password"
	RegistryPort          = 5000
	RegistryAuthPort      = 5001
	TestRegistryServer    = "test-registry-server"
	TestRegistryNamespace = testGitNamespace
	nginxConfigMapName    = "nginx-cm"
	nginxConf             = "nginx.conf"
	RegistryDomain        = TestRegistryServer + "." + TestRegistryNamespace
)

type RegistryServer struct {
	nt            *NT
	forwardedPort int
}

// Install installs an in-cluster registry for testing.
func (rs *RegistryServer) Install() {
	nt := rs.nt
	nt.T.Helper()

	if err := nt.Create(gitNamespace()); err != nil && !apierrors.IsAlreadyExists(err) {
		nt.T.Fatal(err)
	}

	objs := registryServer()

	for _, o := range objs {
		err := nt.Create(o)
		if err != nil {
			nt.T.Fatalf("installing %v %s: %v", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}
}

// WaitForReady waits for the register-server deployment to be ready.
func (rs *RegistryServer) WaitForReady() {
	nt := rs.nt
	nt.T.Helper()
	took, err := Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate(TestRegistryServer, TestRegistryNamespace,
			&appsv1.Deployment{}, isAvailableDeployment)
	})
	if err != nil {
		nt.T.Fatalf("waiting for registry-server deployment: %s", err)
	}
	nt.T.Logf("took %v to wait for %s to come up", took, TestRegistryServer)
}

// PortForward forwards the registry-server deployment to a port.
// Returns the localhost port which forwards to the registry-server Pod.
func (rs *RegistryServer) PortForward() int {
	nt := rs.nt
	nt.T.Helper()
	if rs.forwardedPort > 0 {
		return rs.forwardedPort
	}

	podName := getPodName(nt,
		client.InNamespace(TestRegistryNamespace),
		client.MatchingLabels(testRegistryServerSelector()),
	)
	port, err := nt.ForwardToFreePort(TestRegistryNamespace, podName, fmt.Sprintf(":%d", RegistryPort))
	if err != nil {
		nt.T.Fatal(err)
	}
	rs.forwardedPort = port
	nt.T.Logf("Forwarded %s to localhost:%d", TestRegistryServer, port)
	return rs.forwardedPort
}

// HelmSyncURL returns a Repo URL for the in-cluster registry
func HelmSyncURL(nn types.NamespacedName, withAuth bool) string {
	port := RegistryPort
	if withAuth {
		port = RegistryAuthPort
	}
	return fmt.Sprintf("oci://%s:%d/%s/%s", RegistryDomain, port, "helm", nn.String())
}

func testRegistryServerSelector() map[string]string {
	return map[string]string{"app": TestRegistryServer}
}

func registryServer() []client.Object {
	objs := []client.Object{
		registryService(),
		nginxConfigMap(),
		registryDeployment(),
	}
	return objs
}

func registryService() *v1.Service {
	service := fake.ServiceObject(
		core.Name(TestRegistryServer),
		core.Namespace(TestRegistryNamespace),
	)
	service.Spec.Selector = testRegistryServerSelector()
	service.Spec.Ports = []v1.ServicePort{
		{Name: "http", Port: RegistryPort},
		{Name: "http-authenticated", Port: RegistryAuthPort},
	}
	return service
}

func nginxConfigMap() *v1.ConfigMap {
	configMap := fake.ConfigMapObject(core.Name(nginxConfigMapName),
		core.Namespace(TestRegistryNamespace),
		core.Labels(testRegistryServerSelector()),
	)
	configMap.Data = map[string]string{
		nginxConf: nginxConfig,
	}
	return configMap
}

func registryDeployment() *appsv1.Deployment {
	deployment := fake.DeploymentObject(core.Name(TestRegistryServer),
		core.Namespace(TestRegistryNamespace),
		core.Labels(testRegistryServerSelector()),
	)
	const credVolume = "cred"
	const nginxConfVolume = "nginx-conf"
	const authDir = "/auth"
	const htpasswdFile = "/auth/nginx.htpasswd"

	deployment.Spec = appsv1.DeploymentSpec{
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector: &metav1.LabelSelector{MatchLabels: testRegistryServerSelector()},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: testRegistryServerSelector(),
			},
			Spec: v1.PodSpec{
				Volumes: []v1.Volume{
					{ // Volume for storing nginx config
						Name: nginxConfVolume,
						VolumeSource: v1.VolumeSource{
							ConfigMap: &v1.ConfigMapVolumeSource{
								LocalObjectReference: v1.LocalObjectReference{
									Name: nginxConfigMapName,
								},
							},
						},
					},
					{ // Volume for storing credentials
						Name: credVolume,
						VolumeSource: v1.VolumeSource{
							EmptyDir: &v1.EmptyDirVolumeSource{},
						},
					},
				},
				InitContainers: []v1.Container{
					{ // bootstrap credentials for e2e testing usage
						Image:   "httpd:2",
						Name:    "httpd",
						Command: []string{"htpasswd"},
						Args:    []string{"-Bbc", htpasswdFile, RegistryUsername, RegistryPassword},
						VolumeMounts: []v1.VolumeMount{
							{Name: credVolume, MountPath: authDir},
						},
					},
				},
				Containers: []v1.Container{
					{ // nginx reverse proxy for testing with token auth (username/password)
						Image: "nginx:1.23",
						Name:  "nginx",
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      nginxConfVolume,
								MountPath: "/etc/nginx/nginx.conf",
								SubPath:   nginxConf,
								ReadOnly:  true,
							},
							{
								Name:      credVolume,
								MountPath: "/etc/nginx/conf.d/nginx.htpasswd",
								SubPath:   "nginx.htpasswd",
								ReadOnly:  true,
							},
						},
						Ports: []v1.ContainerPort{
							{ContainerPort: RegistryAuthPort},
						},
					},
					{ // registry server. See https://docs.docker.com/registry/deploying/
						Image: "registry:2",
						Name:  "registry",
						Ports: []v1.ContainerPort{ // expose port for pushing images and public registry testing
							{ContainerPort: RegistryPort},
						},
					},
				},
			},
		},
	}
	return deployment
}

func getPodName(nt *NT, opts ...client.ListOption) string {
	podList := &v1.PodList{}
	err := nt.List(podList, opts...)
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
	return podName
}

// See https://docs.docker.com/registry/recipes/nginx/
// Uses http rather than https since CA Cert is unsupported by helm-sync/oci-sync
const nginxConfig = `
events {}
http {
	upstream docker-registry {
		server localhost:5000;
	}
	map $upstream_http_docker_distribution_api_version $docker_distribution_api_version {
		'' 'registry/2.0';
	}
	# http server with basic auth
	server {
		listen 5001;
		client_max_body_size 0;
		chunked_transfer_encoding on;
		location /v2/ {
			if ($http_user_agent ~ "^(docker\/1\.(3|4|5(?!\.[0-9]-dev))|Go ).*$" ) {
				return 404;
			}
			auth_basic "Registry realm";
			auth_basic_user_file /etc/nginx/conf.d/nginx.htpasswd;
			add_header 'Docker-Distribution-Api-Version' $docker_distribution_api_version always;
			proxy_pass                          http://docker-registry;
			proxy_set_header  Host              $http_host;   # required for docker client's sake
			proxy_set_header  X-Real-IP         $remote_addr; # pass on real client's IP
			proxy_set_header  X-Forwarded-For   $proxy_add_x_forwarded_for;
			proxy_set_header  X-Forwarded-Proto $scheme;
			proxy_read_timeout                  900;
		}
	}
}
`
