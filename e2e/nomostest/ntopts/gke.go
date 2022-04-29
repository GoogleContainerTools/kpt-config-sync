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
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// GKECluster tells the test to use the GKE cluster pointed to by the config flags.
func GKECluster(t testing.NTB) Opt {
	return func(opt *New) {
		t.Helper()

		kubeconfig := *e2e.KubeConfig
		if len(kubeconfig) == 0 {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
		if err := os.Setenv(Kubeconfig, kubeconfig); err != nil {
			t.Fatalf("unexpected error %v", err)
		}

		forceAuthRefresh(t)
		restConfig, err := restconfig.NewRestConfig(restconfig.DefaultTimeout)
		if err != nil {
			t.Fatal(err)
		}
		opt.RESTConfig = restConfig
	}
}

// forceAuthRefresh forces gcloud to refresh the access_token to avoid using an expired one in the middle of a test.
func forceAuthRefresh(t testing.NTB) {
	out, err := exec.Command("kubectl", "config", "current-context").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to get current config: %v", err)
	}
	context := strings.TrimSpace(string(out))
	gkeArgs := strings.Split(context, "_")
	if len(gkeArgs) < 4 {
		t.Fatalf("unknown GKE context fomrat: %s", context)
	}
	gkeProject := gkeArgs[1]
	gkeZone := gkeArgs[2]
	gkeCluster := gkeArgs[3]

	_, err = exec.Command("gcloud", "container", "clusters", "get-credentials",
		gkeCluster, "--zone", gkeZone, "--project", gkeProject).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to get credentials: %v", err)
	}

	_, err = exec.Command("gcloud", "config", "config-helper", "--force-auth-refresh").CombinedOutput()
	if err != nil {
		t.Fatalf("failed to refresh access_token: %v", err)
	}
}
