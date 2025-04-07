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
	"sync"
	"time"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/docker"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
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
	"1.30": "kindest/node:v1.30.0@sha256:047357ac0cfea04663786a612ba1eaba9702bef25227a794b52890dd8bcd692e",
	"1.29": "kindest/node:v1.29.4@sha256:3abb816a5b1061fb15c6e9e60856ec40d56b7b52bcea5f5f1350bc6e2320b6f8",
	"1.28": "kindest/node:v1.28.9@sha256:dca54bc6a6079dd34699d53d7d4ffa2e853e46a20cd12d619a09207e35300bd0",
	"1.27": "kindest/node:v1.27.13@sha256:17439fa5b32290e3ead39ead1250dca1d822d94a10d26f1981756cd51b24b9d8",
	"1.26": "kindest/node:v1.26.15@sha256:84333e26cae1d70361bb7339efb568df1871419f2019c80f9a12b7e2d485fe19",
	"1.25": "kindest/node:v1.25.16@sha256:5da57dfc290ac3599e775e63b8b6c49c0c85d3fec771cd7d55b45fae14b38d3b",
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
	tg := taskgroup.New()
	tg.Go(func() error {
		return createKindCluster(c.provider, c.Name, c.KubeConfigPath, version)
	})
	tg.Go(func() error {
		return pullImages()
	})
	if err := tg.Wait(); err != nil {
		return err
	}
	c.creationSuccessful = true
	return sideLoadImages(c.Name)
}

// Delete the kind cluster
func (c *KindCluster) Delete() error {
	c.initProvider()
	if !c.creationSuccessful || *e2e.Debug || c.T.Failed() {
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
	return "", fmt.Errorf("unrecognized Kind version: %q", version)
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
				// Also mount etcd to tmpfs for memory-backed storage.
				KubeadmConfigPatches: []string{
					`
kind: ClusterConfiguration
etcd:
  local:
    dataDir: /tmp/etcd
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

var preloadImages = []string{
	testing.HTTPDImage,
	testing.RegistryImage,
	testing.NginxImage,
	testing.PrometheusImage,
}

// Pull each public image a single time on the host, and then sideload the images
// into each kind cluster. This reduces the number of times each image is pulled
// to avoid 429 too many requests errors from public docker registries.
// Alternatively we may choose to host these images in a registry we control.
func sideLoadImages(clusterName string) error {
	for _, image := range preloadImages {
		out, err := exec.Command("kind", "load", "docker-image", image, "--name", clusterName).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to sideload image %s: %w\n%s", image, err, out)
		}
	}
	return nil
}

var imagesPulled = false
var imagePullMux sync.Mutex

func pullImages() error {
	imagePullMux.Lock()
	defer imagePullMux.Unlock()

	if imagesPulled {
		return nil
	}

	for _, image := range preloadImages {
		out, err := exec.Command("docker", "pull", image).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to pull image %s: %w\n%s", image, err, out)
		}
	}
	imagesPulled = true
	return nil
}
