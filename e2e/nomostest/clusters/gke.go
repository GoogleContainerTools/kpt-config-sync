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
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
)

const (
	// defaultOperationTimeoutGKE is the default time that we wait for a GKE
	// operation to complete. These can take a long time, especially on autopilot.
	// For example, we've observed RESIZE_CLUSTER operations take ~20 minutes.
	defaultOperationTimeoutGKE = 30 * time.Minute
)

// GKECluster is a GKE cluster for use in the e2e tests
type GKECluster struct {
	// T is a testing interface
	T testing.NTB
	// Name is the name of the cluster
	Name string
	// KubeConfigPath is the path to save the kube config
	KubeConfigPath string
}

// Exists returns whether the GKE cluster exists
func (c *GKECluster) Exists() (bool, error) {
	return clusterExistsGKE(c.T, c.Name)
}

// Create the GKE cluster
func (c *GKECluster) Create() error {
	return createGKECluster(c.T, c.Name)
}

// Delete the GKE cluster
func (c *GKECluster) Delete() error {
	return deleteGKECluster(c.T, c.Name)
}

// Connect to the GKE cluster
func (c *GKECluster) Connect() error {
	return getGKECredentials(c.T, c.Name, c.KubeConfigPath)
}

// Hash returns the cluster hash
func (c *GKECluster) Hash() (string, error) {
	return getClusterHash(c.T, c.Name, c.KubeConfigPath)
}

func withKubeConfig(cmd *exec.Cmd, kubeconfig string) *exec.Cmd {
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", kubeconfig))
	return cmd
}

// listOperations lists RUNNING operations on the target cluster
func listOperations(ctx context.Context, t testing.NTB, name string) ([]string, error) {
	args := []string{
		"container", "operations", "list",
		"--project", *e2e.GCPProject,
		"--filter", fmt.Sprintf("status = RUNNING AND targetLink ~ ^.*/%s$", name),
		"--format", "value(name)",
		"--no-user-output-enabled", // mute warnings
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	t.Logf("gcloud %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list operations for %s: %v\nstdout/stderr:\n%s",
			name, err, string(out))
	}
	operations := strings.Fields(string(out))
	return operations, nil
}

// waitOperation waits for the provided operation to complete
func waitOperation(ctx context.Context, t testing.NTB, operation string) error {
	args := []string{
		"container", "operations", "wait",
		operation,
		"--project", *e2e.GCPProject,
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	start := time.Now()
	defer func() {
		t.Logf("took %v to wait for operation %s", time.Since(start), operation)
	}()
	t.Logf("gcloud %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to wait for operation %s: %v\nstdout/stderr:\n%s",
			operation, err, string(out))
	}
	return nil
}

func listAndWaitForOperations(ctx context.Context, t testing.NTB, name string) error {
	if _, ok := ctx.Deadline(); !ok {
		// set default timeout if parent context doesn't have a deadline
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, defaultOperationTimeoutGKE)
		defer cancel()
	}
	operations, err := listOperations(ctx, t, name)
	if err != nil {
		return err
	}
	tg := taskgroup.New()
	for _, operation := range operations {
		tg.Go(func() error {
			return waitOperation(ctx, t, operation)
		})
	}
	return tg.Wait()
}

func deleteGKECluster(t testing.NTB, name string) error {
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
	// Sometimes an operation may be happening at the time the deletion request is
	// sent, causing delete to error. Retry for a brief period to increase the
	// chances for the delete operation.
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeoutGKE)
	defer cancel()
	took, err := retry.WithContext(ctx, func() error {
		if err := listAndWaitForOperations(ctx, t, name); err != nil {
			return err
		}
		t.Logf("gcloud %s", strings.Join(args, " "))
		cmd := exec.Command("gcloud", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to delete cluster %s: %v\nstdout/stderr:\n%s", name, err, string(out))
		}
		return nil
	})
	t.Logf("took %v retrying to start cluster deletion for %s", took, name)
	if err != nil {
		return err
	}
	// Wait for the cluster delete operation to complete. In CI we want to avoid
	// having the next job start until the previous job cleaned up (e.g. quota).
	return listAndWaitForOperations(context.Background(), t, name)
}

