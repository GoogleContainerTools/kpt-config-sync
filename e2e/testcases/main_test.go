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

package e2e

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/multierr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/util"
)

// This is a bit of a hack to enforce our --num-clusters flag over the --test.parallel
// parameter of `go test`. The `go test` defaults to GOMAXPROCS, which is sane
// for unit tests but is an undesirable default for e2e tests which create
// a cluster per thread. Default to 1 instead.
func setParallelFlag() error {
	err := flag.Set("test.parallel", strconv.Itoa(e2e.NumParallel()))
	return err
}

func setDefaultArgs() {
	// Force lower case to match enums
	*e2e.TestCluster = strings.ToLower(*e2e.TestCluster)

	// backwards compatibility for GCPCLuster arg
	if *e2e.GCPCluster != "" {
		fmt.Printf("GCP_CLUSTER provided. Setting CLUSTER_NAMES to [%s]\n", *e2e.GCPCluster)
		*e2e.ClusterNames = []string{*e2e.GCPCluster}
	}
	// if cluster-names is provided, set the number of test threads to the number
	// of pre-provisioned clusters.
	if len(*e2e.ClusterNames) > 0 {
		*e2e.NumClusters = len(*e2e.ClusterNames)
	}
	// convenience to reduce required params. cluster-names implies share-test-env.
	if len(*e2e.ClusterNames) > 0 {
		*e2e.ShareTestEnv = true
	}
	// default to creating clusters for KinD. this is an acceptable default for
	// KinD, but for GKE the user should explicitly request creating clusters.
	if *e2e.TestCluster == e2e.Kind && len(*e2e.ClusterNames) == 0 {
		*e2e.CreateClusters = e2e.CreateClustersEnabled
	}

	// GKE Standard dynamic defaults.
	// flag > environment variable > stress default > regular default.
	// This allows us to use special defaults for stress tests.
	// TestStressManyDeployments needs at least 10 nodes to schedule 1,000 pods,
	// due to 110 pod limit per node. Having more nodes also helps prime the
	// control plane auto-scaler to handle higher load.
	// Smaller nodes help keep cost down when using more of them.
	if *e2e.TestCluster == e2e.GKE && !*e2e.GKEAutopilot {
		*e2e.GKEMachineType = flagEnvVarStressDefaultString(*e2e.GKEMachineType,
			"GKE_MACHINE_TYPE",
			e2e.DefaultGKEMachineTypeForStress,
			e2e.DefaultGKEMachineType)

		*e2e.GKEDiskSize = flagEnvVarStressDefaultString(*e2e.GKEDiskSize,
			"GKE_DISK_SIZE",
			e2e.DefaultGKEDiskSizeForStress,
			e2e.DefaultGKEDiskSize)

		*e2e.GKENumNodes = flagEnvVarStressDefaultInt(*e2e.GKENumNodes,
			"GKE_NUM_NODES",
			e2e.DefaultGKENumNodesForStress,
			e2e.DefaultGKENumNodes)
	}
}

func flagEnvVarStressDefaultString(flagValue, envVarName, stressDefault, elseDefault string) string {
	if flagValue != "" {
		return flagValue
	}
	return util.EnvString(envVarName,
		ifStressString(stressDefault,
			elseDefault))
}

func flagEnvVarStressDefaultInt(flagValue int, envVarName string, stressDefault, elseDefault int) int {
	if flagValue != 0 {
		return flagValue
	}
	return util.EnvInt(envVarName,
		ifStressInt(stressDefault,
			elseDefault))
}

func ifStressString(yes, no string) string {
	if *e2e.Stress {
		return yes
	}
	return no
}

func ifStressInt(yes, no int) int {
	if *e2e.Stress {
		return yes
	}
	return no
}

func validateArgs() error {
	var errs error
	if len(*e2e.ClusterNames) == 0 && *e2e.CreateClusters == e2e.CreateClustersDisabled {
		errs = multierr.Append(errs, fmt.Errorf("At least one of CLUSTER_NAMES or CREATE_CLUSTERS is required"))
	}
	if !slices.Contains(e2e.CreateClustersAllowedValues, *e2e.CreateClusters) {
		errs = multierr.Append(errs,
			fmt.Errorf("Unrecognized value %s for CREATE_CLUSTERS. Allowed values: [%s]",
				*e2e.CreateClusters, strings.Join(e2e.CreateClustersAllowedValues, ", ")))
	}
	if !slices.Contains(e2e.DestroyClustersAllowedValues, *e2e.DestroyClusters) {
		errs = multierr.Append(errs,
			fmt.Errorf("Unrecognized value %s for DESTROY_CLUSTERS. Allowed values: [%s]",
				*e2e.DestroyClusters, strings.Join(e2e.DestroyClustersAllowedValues, ", ")))
	}
	if *e2e.TestCluster == e2e.GKE { // required vars for GKE
		if *e2e.GCPProject == "" {
			errs = multierr.Append(errs, fmt.Errorf("Environment variable GCP_PROJECT is required for GKE clusters"))
		}
		if *e2e.GCPRegion == "" && *e2e.GCPZone == "" {
			errs = multierr.Append(errs, fmt.Errorf("One of GCP_REGION or GCP_ZONE is required for GKE clusters"))
		}
		if *e2e.GCPRegion != "" && *e2e.GCPZone != "" {
			errs = multierr.Append(errs, fmt.Errorf("At most one of GCP_ZONE or GCP_REGION may be specified"))
		}
		if *e2e.GKEAutopilot && *e2e.GCPRegion == "" {
			errs = multierr.Append(errs, fmt.Errorf("Autopilot clusters must be created with a region"))
		}
		if *e2e.GKEAutopilot && *e2e.GceNode {
			errs = multierr.Append(errs, fmt.Errorf("Cannot run gcenode tests on autopilot clusters"))
		}
	}
	return errs
}

func TestMain(m *testing.M) {
	os.Exit(main(m))
}

func main(m *testing.M) int {
	// This TestMain function is required in every e2e test case file.
	flag.Parse()

	if !*e2e.E2E && !*e2e.Stress && !*e2e.Profiling {
		// This allows `go test ./...` to function as expected without triggering any long running tests.
		return 0
	}
	if *e2e.Usage {
		flag.Usage()
		return 0
	}
	setDefaultArgs()
	if err := setParallelFlag(); err != nil {
		fmt.Printf("Error setting test.parallel: %v\n", err)
		return 1
	}

	if err := validateArgs(); err != nil {
		fmt.Printf("error validating e2e test args: %v\n", err)
		return 1
	}

	if err := nomostest.CheckImages(); err != nil {
		fmt.Println(err)
		return 1
	}

	if *e2e.ShareTestEnv {
		defer nomostest.CleanSharedNTs()
		if err := nomostest.InitSharedEnvironments(); err != nil {
			fmt.Printf("Error in InitSharedEnvironments: %v\n", err)
			return 1
		}
	}
	defer func() {
		nomostest.PrintFeatureDurations()
	}()
	exitCode := m.Run()
	return exitCode
}
