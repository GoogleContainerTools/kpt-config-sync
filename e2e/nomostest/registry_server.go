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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// RegistryUsername is a contrived username used for authenticating to the in-cluster registry
	RegistryUsername = "user"
	// RegistryPassword is a contrived password used for authenticating to the in-cluster registry
	RegistryPassword = "password"
	// RegistryHTTPPort is an HTTP port surfaced on the in-cluster registry.
	RegistryHTTPPort = 5000
	// RegistryHTTPSPort is an HTTPS port surfaced on the in-cluster registry. The HTTPS
	// certificate is signed using a contrived CA.
	RegistryHTTPSPort = 5001
	// RegistryHTTPSAuthPort is an HTTPS port surfaced on the in-cluster registry with
	// basic authentication enabled. The HTTPS certificate is signed using a contrived CA.
	RegistryHTTPSAuthPort = 5002
	// TestRegistryServer is the name of the in-cluster registry Deployment and Service
	TestRegistryServer = "test-registry-server"
	// TestRegistryServerAuthenticated is the name of the in-cluster registry Service
	// which requires basic auth. This endpoint can be used as a sync URL when writing
	// tests to verify basic auth.
	TestRegistryServerAuthenticated = "test-registry-server-auth"
	// TestRegistryNamespace is the namespace used for the in-cluster registry
	TestRegistryNamespace = "test-registry-system"
	nginxConfigMapName    = "nginx-cm"
	nginxConf             = "nginx.conf"
)

func setupRegistry(nt *NT) error {
	nt.T.Cleanup(func() {
		if err := nt.HelmProvider.Teardown(); err != nil {
			nt.T.Error(err)
		}
		if err := nt.OCIProvider.Teardown(); err != nil {
			nt.T.Error(err)
		}
	})
	if err := nt.OCIProvider.Setup(); err != nil {
		return err
	}
	if err := nt.HelmProvider.Setup(); err != nil {
		return err
	}
	if *e2e.OCIProvider == e2e.Local || *e2e.HelmProvider == e2e.Local {
		if err := nt.KubeClient.Create(k8sobjects.NamespaceObject(TestRegistryNamespace)); err != nil {
			return err
		}

		registryDomains := []string{
			fmt.Sprintf("%s.%s", TestRegistryServer, TestRegistryNamespace),
			fmt.Sprintf("%s.%s", TestRegistryServerAuthenticated, TestRegistryNamespace),
		}
		ociCACertPath, err := generateSSLKeys(nt, RegistrySyncSource, TestRegistryNamespace, registryDomains)
		if err != nil {
			return err
		}
		nt.registryCACertPath = ociCACertPath

		if err := installRegistryServer(nt); err != nil {
			nt.describeNotRunningTestPods(TestRegistryNamespace)
			return fmt.Errorf("waiting for registry-server Deployment to become available: %w", err)
		}
	}
	return nil
}

// setupRegistryClient handles registry authentication and port-forwarding.
//
// Port-forwarding is configured for local registries so that if the registry
// pod crashes or is rescheduled the port-forwarding will be restarted after it
// becomes healthy again. Then the client will re-login and re-push all the
// images and charts that were previously pushed during the same test.
//
// For non-local registries, only login, logout, and cleanup are performed.
//
// Helm provider is only configured if RequireHelmProvider is true.
// OCI provider is only configured if RequireOCIProvider is true.
func setupRegistryClient(nt *NT, opts *ntopts.New) error {
	// For local registries, set up port-forwarding to the in-cluster registry server.
	// portForwardRegistryServer handles login and logout too.
	if opts.RequireOCIProvider && *e2e.OCIProvider == e2e.Local || opts.RequireHelmProvider && *e2e.HelmProvider == e2e.Local {
		nt.portForwardRegistryServer(opts.RequireHelmProvider, opts.RequireOCIProvider)
	}
	// Login before each test, then reset the registry and logout after the test.
	if opts.RequireOCIProvider {
		if err := nt.OCIProvider.Login(); err != nil {
			return err
		}
		nt.T.Cleanup(func() {
			if err := nt.OCIProvider.Reset(); err != nil {
				nt.T.Error(err)
			}
			if err := nt.OCIProvider.Logout(); err != nil {
				nt.T.Error(err)
			}
		})
	}
	if opts.RequireHelmProvider {
		if err := nt.HelmProvider.Login(); err != nil {
			return err
		}
		nt.T.Cleanup(func() {
			if err := nt.HelmProvider.Reset(); err != nil {
				nt.T.Error(err)
			}
			if err := nt.HelmProvider.Logout(); err != nil {
				nt.T.Error(err)
			}
		})
	}
	return nil
}