func createGKECluster(t testing.NTB, name string) error {
	args := []string{
		"container", "clusters",
	}
	if *e2e.GKEAutopilot {
		args = append(args, "create-auto")
	} else {
		args = append(args, "create")
	}
	args = append(args, name,
		"--project", *e2e.GCPProject,
		"--async",
		"--cluster-ipv4-cidr", "/19", // use smaller than default CIDR
	)
	var scopes []string
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
	// Standard-specific options (cannot specify for autopilot)
	if !*e2e.GKEAutopilot {
		if *e2e.GKEMachineType != "" {
			args = append(args, "--machine-type", *e2e.GKEMachineType)
		}
		if *e2e.GKEDiskSize != "" {
			args = append(args, "--disk-size", *e2e.GKEDiskSize)
		}
		if *e2e.GKEDiskType != "" {
			args = append(args, "--disk-type", *e2e.GKEDiskType)
		}
		if *e2e.GKENumNodes > 0 {
			args = append(args, "--num-nodes", fmt.Sprintf("%d", *e2e.GKENumNodes))
		}
		if *e2e.GceNode {
			// legacy oauth scopes are required for gcenode auth to access CSR/GAR
			scopes = append(scopes, "cloud-platform")
		} else {
			// gcenode tests require workload identity to be disabled
			args = append(args, "--workload-pool", fmt.Sprintf("%s.svc.id.goog", *e2e.GCPProject))
		}
		var addons []string
		if *e2e.KCC {
			addons = append(addons, "ConfigConnector")
		}
		if len(addons) > 0 {
			args = append(args, "--addons", strings.Join(addons, ","))
		}
	}
	if len(scopes) > 0 {
		args = append(args, "--scopes", strings.Join(scopes, ","))
	}
	t.Logf("gcloud %s", strings.Join(args, " "))
	cmd := exec.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create cluster %s: %v\nstdout/stderr:\n%s", name, err, string(out))
	}
	return listAndWaitForOperations(context.Background(), t, name)
}

// getGKECredentials fetches GKE credentials at the specified kubeconfig path.
func getGKECredentials(t testing.NTB, clusterName, kubeconfig string) error {
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
	t.Logf("To connect to GKE cluster %s:\ngcloud %s",
		clusterName, strings.Join(args, " "))
	cmd := withKubeConfig( // gcloud container clusters get-credentials <args>
		exec.Command("gcloud", args...),
		kubeconfig,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to get credentials: %v\nstdout/stderr:\n%s", err, string(out))
	}
	cmd = withKubeConfig( // gcloud config config-helper --force-auth-refresh
		exec.Command("gcloud", "config", "config-helper", "--force-auth-refresh"),
		kubeconfig,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to refresh access_token: %v\nstdout/stderr:\n%s", err, string(out))
	}
	// after getting credentials, wait for any running operations to complete
	// before handing over this cluster to the test environment
	return listAndWaitForOperations(context.Background(), t, clusterName)
}

func clusterExistsGKE(t testing.NTB, clusterName string) (bool, error) {
	args := []string{
		"container", "clusters", "list",
		"--project", *e2e.GCPProject,
		"--filter", fmt.Sprintf("name ~ ^%s$", clusterName),
		"--format", "value(name)",
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	t.Logf("gcloud %s", strings.Join(args, " "))
	cmd := exec.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to list cluster (%s): %v\nstdout/stderr:\n%s",
			clusterName, err, string(out))
	}
	clusters := strings.Fields(string(out))
	if len(clusters) == 1 && clusters[0] == clusterName {
		return true, nil
	}
	return false, nil
}

// getClusterHash fetches GKE cluster hash
func getClusterHash(t testing.NTB, clusterName, kubeconfig string) (string, error) {
	args := []string{
		"container", "clusters", "describe",
		clusterName, "--project", *e2e.GCPProject,
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	args = append(args, "--format", "value(id)")
	t.Logf("gcloud %s", strings.Join(args, " "))
	cmd := withKubeConfig( // gcloud container clusters describe <args> --format 'value(id)'
		exec.Command("gcloud", args...),
		kubeconfig,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get cluster hash: %v\nstdout/stderr:\n%s", err, string(out))
	}
	return string(out), nil
}
