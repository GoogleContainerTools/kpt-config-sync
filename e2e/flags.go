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

// Package e2e defines e2e-test-specific imports and flags for use in e2e
// testing.
package e2e

import (
	"flag"
	"fmt"
	"slices"
	"strings"
	"testing"

	"kpt.dev/configsync/pkg/util"
)

// stringListFlag parses a comma delimited string field into a string slice
type stringListFlag struct {
	arr []string
}

func (i *stringListFlag) String() string {
	return strings.Join(i.arr, ",")
}

func (i *stringListFlag) Set(value string) error {
	i.arr = strings.Split(value, ",")
	return nil
}

func newStringListFlag(name string, def []string, usage string) *[]string {
	lf := &stringListFlag{
		arr: def,
	}
	flag.Var(lf, name, usage)
	return &lf.arr
}

// stringEnum is a string flag with a set of allowed values
type stringEnum struct {
	value         string
	allowedValues []string
}

func (i *stringEnum) String() string {
	return i.value
}

func (i *stringEnum) Set(value string) error {
	if !slices.Contains(i.allowedValues, value) {
		return fmt.Errorf("%s not in allowed values: %s", value, i.allowedValues)
	}
	i.value = value
	return nil
}

func newStringEnum(name string, defaultVal string, usage string, allowed []string) *string {
	lf := &stringEnum{
		value:         defaultVal,
		allowedValues: allowed,
	}
	flag.Var(lf, name, fmt.Sprintf("%s Allowed values: %s", usage, allowed))
	return &lf.value
}

// E2E enables running end-to-end tests.
var E2E = flag.Bool("e2e", false,
	"If true, run end-to-end tests.")

// Stress enables running of stress tests.
var Stress = flag.Bool("stress", false,
	"If true, run stress tests.")

// Profiling enables running of profiling tests.
var Profiling = flag.Bool("profiling", false,
	"If true, run profiling tests.")

// KCC enables running the e2e tests for kcc resources.
var KCC = flag.Bool("kcc", false,
	"If true, run kcc tests.")

// GceNode enables running the e2e tests for 'gcenode' auth type
var GceNode = flag.Bool("gcenode", false,
	"If true, run test with 'gcenode' auth type.")

// GitHubApp enables running the e2e tests for 'githubapp' auth type
var GitHubApp = flag.Bool("githubapp", false,
	"If true, run test with 'githubapp' auth type.")

// GitHubAppConfigFile specifies a file path to read GitHub App config from.
// If unset, defaults to fetching the GitHub App config from Secret Manager.
var GitHubAppConfigFile = flag.String("githubapp-config-file",
	util.EnvString("E2E_GITHUBAPP_CONFIG_FILE", ""),
	"Local file path to GitHub App configuration file.")

// Debug enables running the test in debug mode.
// In debug mode:
//  1. Test execution immediately stops on a call to t.Fatal.
//  2. The test prints the absolute path to the test temporary directory, and
//     not delete it.
//  3. The test prints out how to connect to the kind cluster.
var Debug = flag.Bool("debug", false,
	"If true, do not destroy cluster and clean up temporary directory after test.")

// KubernetesVersion is the version of Kubernetes to test against. Only has effect
// when testing against test-created Kind clusters.
var KubernetesVersion = flag.String("kubernetes-version", "1.33",
	"The version of Kubernetes to create")

// DefaultImagePrefix points to the local docker registry.
const DefaultImagePrefix = "localhost:5000"

// Manual indicates the test is being run manually. Some tests are not yet safe
// to be run automatically.
var Manual = flag.Bool("manual", false,
	"Specify that the test is being run manually.")

// TestCluster specifies the cluster config used for testing.
var TestCluster = flag.String("test-cluster", Kind,
	fmt.Sprintf("The cluster config used for testing. Allowed values are: %s and %s. "+
		"If --test-cluster=%s, create a Kind cluster. Otherwise use the GKE context specified in %s.",
		GKE, Kind, Kind, Kubeconfig))

// ShareTestEnv indicates whether to share the test env for all test cases.
// If it is true, we only install nomos once before all tests and tear it down until all tests complete.
var ShareTestEnv = flag.Bool("share-test-env", false,
	"Specify that the test is using a shared test environment instead of fresh installation per test case.")

// GitProvider is the provider that hosts the Git repositories.
var GitProvider = newStringEnum("git-provider", util.EnvString("E2E_GIT_PROVIDER", Local),
	"The git provider that hosts the Git repositories. Defaults to Local.",
	[]string{Local, Bitbucket, GitLab, CSR})

// OCIProvider is the provider that hosts the OCI repositories.
var OCIProvider = newStringEnum("oci-provider", util.EnvString("E2E_OCI_PROVIDER", Local),
	"The registry provider that hosts the OCI repositories. Defaults to Local.",
	[]string{Local, ArtifactRegistry})

// HelmProvider is the provider that hosts the helm repositories.
var HelmProvider = newStringEnum("helm-provider", util.EnvString("E2E_HELM_PROVIDER", Local),
	"The registry provider that hosts the helm repositories. Defaults to Local.",
	[]string{Local, ArtifactRegistry})

