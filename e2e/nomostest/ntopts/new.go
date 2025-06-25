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
	"k8s.io/client-go/rest"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Opt is an option type for ntopts.New.
type Opt func(opt *New)

// Commit represents a commit to be created on a git repository
type Commit struct {
	// Message is the commit message
	Message string
	// Files is a map of file paths to Objects
	Files map[string]client.Object
}

// New is the set of options for instantiating a new NT test.
type New struct {
	// Name is the name of the test. Overrides the one generated from the test
	// name.
	Name string

	// ClusterName is the name of the target cluster used by the test.
	ClusterName string

	// ClusterHash is a 64 character long unique identifier formed in hex used to identify a GKE cluster.
	ClusterHash string

	// IsEphemeralCluster indicates whether the cluster will be destroyed.
	IsEphemeralCluster bool

	// TmpDir is the base temporary directory to use for the test. Overrides the
	// generated directory based on Name and the OS's main temporary directory.
	TmpDir string

	// RESTConfig is the config for creating a Client connection to a K8s cluster.
	RESTConfig *rest.Config

	// KubeconfigPath is the path to the kubeconfig file
	KubeconfigPath string

	// SkipConfigSyncInstall skips installation/cleanup of Config Sync
	SkipConfigSyncInstall bool

	// SkipAutopilot will skip the test if running on an Autopilot cluster.
	SkipAutopilot bool

	// InitialCommit commit to create before the initial sync
	InitialCommit *Commit

	// TestFeature is the feature that the test verifies
	TestFeature testing.Feature

	MultiRepo
	TestType
}

// EvaluateSkipOptions compares the flags for this specific test case against
// the runtime options that were provided to the test suite. The test may be
// skipped based on the below set of rules.
func (optsStruct *New) EvaluateSkipOptions(t testing.NTB) {
	// Stress tests should run if and only if the --stress flag is specified.
	if !*e2e.Stress && optsStruct.StressTest {
		t.Skip("Test skipped since the stress test requires the '--stress' flag")
	}
	if *e2e.Stress && !optsStruct.StressTest {
		t.Skip("Test skipped since the '--stress' flag should only select stress tests")
	}

	// Profiling tests should run if and only if the --profiling flag is specified.
	if !*e2e.Profiling && optsStruct.ProfilingTest {
		t.Skip("Test skipped since the stress test requires the '--profiling' flag")
	}
	if *e2e.Profiling && !optsStruct.ProfilingTest {
		t.Skip("Test skipped since the '--profiling' flag should only select profiling tests")
	}

	// KCC tests should run if and only if the --kcc flag is specified.
	if !*e2e.KCC && optsStruct.KCCTest {
		t.Skip("Test skipped since the KCC test requires the '--kcc' flag")
	}
	if *e2e.KCC && !optsStruct.KCCTest {
		t.Skip("Test skipped since the '--kcc' flag should only select KCC tests")
	}

	// GCENode tests should run if and only if the --gcenode flag is specified.
	if !*e2e.GceNode && optsStruct.GCENodeTest {
		t.Skip("Test skipped since the GCENode test requires the '--gcenode' flag")
	}
	if *e2e.GceNode && !optsStruct.GCENodeTest {
		t.Skip("Test skipped since the '--gcenode' flag should only select GCENode tests")
	}

	// GitHub App tests should run if and only if the --githubapp flag is specified.
	if !*e2e.GitHubApp && optsStruct.GitHubAppTest {
		t.Skip("Test skipped since the GitHubApp test requires the '--githubapp' flag")
	}
	if *e2e.GitHubApp && !optsStruct.GitHubAppTest {
		t.Skip("Test skipped since the '--githubapp' flag should only select GitHubApp tests")
	}

	if *e2e.GitProvider != e2e.Local && optsStruct.RequireLocalGitProvider {
		t.Skip("Test skipped for non-local GitProvider types")
	}

	if *e2e.OCIProvider != e2e.Local && optsStruct.RequireLocalOCIProvider {
		t.Skip("Test skipped for non-local OCIProvider types")
	}

	if *e2e.HelmProvider != e2e.Local && optsStruct.RequireLocalHelmProvider {
		t.Skip("Test skipped for non-local HelmProvider types")
	}
}

// SkipAutopilotCluster will skip the test on the autopilot cluster.
func SkipAutopilotCluster(opt *New) {
	opt.SkipAutopilot = true
}

// RequireGKE requires the --test-cluster flag to be `gke` so that the test only runs on GKE clusters.
func RequireGKE(t testing.NTB) Opt {
	if *e2e.TestCluster != e2e.GKE {
		t.Skip("The --test-cluster flag must be set to `gke` to run this test.")
	}
	return func(_ *New) {}
}

// RequireKind requires the --test-cluster flag to be `kind` so that the test only runs on kind clusters.
func RequireKind(t testing.NTB) Opt {
	if *e2e.TestCluster != e2e.Kind {
		t.Skip("The --test-cluster flag must be set to `kind` to run this test.")
	}
	return func(_ *New) {}
}

// RequireCloudSourceRepository requires the --git-provider flag to be set to csr
func RequireCloudSourceRepository(t testing.NTB) Opt {
	if *e2e.GitProvider != e2e.CSR {
		t.Skip("The --git-provider flag must be set to `csr` to run this test.")
	}
	return func(_ *New) {}
}

// RequireSecureSourceManagerRepository requires the --git-provider flag to be set to ssm
func RequireSecureSourceManagerRepository(t testing.NTB) Opt {
	if *e2e.GitProvider != e2e.SSM {
		t.Skip("The --git-provider flag must be set to `ssm` to run this test.")
	}
	return func(_ *New) {}
}

// RequireHelmArtifactRegistry requires the --helm-provider flag to be set to `gar`.
// RequireHelmArtifactRegistry implies RequireHelmProvider.
func RequireHelmArtifactRegistry(t testing.NTB) Opt {
	if *e2e.HelmProvider != e2e.ArtifactRegistry {
		t.Skip("The --helm-provider flag must be set to `gar` to run this test.")
	}
	return func(opts *New) {
		opts.RequireHelmProvider = true
	}
}

// RequireOCIArtifactRegistry requires the --oci-provider flag to be set to `gar`.
// RequireOCIArtifactRegistry implies RequireOCIProvider.
func RequireOCIArtifactRegistry(t testing.NTB) Opt {
	if *e2e.OCIProvider != e2e.ArtifactRegistry {
		t.Skip("The --oci-provider flag must be set to `gar` to run this test.")
	}
	return func(opts *New) {
		opts.RequireOCIProvider = true
	}
}

// WithInitialCommit creates the initialCommit before the first sync
func WithInitialCommit(initialCommit Commit) func(opt *New) {
	return func(opt *New) {
		opt.InitialCommit = &initialCommit
	}
}

// SkipConfigSyncInstall skip installation of Config Sync components in cluster
func SkipConfigSyncInstall(opt *New) {
	opt.SkipConfigSyncInstall = true
}
