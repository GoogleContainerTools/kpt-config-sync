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
	"time"

	"kpt.dev/configsync/e2e/nomostest/testing"
)

// GKECluster tells the test to use the GKE cluster pointed to by the config flags.
func GKECluster(t testing.NTB, apiServerTimeout time.Duration) Opt {
	return func(opt *New) {
		t.Helper()

		getGKECredentials(t, opt.KubeconfigPath)
	}
}

func withKubeConfig(cmd *exec.Cmd, kubeconfig string) *exec.Cmd {
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", kubeconfig))
	return cmd
}

// getGKECredentials fetches GKE credentials at the specified kubeconfig path.
func getGKECredentials(t testing.NTB, kubeconfig string) {
	args := []string{
		"container", "clusters", "get-credentials",
		testing.GCPClusterFromEnv, "--project", testing.GCPProjectIDFromEnv,
	}
	if testing.GCPZoneFromEnv != "" {
		args = append(args, "--zone", testing.GCPZoneFromEnv)
	}
	if testing.GCPRegionFromEnv != "" {
		args = append(args, "--region", testing.GCPRegionFromEnv)
	}
	cmd := withKubeConfig( // gcloud container clusters get-credentials <args>
		exec.Command("gcloud", args...),
		kubeconfig,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to get credentials: %v\nstdout/stderr:\n%s", err, string(out))
	}
	cmd = withKubeConfig( // gcloud config config-helper --force-auth-refresh
		exec.Command("gcloud", "config", "config-helper", "--force-auth-refresh"),
		kubeconfig,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to refresh access_token: %v\nstdout/stderr:\n%s", err, string(out))
	}
}
