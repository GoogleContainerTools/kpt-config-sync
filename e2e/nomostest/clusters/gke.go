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
	"strings"
	"time"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/testing"
)

// GKECluster tells the test to use the GKE cluster pointed to by the config flags.
func GKECluster(t testing.NTB) ntopts.Opt {
	return func(opt *ntopts.New) {
		t.Helper()
		clusterName := *e2e.GCPCluster
		if *e2e.CreateClusters {
			opt.IsEphemeralCluster = true
			clusterName = opt.Name
			t.Cleanup(func() {
				deleteGKECluster(t, clusterName)
			})
			createGKECluster(t, clusterName)
		}
		getGKECredentials(t, clusterName, opt.KubeconfigPath)
	}
}

func withKubeConfig(cmd *exec.Cmd, kubeconfig string) *exec.Cmd {
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", kubeconfig))
	return cmd
}

func deleteGKECluster(t testing.NTB, name string) {
	args := []string{
		"container", "clusters", "delete",
		name, "--project", *e2e.GCPProject, "--quiet", "--async",
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	if skipClusterCleanup(t) {
		t.Errorf("[WARNING] skipping cleanup of GKE cluster %s (--debug)", name)
		t.Errorf("To delete GKE cluster %s:\ngcloud %s", name, strings.Join(args, " "))
		return
	}
	t.Logf("Deleting GKE cluster %s", name)
	start := time.Now()
	defer func() {
		t.Logf("took %s to delete cluster %s", time.Since(start), name)
	}()
	cmd := exec.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to delete cluster %s: %v\nstdout/stderr:\n%s", name, err, string(out))
	}
}

func createGKECluster(t testing.NTB, name string) {
	args := []string{
		"container", "clusters",
	}
	if *e2e.GKEAutopilot {
		args = append(args, "create-auto")
	} else {
		args = append(args, "create")
	}
	args = append(args, name, "--project", *e2e.GCPProject)
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	if *e2e.GCPNetwork != "" {
		args = append(args, "--network", *e2e.GCPNetwork)
	}
	if *e2e.GCPSubNetwork != "" {
		args = append(args, "--subnetwork", *e2e.GCPSubNetwork)
	}
	if *e2e.GKEReleaseChannel != "" {
		args = append(args, "--release-channel", *e2e.GKEReleaseChannel)
	}
	if *e2e.GKEClusterVersion != "" {
		args = append(args, "--cluster-version", *e2e.GKEClusterVersion)
	}
	if *e2e.GKEMachineType != "" {
		args = append(args, "--machine-type", *e2e.GKEMachineType)
	}
	if *e2e.GKENumNodes > 0 {
		args = append(args, "--num-nodes", fmt.Sprintf("%d", *e2e.GKENumNodes))
	}
	start := time.Now()
	t.Logf("Creating GKE cluster %s at %s:\ngcloud %s",
		name, start.Format(time.RFC3339), strings.Join(args, " "))
	defer func() {
		t.Logf("took %s to create GKE cluster %s", time.Since(start), name)
	}()
	cmd := exec.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to create cluster %s: %v\nstdout/stderr:\n%s", name, err, string(out))
	}
}

// getGKECredentials fetches GKE credentials at the specified kubeconfig path.
func getGKECredentials(t testing.NTB, clusterName, kubeconfig string) {
	args := []string{
		"container", "clusters", "get-credentials",
		clusterName, "--project", *e2e.GCPProject,
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	logFunc := func() {
		t.Logf("To connect to GKE cluster %s:\ngcloud %s",
			clusterName, strings.Join(args, " "))
	}
	t.Cleanup(func() {
		if skipClusterCleanup(t) {
			logFunc()
		}
	})
	logFunc()
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

func skipClusterCleanup(t testing.NTB) bool {
	return t.Failed() && *e2e.Debug
}