// installRegistryServer installs the registry-server Deployment and waits for it to
// become ready.
func installRegistryServer(nt *NT) error {
	objs := registryServer()

	for _, o := range objs {
		if err := nt.KubeClient.Apply(o); err != nil {
			return fmt.Errorf("applying %v %s: %w", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), TestRegistryServer, TestRegistryNamespace)
}

// BuildAndPushOCIImage uses the current file system state of the provided Repository
// to build an OCI image and push it to the current OCIProvider. The resulting
// OCIImage object can be used to set the spec.oci.image field on the RSync.
func (nt *NT) BuildAndPushOCIImage(rsRef types.NamespacedName, options ...registryproviders.ImageOption) (*registryproviders.OCIImage, error) {
	// Construct artifactDir using TmpDir. TmpDir is scoped to each test case and
	// cleaned up after the test.
	artifactDir := filepath.Join(nt.TmpDir, "artifacts", "oci", rsRef.Namespace, rsRef.Name)
	if err := os.MkdirAll(artifactDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("creating artifact dir: %w", err)
	}
	image, err := registryproviders.BuildImage(artifactDir, nt.Shell, nt.OCIProvider, rsRef, options...)
	if err != nil {
		return nil, err
	}
	image, err = nt.OCIProvider.PushImage(image.Name, image.Tag, image.LocalSourceTgzPath)
	if err != nil {
		return nil, err
	}
	return image, nil
}

// BuildAndPushHelmPackage uses the current file system state of the provided Repository
// to build a helm package and push it to the current HelmProvider. The resulting
// HelmPackage object can be used to set the spec.oci.image field on the RSync.
func (nt *NT) BuildAndPushHelmPackage(rsRef types.NamespacedName, options ...registryproviders.HelmOption) (*registryproviders.HelmPackage, error) {
	// Construct artifactDir using TmpDir. TmpDir is scoped to each test case and
	// cleaned up after the test.
	artifactDir := filepath.Join(nt.TmpDir, "artifacts", "helm", rsRef.Namespace, rsRef.Name)
	if err := os.MkdirAll(artifactDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("creating artifact dir: %w", err)
	}
	pkg, err := registryproviders.BuildHelmPackage(artifactDir, nt.HelmProvider, rsRef, options...)
	if err != nil {
		return nil, err
	}
	pkg, err = nt.HelmProvider.PushPackage(pkg.LocalChartTgzPath)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

func testRegistryServerSelector() map[string]string {
	return map[string]string{"app": TestRegistryServer}
}

func registryServer() []client.Object {
	objs := []client.Object{
		registryService(),
		registryServiceAuthenticated(),
		nginxConfigMap(),
		registryDeployment(),
	}
	return objs
}

// This service exposes a port which maps to the nginx endpoint which uses HTTPS
// but does not require authentication
func registryService() *corev1.Service {
	service := k8sobjects.ServiceObject(
		core.Name(TestRegistryServer),
		core.Namespace(TestRegistryNamespace),
	)
	service.Spec.Selector = testRegistryServerSelector()
	service.Spec.Ports = []corev1.ServicePort{
		{Name: "https", Port: 443, TargetPort: intstr.FromInt(RegistryHTTPSPort)},
	}
	return service
}

// This service exposes a port which maps to the nginx endpoint which requires
// authentication (using contrived username/password)
func registryServiceAuthenticated() *corev1.Service {
	service := k8sobjects.ServiceObject(
		core.Name(TestRegistryServerAuthenticated),
		core.Namespace(TestRegistryNamespace),
	)
	service.Spec.Selector = testRegistryServerSelector()
	service.Spec.Ports = []corev1.ServicePort{
		{Name: "https", Port: 443, TargetPort: intstr.FromInt(RegistryHTTPSAuthPort)},
	}
	return service
}

func nginxConfigMap() *corev1.ConfigMap {
	configMap := k8sobjects.ConfigMapObject(core.Name(nginxConfigMapName),
		core.Namespace(TestRegistryNamespace),
		core.Labels(testRegistryServerSelector()),
	)
	configMap.Data = map[string]string{
		nginxConf: nginxConfig,
	}
	return configMap
}

func registryDeployment() *appsv1.Deployment {
	deployment := k8sobjects.DeploymentObject(core.Name(TestRegistryServer),
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
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: testRegistryServerSelector(),
				Annotations: map[string]string{
					safeToEvictAnnotation: "false",
				},
			},
			Spec: corev1.PodSpec{
				Volumes: []corev1.Volume{
					{ // Volume for storing nginx config
						Name: nginxConfVolume,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: nginxConfigMapName,
								},
							},
						},
					},
					{ // Volume for storing credentials
						Name: credVolume,
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
					{ // Volume for storing CA cert
						Name: "ssl-cert",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: privateCertSecretName(RegistrySyncSource)},
						},
					},
				},
				InitContainers: []corev1.Container{
					{ // bootstrap credentials for e2e testing usage
						Image:   testing.HTTPDImage,
						Name:    "httpd",
						Command: []string{"htpasswd"},
						// use contrived username/password for testing
						Args: []string{"-Bbc", htpasswdFile, RegistryUsername, RegistryPassword},
						VolumeMounts: []corev1.VolumeMount{
							{Name: credVolume, MountPath: authDir},
						},
					},
				},
				Containers: []corev1.Container{
					{ // nginx reverse proxy for testing with token auth (username/password)
						Image: testing.NginxImage,
						Name:  "nginx",
						VolumeMounts: []corev1.VolumeMount{
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
							{Name: "ssl-cert", MountPath: "/etc/nginx/ssl"},
						},
						Ports: []corev1.ContainerPort{
							{ContainerPort: RegistryHTTPSPort},
							{ContainerPort: RegistryHTTPSAuthPort},
						},
					},
					{ // registry server. See https://distribution.github.io/distribution/about/deploying/
						Image: testing.RegistryImage,
						Name:  "registry",
						Ports: []corev1.ContainerPort{ // expose port for pushing images and public registry testing
							{ContainerPort: RegistryHTTPPort},
						},
						Env: []corev1.EnvVar{
							{ // Enable deleting image blobs
								Name:  "REGISTRY_STORAGE_DELETE_ENABLED",
								Value: "true",
							},
						},
					},
				},
			},
		},
	}
	return deployment
}

