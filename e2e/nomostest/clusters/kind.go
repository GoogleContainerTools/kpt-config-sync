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

	"github.com/pkg/errors"
	"kpt.dev/configsync/e2e/nomostest/docker"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
)

// KindVersion is a specific Kind version associated with a Kubernetes minor version.
type KindVersion string

// kind v0.14.x supports k8s 1.18-1.24 - images from https://github.com/kubernetes-sigs/kind/releases/tag/v0.14.0
// kind v0.12.x supports k8s 1.14-1.23 - images from https://github.com/kubernetes-sigs/kind/releases/tag/v0.12.0
const (
	Kind1_24 KindVersion = "kindest/node:v1.24.0@sha256:0866296e693efe1fed79d5e6c7af8df71fc73ae45e3679af05342239cdc5bc8e"
	Kind1_23 KindVersion = "kindest/node:v1.23.6@sha256:b1fa224cc6c7ff32455e0b1fd9cbfd3d3bc87ecaa8fcb06961ed1afb3db0f9ae"
	Kind1_22 KindVersion = "kindest/node:v1.22.9@sha256:8135260b959dfe320206eb36b3aeda9cffcb262f4b44cda6b33f7bb73f453105"
	Kind1_21 KindVersion = "kindest/node:v1.21.12@sha256:f316b33dd88f8196379f38feb80545ef3ed44d9197dca1bfd48bcb1583210207"
	Kind1_20 KindVersion = "kindest/node:v1.20.15@sha256:6f2d011dffe182bad80b85f6c00e8ca9d86b5b8922cdf433d53575c4c5212248"
	Kind1_19 KindVersion = "kindest/node:v1.19.16@sha256:d9c819e8668de8d5030708e484a9fdff44d95ec4675d136ef0a0a584e587f65c"
	Kind1_18 KindVersion = "kindest/node:v1.18.20@sha256:738cdc23ed4be6cc0b7ea277a2ebcc454c8373d7d8fb991a7fcdbd126188e6d7"
	Kind1_17 KindVersion = "kindest/node:v1.17.17@sha256:e477ee64df5731aa4ef4deabbafc34e8d9a686b49178f726563598344a3898d5"
	Kind1_16 KindVersion = "kindest/node:v1.16.15@sha256:64bac16b83b6adfd04ea3fbcf6c9b5b893277120f2b2cbf9f5fa3e5d4c2260cc"
	Kind1_15 KindVersion = "kindest/node:v1.15.12@sha256:9dfc13db6d3fd5e5b275f8c4657ee6a62ef9cb405546664f2de2eabcfd6db778"
	Kind1_14 KindVersion = "kindest/node:v1.14.10@sha256:b693339da2a927949025869425e20daf80111ccabf020d4021a23c00bae29d82"

	// Kubeconfig is the filename of the KUBECONFIG file.
	Kubeconfig = "KUBECONFIG"

	// maxKindTries is the number of times to attempt to create a Kind cluster for
	// a single test.
	maxKindTries = 6
)

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

// Create the kind cluster
func (c *KindCluster) Create() error {
	c.provider = cluster.NewProvider()

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
		return errors.Errorf("deleting Kind cluster %q: %v", c.Name, err)
	}
	return nil
}

// Connect to the kind cluster
func (c *KindCluster) Connect() error {
	return c.provider.ExportKubeConfig(c.Name, c.KubeConfigPath, false)
}

// asKindVersion returns the latest Kind version associated with a given
// Kubernetes minor version.
func asKindVersion(version string) (KindVersion, error) {
	switch version {
	case "1.14":
		return Kind1_14, nil
	case "1.15":
		return Kind1_15, nil
	case "1.16":
		return Kind1_16, nil
	case "1.17":
		return Kind1_17, nil
	case "1.18":
		return Kind1_18, nil
	case "1.19":
		return Kind1_19, nil
	case "1.20":
		return Kind1_20, nil
	case "1.21":
		return Kind1_21, nil
	case "1.22":
		return Kind1_22, nil
	case "1.23":
		return Kind1_23, nil
	case "1.24":
		return Kind1_24, nil
	}
	return "", errors.Errorf("Unrecognized Kind version: %q", version)
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
