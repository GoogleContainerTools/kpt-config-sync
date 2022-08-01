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

package ntopts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"kpt.dev/configsync/e2e"
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

// Kind creates a Kind cluster for the test and fills in the RESTConfig option
// with the information needed to establish a Client with it.
//
// version is one of the KindVersion constants above.
func Kind(t testing.NTB, version string) Opt {
	v := asKindVersion(t, version)
	return func(opt *New) {
		opt.RESTConfig = newKind(t, opt.Name, opt.TmpDir, v)
	}
}

// asKindVersion returns the latest Kind version associated with a given
// Kubernetes minor version.
func asKindVersion(t testing.NTB, version string) KindVersion {
	t.Helper()

	switch version {
	case "1.14":
		return Kind1_14
	case "1.15":
		return Kind1_15
	case "1.16":
		return Kind1_16
	case "1.17":
		return Kind1_17
	case "1.18":
		return Kind1_18
	case "1.19":
		return Kind1_19
	case "1.20":
		return Kind1_20
	case "1.21":
		return Kind1_21
	case "1.22":
		return Kind1_22
	case "1.23":
		return Kind1_23
	case "1.24":
		return Kind1_24
	}
	t.Fatalf("Unrecognized Kind version: %q", version)
	return ""
}

// newKind creates a new Kind cluster for use in testing with the specified name.
//
// Automatically registers the cluster to be deleted at the end of the test.
func newKind(t testing.NTB, name, tmpDir string, version KindVersion) *rest.Config {
	p := cluster.NewProvider()
	kcfgPath := filepath.Join(tmpDir, Kubeconfig)

	if err := os.Setenv(Kubeconfig, kcfgPath); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	start := time.Now()
	t.Logf("started creating cluster at %s", start.Format(time.RFC3339))

	err := createKindCluster(p, name, kcfgPath, version)
	creationSuccessful := err == nil
	finish := time.Now()

	// Register the cluster to be deleted at the end of the test, even if cluster
	// creation failed.
	t.Cleanup(func() {
		if t.Failed() && *e2e.Debug {
			t.Errorf(`Conect to kind cluster:
kind export kubeconfig --name=%s`, name)
			t.Errorf(`Delete kind cluster:
kind delete cluster --name=%s`, name)
			return
		}

		if !creationSuccessful {
			// Since we have set retain=true, the cluster is still available even
			// though creation did not execute successfully.
			artifactsDir := os.Getenv("ARTIFACTS")
			if artifactsDir == "" {
				artifactsDir = filepath.Join(tmpDir, "artifacts")
			}
			t.Logf("exporting failed cluster logs to %s", artifactsDir)
			err := exec.Command("kind", "export", "logs", "--name", name, artifactsDir).Run()
			if err != nil {
				t.Errorf("exporting kind logs: %v", err)
			}
		}

		// If the test runner stops testing with a command like ^C, cleanup
		// callbacks such as this are not executed.
		err := p.Delete(name, kcfgPath)
		if err != nil {
			t.Errorf("deleting Kind cluster %q: %v", name, err)
		}
	})

	if err != nil {
		t.Logf("failed creating cluster at %s", finish.Format(time.RFC3339))
		t.Logf("command took %v to fail", finish.Sub(start))
		t.Fatalf("creating Kind cluster: %v", err)
	}
	t.Logf("finished creating cluster at %s", finish.Format(time.RFC3339))

	// We don't need to specify masterUrl since we have a Kubeconfig.
	cfg, err := clientcmd.BuildConfigFromFlags("", kcfgPath)
	if err != nil {
		t.Fatalf("building rest.Config: %v", err)
	}

	return cfg
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
		)
		if err == nil {
			return nil
		}
	}

	// We failed to create the cluster maxKindTries times, so fail out.
	return err
}
