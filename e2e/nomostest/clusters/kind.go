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
	"1.33": "kindest/node:v1.33.1@sha256:8d866994839cd096b3590681c55a6fa4a071fdaf33be7b9660e5697d2ed13002",
	"1.32": "kindest/node:v1.32.5@sha256:36187f6c542fa9b78d2d499de4c857249c5a0ac8cc2241bef2ccd92729a7a259",
	"1.31": "kindest/node:v1.31.9@sha256:156da58ab617d0cb4f56bbdb4b493f4dc89725505347a4babde9e9544888bb92",
	"1.30": "kindest/node:v1.30.13@sha256:8673291894dc400e0fb4f57243f5fdc6e355ceaa765505e0e73941aa1b6e0b80",
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
