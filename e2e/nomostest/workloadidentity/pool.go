// Copyright 2023 Google LLC
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

package workloadidentity

import (
	"encoding/json"
	"fmt"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
)

// ValidateEnabled validates that the test cluster has Workload Identity
// enabled and that the pool matches the test project.
func ValidateEnabled(nt *nomostest.NT) error {
	expectedPool := fmt.Sprintf("%s.svc.id.goog", *e2e.GCPProject)
	if workloadPool, err := GetWorkloadPool(nt); err != nil {
		return err
	} else if workloadPool != expectedPool {
		return fmt.Errorf("expected workloadPool %s but got %s", expectedPool, workloadPool)
	}
	return nil
}

// ValidateDisabled validates that the test cluster has Workload Identity
// disabled.S
func ValidateDisabled(nt *nomostest.NT) error {
	if workloadPool, err := GetWorkloadPool(nt); err != nil {
		return err
	} else if workloadPool != "" {
		return fmt.Errorf("expected workload identity to be disabled but got pool: %s", workloadPool)
	}
	return nil
}

// GetWorkloadPool verifies that the test cluster has workload identity enabled.
func GetWorkloadPool(nt *nomostest.NT) (string, error) {
	args := []string{
		"container", "clusters", "describe", nt.ClusterName,
		"--project", *e2e.GCPProject,
		"--format", "json",
	}
	if *e2e.GCPZone != "" {
		args = append(args, "--zone", *e2e.GCPZone)
	}
	if *e2e.GCPRegion != "" {
		args = append(args, "--region", *e2e.GCPRegion)
	}
	cmd := nt.Shell.Command("gcloud", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	clusterConfig := &clusterDescribe{}
	if err := json.Unmarshal(out, clusterConfig); err != nil {
		return "", err
	}
	return clusterConfig.WorkloadIdentityConfig.WorkloadPool, nil
}

// clusterDescribe represents the output format of gcloud container clusters describe
// this struct contains the field we are interested in
type clusterDescribe struct {
	// WorkloadIdentityConfig is the workload identity config
	WorkloadIdentityConfig workloadIdentityConfig `json:"workloadIdentityConfig"`
}

type workloadIdentityConfig struct {
	// WorkloadPool is the workload pool
	WorkloadPool string `json:"workloadPool"`
}