// TestFeatures is the list of features to run.
var TestFeatures = flag.String("test-features", "",
	"A list of features to run, separated by comma. Defaults to empty, which should run all tests.")

// NumClusters is the number of clusters to run tests on. Each cluster only has
// a single test running on it at a given time. The number of clusters equals the
// number of test threads which can run concurrently.
var NumClusters = flag.Int("num-clusters", util.EnvInt("E2E_NUM_CLUSTERS", 1),
	"Number of parallel test threads to run. Also dictates the number of clusters which will be created in parallel. Overrides the -test.parallel flag.")

// ClusterPrefix is the prefix to use when naming clusters. An index is appended
// to the prefix with the pattern '<prefix>-<index>', starting at 0. If unset, defaults
// to cs-e2e-<UNIX_TIME>. cluster-names takes precedence, if provided.
var ClusterPrefix = flag.String("cluster-prefix", util.EnvString("E2E_CLUSTER_PREFIX", ""),
	"Prefix to use when naming clusters. An index is appended to the prefix with the pattern '<prefix>-<index>', starting at 0. If unset, defaults to cs-e2e-<UNIX_TIME>. cluster-names takes precedence, if provided.")

// ClusterNames is a list of cluster names to use for the tests. If specified without
// create-clusters, assumes the clusters were pre-provisioned.
var ClusterNames = newStringListFlag("cluster-names", util.EnvList("E2E_CLUSTER_NAMES", nil),
	"List of cluster names to use for the tests. If specified without create-clusters, assumes the clusters were pre-provisioned.")

// Usage indicates to print usage and exit. This is a workaround for the builtin
// help command of `go test`
var Usage = flag.Bool("usage", false, "Print usage and exit.")

// GCPProject is the GCP project to use when running tests.
var GCPProject = flag.String("gcp-project", util.EnvString("GCP_PROJECT", ""),
	"GCP Project to use when running tests. Defaults to GCP_PROJECT env var.")

// GCPCluster is the GCP Cluster (GKE) to use when running tests.
var GCPCluster = flag.String("gcp-cluster", util.EnvString("GCP_CLUSTER", ""),
	"GCP Cluster (GKE) to use when running tests. Defaults to GCP_CLUSTER env var.")

// GCPRegion is the GCP Region to use when running tests.
var GCPRegion = flag.String("gcp-region", util.EnvString("GCP_REGION", ""),
	"GCP Region to use when running tests. Only one of gcp-region and gcp-zone must be set. Defaults to GCP_REGION env var.")

// GCPZone is the GCP Zone to use when running tests.
var GCPZone = flag.String("gcp-zone", util.EnvString("GCP_ZONE", ""),
	"GCP Zone to use when running tests. Only one of gcp-region and gcp-zone must be set. Defaults to GCP_ZONE env var.")

// GCPNetwork is the GCP network to use when creating GKE clusters.
var GCPNetwork = flag.String("gcp-network", util.EnvString("GCP_NETWORK", ""),
	"GCP Network to use when creating GKE clusters. Defaults to GCP_SUBNETWORK env var,")

// GCPSubNetwork is the GCP subnetwork to use when creating GKE clusters.
var GCPSubNetwork = flag.String("gcp-subnetwork", util.EnvString("GCP_SUBNETWORK", ""),
	"GCP Subnetwork to use when creating clusters. Defaults to GCP_SUBNETWORK env var.")

// GKEReleaseChannel is the GKE release channel to use when creating GKE clusters.
var GKEReleaseChannel = flag.String("gke-release-channel", util.EnvString("GKE_RELEASE_CHANNEL", DefaultGKEChannel),
	"GKE release channel to use when creating GKE clusters. Defaults to GKE_RELEASE_CHANNEL env var.")

// GKEClusterVersion is the GKE cluster version to use when creating GKE clusters.
var GKEClusterVersion = flag.String("gke-cluster-version", util.EnvString("GKE_CLUSTER_VERSION", ""),
	"GKE cluster version to use when creating GKE clusters. Defaults to GKE_CLUSTER_VERSION env var.")

// GKEMachineType is the GKE machine type to use when creating GKE clusters.
// See testcases.setDefaultArgs for dynamic defaults.
var GKEMachineType = flag.String("gke-machine-type", "",
	"GKE machine type to use when creating GKE clusters. Defaults to GKE_MACHINE_TYPE env var.")

// GKEDiskSize is the GKE disk size to use when creating GKE clusters.
// See testcases.setDefaultArgs for dynamic defaults.
var GKEDiskSize = flag.String("gke-disk-size", "",
	"GKE disk size to use when creating GKE clusters. Defaults to GKE_DISK_SIZE env var.")

// GKEDiskType is the GKE disk type to use when creating GKE clusters.
var GKEDiskType = flag.String("gke-disk-type", util.EnvString("GKE_DISK_TYPE", DefaultGKEDiskType),
	"GKE disk type to use when creating GKE clusters. Defaults to GKE_DISK_TYPE env var.")

