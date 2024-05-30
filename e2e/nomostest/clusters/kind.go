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

package clusters

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"kpt.dev/configsync/e2e/nomostest/docker"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// KindVersion is a specific Kind version associated with a Kubernetes minor version.
type KindVersion string

const (
	// Kubeconfig is the filename of the KUBECONFIG file.
	Kubeconfig = "KUBECONFIG"

	// maxKindTries is the number of times to attempt to create a Kind cluster for
	// a single test.
	maxKindTries = 6
)

// When upgrading KinD, find the node images from the release at https://github.com/kubernetes-sigs/kind/releases
// Update this mapping accordingly.
var kindNodeImages = map[string]KindVersion{
	"1.27": "kindest/node:v1.27.3@sha256:3966ac761ae0136263ffdb6cfd4db23ef8a83cba8a463690e98317add2c9ba72",
	"1.26": "kindest/node:v1.26.6@sha256:6e2d8b28a5b601defe327b98bd1c2d1930b49e5d8c512e1895099e4504007adb",
	"1.25": "kindest/node:v1.25.11@sha256:227fa11ce74ea76a0474eeefb84cb75d8dad1b08638371ecf0e86259b35be0c8",
	"1.24": "kindest/node:v1.24.15@sha256:7db4f8bea3e14b82d12e044e25e34bd53754b7f2b0e9d56df21774e6f66a70ab",
	"1.23": "kindest/node:v1.23.17@sha256:59c989ff8a517a93127d4a536e7014d28e235fb3529d9fba91b3951d461edfdb",
	"1.22": "kindest/node:v1.22.17@sha256:f5b2e5698c6c9d6d0adc419c0deae21a425c07d81bbf3b6a6834042f25d4fba2",
	"1.21": "kindest/node:v1.21.14@sha256:8a4e9bb3f415d2bb81629ce33ef9c76ba514c14d707f9797a01e3216376ba093",
}

// KindCluster is a kind cluster for use in the e2e tests
type KindCluster struct {
	// T is a testing interface
	T testing.NTB
	// Name is the name of the cluster
	Name string
	// KubeConfigPath is the path to save the kube config
	KubeConfigPath string
	// TmpDir is the temporary directory for this test
	TmpDir string
	// KubernetesVersion is the version to use when creating the kind cluster
	KubernetesVersion  string
	creationSuccessful bool
	provider           *cluster.Provider
}

// Exists returns whether the KinD cluster exists
func (c *KindCluster) Exists() (bool, error) {
	c.initProvider()
	kindClusters, err := c.provider.List()
	if err != nil {
		return false, err
	}
	for _, kindCluster := range kindClusters {
		if kindCluster == c.Name {
			return true, nil
		}
	}
	return false, nil
}

func (c *KindCluster) initProvider() {
	if c.provider == nil {
		c.provider = cluster.NewProvider()
	}
}

// Create the kind cluster
func (c *KindCluster) Create() error {
	c.initProvider()
	version, err := asKindVersion(c.KubernetesVersion)
	if err != nil {
		return err
	}
	err = createKindCluster(c.provider, c.Name, c.KubeConfigPath, version)
	if err == nil {
		c.creationSuccessful = true
	} else {
		c.creationSuccessful = false
	}
	return err
}

// Delete the kind cluster
func (c *KindCluster) Delete() error {
	c.initProvider()
	if !c.creationSuccessful {
		// Since we have set retain=true, the cluster is still available even
		// though creation did not execute successfully.
		artifactsDir := os.Getenv("ARTIFACTS")
		if artifactsDir == "" {
			artifactsDir = filepath.Join(c.TmpDir, "artifacts")
		}
		c.T.Logf("exporting failed cluster logs to %s", artifactsDir)
		err := exec.Command("kind", "export", "logs", "--name", c.Name, artifactsDir).Run()
		if err != nil {
			c.T.Errorf("exporting kind logs: %v", err)
		}
	}

	// If the test runner stops testing with a command like ^C, cleanup
	// callbacks such as this are not executed.
	err := c.provider.Delete(c.Name, c.KubeConfigPath)
	if err != nil {
		return fmt.Errorf("deleting Kind cluster %q: %v", c.Name, err)
	}
	return nil
}

// Connect to the kind cluster
func (c *KindCluster) Connect() error {
	c.initProvider()
	return c.provider.ExportKubeConfig(c.Name, c.KubeConfigPath, false)
}

// Hash returns N/A for kind cluster
func (c *KindCluster) Hash() (string, error) {
	return "N/A for KinD cluster", nil
}

// asKindVersion returns the latest Kind version associated with a given
// Kubernetes minor version.
func asKindVersion(version string) (KindVersion, error) {
	kindVersion, ok := kindNodeImages[version]
	if ok {
		return kindVersion, nil
	}
	return "", fmt.Errorf("Unrecognized Kind version: %q", version)
}

func createKindCluster(p *cluster.Provider, name, kcfgPath string, version KindVersion) error {
	var err error
	for i := 0; i < maxKindTries; i++ {
		if i > 0 {
			// This isn't the first time we're executing this loop.
			// We've tried creating the cluster before but got an error. Since we set
			// retain=true, the cluster still exists in a problematic state. We must
			// delete it before retrying.
			err = p.Delete(name, kcfgPath)
			if err != nil {
				return err
			}
		}

		err = p.Create(name,
			// Use the specified version per --kubernetes-version.
			cluster.CreateWithNodeImage(string(version)),
			// Store the KUBECONFIG at the specified path.
			cluster.CreateWithKubeconfigPath(kcfgPath),
			// Allow the cluster to see the local Docker container registry.
			// https://kind.sigs.k8s.io/docs/user/local-registry/
			cluster.CreateWithV1Alpha4Config(&v1alpha4.Cluster{
				ContainerdConfigPatches: []string{
					fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:%d"]
  endpoint = ["http://%s:%d"]`, docker.RegistryPort, docker.RegistryName, docker.RegistryPort),
				},
				// Enable ValidatingAdmissionWebhooks in the Kind cluster, as these
				// are disabled by default.
				KubeadmConfigPatches: []string{
					`
apiVersion: kubeadm.k8s.io/v1beta2
kind: ClusterConfiguration
metadata:
  name: config
apiServer:
  extraArgs:
    "enable-admission-plugins": "ValidatingAdmissionWebhook"`,
				},
			}),
			// Retain nodes for debugging logs.
			cluster.CreateWithRetain(true),
			// Wait for cluster to be ready before proceeding
			cluster.CreateWithWaitForReady(10*time.Minute),
		)
		if err == nil {
			return nil
		}
	}

	// We failed to create the cluster maxKindTries times, so fail out.
	return err
}