// See https://distribution.github.io/distribution/recipes/nginx/
const nginxConfig = `
events {
  worker_connections  1024;
}

http {

	upstream docker-registry {
		server localhost:5000;
	}

  ## Set a variable to help us decide if we need to add the
  ## 'Docker-Distribution-Api-Version' header.
  ## The registry always sets this header.
  ## In the case of nginx performing auth, the header is unset
  ## since nginx is auth-ing before proxying.
	map $upstream_http_docker_distribution_api_version $docker_distribution_api_version {
		'' 'registry/2.0';
	}

  # HTTPS endpoint which uses the generated CA for e2e testing. Does not require auth.
	server {
		listen 5001 ssl;

    # SSL
    ssl_certificate /etc/nginx/ssl/server.crt;
    ssl_certificate_key /etc/nginx/ssl/server.key;

    # Recommendations from https://raymii.org/s/tutorials/Strong_SSL_Security_On_nginx.html
    ssl_protocols TLSv1.1 TLSv1.2;
    ssl_ciphers 'EECDH+AESGCM:EDH+AESGCM:AES256+EECDH:AES256+EDH';
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;

    # disable any limits to avoid HTTP 413 for large image uploads
    client_max_body_size 0;

    # required to avoid HTTP 411: see Issue #1486 (https://github.com/moby/moby/issues/1486)
    chunked_transfer_encoding on;
		location /v2/ {
      # Do not allow connections from docker 1.5 and earlier
      # docker pre-1.6.0 did not properly set the user agent on ping, catch "Go *" user agents
			if ($http_user_agent ~ "^(docker\/1\.(3|4|5(?!\.[0-9]-dev))|Go ).*$" ) {
				return 404;
			}

      ## If $docker_distribution_api_version is empty, the header is not added.
      ## See the map directive above where this variable is defined.
			add_header 'Docker-Distribution-Api-Version' $docker_distribution_api_version always;

			proxy_pass                          http://docker-registry;
			proxy_set_header  Host              $http_host;   # required for docker client's sake
			proxy_set_header  X-Real-IP         $remote_addr; # pass on real client's IP
			proxy_set_header  X-Forwarded-For   $proxy_add_x_forwarded_for;
			proxy_set_header  X-Forwarded-Proto $scheme;
			proxy_read_timeout                  900;
		}
	}

  # HTTPS endpoint which uses the generated CA for e2e testing as well as basic auth (username+password)
  # This endpoint can be used to write e2e tests which validate basic auth.
	server {
		listen 5002 ssl;

    # SSL
    ssl_certificate /etc/nginx/ssl/server.crt;
    ssl_certificate_key /etc/nginx/ssl/server.key;

    # Recommendations from https://raymii.org/s/tutorials/Strong_SSL_Security_On_nginx.html
    ssl_protocols TLSv1.1 TLSv1.2;
    ssl_ciphers 'EECDH+AESGCM:EDH+AESGCM:AES256+EECDH:AES256+EDH';
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;

    # disable any limits to avoid HTTP 413 for large image uploads
    client_max_body_size 0;

    # required to avoid HTTP 411: see Issue #1486 (https://github.com/moby/moby/issues/1486)
    chunked_transfer_encoding on;
		location /v2/ {
      # Do not allow connections from docker 1.5 and earlier
      # docker pre-1.6.0 did not properly set the user agent on ping, catch "Go *" user agents
			if ($http_user_agent ~ "^(docker\/1\.(3|4|5(?!\.[0-9]-dev))|Go ).*$" ) {
				return 404;
			}
			# To add basic authentication to v2 use auth_basic setting.
			auth_basic "Registry realm";
			auth_basic_user_file /etc/nginx/conf.d/nginx.htpasswd;

      ## If $docker_distribution_api_version is empty, the header is not added.
      ## See the map directive above where this variable is defined.
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