// GKENumNodes is the number of nodes to use when creating GKE clusters.
// See testcases.setDefaultArgs for dynamic defaults.
var GKENumNodes = flag.Int("gke-num-nodes", 0,
	"Number of node to use when creating GKE clusters. Defaults to GKE_NUM_NODES env var.")

// GKEAutopilot indicates whether to enable autopilot when creating GKE clusters.
var GKEAutopilot = flag.Bool("gke-autopilot", util.EnvBool("GKE_AUTOPILOT", false),
	"Whether to create GKE clusters with autopilot enabled.")

// CreateClusters indicates the test framework should create clusters.
var CreateClusters = flag.String("create-clusters", util.EnvString("E2E_CREATE_CLUSTERS", CreateClustersDisabled),
	fmt.Sprintf("Whether to create clusters, otherwise will assume pre-provisioned clusters. Allowed values: [%s]",
		strings.Join(CreateClustersAllowedValues, ", ")))

// CreateClustersAllowedValues is a list of allowed values for the create-clusters parameter
var CreateClustersAllowedValues = []string{CreateClustersEnabled, CreateClustersLazy, CreateClustersDisabled}

// DestroyClusters indicates whether to destroy clusters that were created by the test suite after the tests.
var DestroyClusters = flag.String("destroy-clusters", util.EnvString("E2E_DESTROY_CLUSTERS", DestroyClustersAuto),
	fmt.Sprintf("Whether to destroy clusters that were created by the test suite after the tests. Allowed Values: [%s]",
		strings.Join(DestroyClustersAllowedValues, ", ")))

// DestroyClustersAllowedValues is a list of allowed values for the destroy-clusters parameter
var DestroyClustersAllowedValues = []string{DestroyClustersEnabled, DestroyClustersAuto, DestroyClustersDisabled}

const (
	// Kind indicates creating a Kind cluster for testing.
	Kind = "kind"
	// GKE indicates using an existing GKE cluster for testing.
	GKE = "gke"
	// Kubeconfig provides the context via KUBECONFIG for testing.
	Kubeconfig = "kube-config"
	// DefaultGKEChannel is the default GKE release channel to use when creating a cluster
	DefaultGKEChannel = "regular"
	// DefaultGKEMachineType is the default GKE machine type to use when creating a cluster
	DefaultGKEMachineType = "n2-standard-8"
	// DefaultGKEMachineTypeForStress is the default GKE machine type to
	// use when creating a GKE cluster for stress tests.
	DefaultGKEMachineTypeForStress = "n2-standard-4"
	// DefaultGKENumNodes is the default number of nodes to use when creating a GKE cluster
	DefaultGKENumNodes = 1
	// DefaultGKENumNodesForStress is the default number of nodes to use
	// when creating a GKE cluster for stress tests.
	DefaultGKENumNodesForStress = 4
	// DefaultGKEDiskType is the default disk type to use when creating a cluster
	DefaultGKEDiskType = "pd-ssd"
	// DefaultGKEDiskSize is the default disk size to use when creating a GKE cluster
	DefaultGKEDiskSize = "50Gb"
	// DefaultGKEDiskSizeForStress is the default disk size to use when creating
	// a GKE cluster for stress tests.
	DefaultGKEDiskSizeForStress = "25Gb"
	// CreateClustersEnabled indicates that clusters should be created and error
	// if the cluster already exists.
	CreateClustersEnabled = "true"
	// CreateClustersLazy indicates to use clusters that exist and create them
	// if they don't
	CreateClustersLazy = "lazy"
	// CreateClustersDisabled indicates to not create clusters
	CreateClustersDisabled = "false"
	// DestroyClustersEnabled indicates to destroy clusters
	DestroyClustersEnabled = "true"
	// DestroyClustersAuto indicates to only destroy clusters if they were created
	// by the test framework
	DestroyClustersAuto = "auto"
	// DestroyClustersDisabled indicates to not destroy clusters
	DestroyClustersDisabled = "false"
)

const (
	// Local indicates using a local git-test-server.
	Local = "local"
	// Bitbucket indicates using Bitbucket to host the repositories.
	Bitbucket = "bitbucket"
	// Github indicates using GitHub to host the repositories.
	Github = "github"
	// GitLab indicates using GitLab to host the repositories.
	GitLab = "gitlab"
	// CSR indicates using Google Cloud Source Repositories to host the repositories.
	CSR = "csr"
	// ArtifactRegistry indicates using Google Artifact Registry to host the repositories.
	ArtifactRegistry = "gar"
)

// NumParallel returns the number of parallel test threads
func NumParallel() int {
	return *NumClusters
}

// RunInParallel indicates whether the test is running in parallel.
func RunInParallel() bool {
	return NumParallel() > 1
}

// EnableParallel allows parallel execution of test functions that call t.Parallel
// if test.parallel is greater than 1.
func EnableParallel(t *testing.T) {
	if RunInParallel() {
		t.Parallel()
	}
}
