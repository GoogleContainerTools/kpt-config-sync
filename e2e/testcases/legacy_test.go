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
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"

	"kpt.dev/configsync/e2e/nomostest"
)

type BatsTest struct {
	fileName              string
	nomosDir              string
	skipMultiRepo         func(int) bool
	multiRepoIncompatible func(int) bool
	skipAutopilotCluster  func(int) bool
}

func (bt *BatsTest) batsPath() string {
	return filepath.Join(bt.nomosDir, filepath.FromSlash("third_party/bats-core/bin/bats"))
}

func (bt *BatsTest) Run(t *testing.T) {
	e2e.EnableParallel(t)
	tw := nomostesting.New(t)

	countCmd := exec.Command(bt.batsPath(), "--count", bt.fileName)
	out, err := countCmd.CombinedOutput()
	if err != nil {
		tw.Errorf("%v: %s", countCmd, string(out))
		tw.Fatal("Failed to get test count from bats:", err)
	}
	testCount, err := strconv.Atoi(strings.Trim(string(out), "\n"))
	if err != nil {
		tw.Fatalf("Failed to parse test count %q from bats: %v", out, err)
	}
	tw.Logf("Found %d testcases in %s", testCount, bt.fileName)
	for testNum := 1; testNum <= testCount; testNum++ {
		t.Run(strconv.Itoa(testNum), bt.runTest(testNum))
	}
}

func (bt *BatsTest) runTest(testNum int) func(t *testing.T) {
	return func(t *testing.T) {
		var opts []ntopts.Opt
		if bt.skipMultiRepo != nil && bt.skipMultiRepo(testNum) {
			opts = append(opts, ntopts.SkipMultiRepo)
		}
		if bt.multiRepoIncompatible != nil && bt.multiRepoIncompatible(testNum) {
			opts = append(opts, ntopts.MultiRepoIncompatible)
		}
		if bt.skipAutopilotCluster != nil && bt.skipAutopilotCluster(testNum) {
			opts = append(opts, ntopts.SkipAutopilotCluster)
		}
		nt := nomostest.New(t, opts...)

		// Factored out for accessing deprecated functions that only exist for supporting bats tests.
		privateKeyPath := nt.GitPrivateKeyPath() //nolint:staticcheck
		gitRepoPort := nt.GitRepoPort()          //nolint:staticcheck
		kubeConfigPath := nt.KubeconfigPath()    //nolint:staticcheck

		batsTmpDir := filepath.Join(nt.TmpDir, "bats", "tmp")
		batsHome := filepath.Join(nt.TmpDir, "bats", "home")
		for _, dir := range []string{batsTmpDir, batsHome} {
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				nt.T.Fatalf("failed to create dir %s for bats testing: %v", dir, err)
			}
		}

		pipeRead, pipeWrite, err := os.Pipe()
		if err != nil {
			nt.T.Fatal("failed to create pipe for test output", err)
		}
		defer func() {
			if pipeWrite != nil {
				_ = pipeWrite.Close()
			}
			_ = pipeRead.Close()
		}()
		cmd := exec.Command(bt.batsPath(), "--tap", bt.fileName)
		cmd.Stdout = pipeWrite
		cmd.Stderr = pipeWrite
		// Set fd3 (tap output) to stdout
		cmd.ExtraFiles = []*os.File{pipeWrite}
		cmd.Env = []string{
			// Indicate the exact test num to run.
			fmt.Sprintf("E2E_RUN_TEST_NUM=%d", testNum),
			// instruct bats to use the per-testcase temp directory rather than /tmp
			fmt.Sprintf("TMPDIR=%s", batsTmpDir),
			// instruct our e2e tests to report timing information
			"TIMING=true",
			// tell git to use the ssh private key and not check host key
			fmt.Sprintf("GIT_SSH_COMMAND=ssh -q -o StrictHostKeyChecking=no -i %s", privateKeyPath),
			// passes the path to e2e manifests to the bats tests
			fmt.Sprintf("MANIFEST_DIR=%s", filepath.Join(bt.nomosDir, filepath.FromSlash("e2e/raw-nomos/manifests"))),
			// passes the git server SSH port to bash tests
			fmt.Sprintf("FWD_SSH_PORT=%d", gitRepoPort),
			// for running 'nomos' command from built binary
			fmt.Sprintf("PATH=%s", os.Getenv("PATH")),
			// provide kubeconfig path to kubectl
			fmt.Sprintf("KUBECONFIG=%s", kubeConfigPath),
			// kubectl creates the .kube directory in HOME if it does not exist
			fmt.Sprintf("HOME=%s", batsHome),
		}
		if nt.MultiRepo {
			cmd.Env = append(cmd.Env, "CSMR=true")
		}

		nt.T.Log("Using environment")
		for _, env := range cmd.Env {
			nt.T.Logf("  %s", env)
		}

		nt.T.Logf("Starting legacy test %s", bt.fileName)
		err = cmd.Start()
		if err != nil {
			nt.T.Fatalf("failed to start command %s: %v", cmd, err)
		}

		// Close our copy of pipe so our read end of the pipe will get EOF when the subprocess terminates (and closes
		// it's copy of the write end).  Bats will still have the write end of the pipe hooked up to stdout/stderr/fd3
		// until it exits.
		_ = pipeWrite.Close()
		pipeWrite = nil

		reader := bufio.NewReader(pipeRead)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					if len(line) != 0 {
						nt.T.Log(string(line))
					}
					break
				}
				nt.T.Fatal("error reading from bats subprocess:", err)
			}
			nt.T.Log(strings.TrimRight(string(line), "\n"))
		}

		err = cmd.Wait()
		if err != nil {
			nt.T.Fatalf("command failed %s: %v", cmd, err)
		}
	}
}

func TestBats(t *testing.T) {
	if *e2e.GitProvider != e2e.Local {
		t.Skip("Skip running bats test on non-local git providers")
	}
	e2e.EnableParallel(t)
	allTests := func(int) bool {
		return true
	}
	testNums := func(nums ...int) func(int) bool {
		return func(num int) bool {
			for _, n := range nums {
				if n == num {
					return true
				}
			}
			return false
		}
	}

	nomosDir, err := filepath.Abs("../..")
	if err != nil {
		tw := nomostesting.New(t)
		tw.Fatal("Failed to get nomos dir: ", err)
	}

	// NOTE: eventually all skipped tests will need a note on exemption (see basic.bats as an example).
	// Once CSMR is the only implementation we ship, the skipped tests that are specific to legacy can be removed, and
	// all the skips can be removed.
	testCases := []*BatsTest{
		// Converted to acme_test.go.
		//{fileName: "acme.bats"},
		{fileName: "apiservice.bats"},
		{
			fileName: "basic.bats",
			multiRepoIncompatible: testNums(
				1, // tests internals of namespaceconfig
				2, // tests internals of syncs
				3, // tests internals of clusterconfigs
			),
			skipAutopilotCluster: testNums(10), // ignore the test that mutates the kube-system namespace
		},
		{
			fileName:      "cli.bats",
			skipMultiRepo: allTests, // TODO: implement nomos status in CLI, this may be a go rewrite
		},
		// Converted to cluster_resources_test.go.
		// {fileName: "cluster_resources.bats"},
		// Converted to custom_resource_definitions_test.go
		// {fileName: "custom_resource_definitions_v1.bats"},
		// Converted to custom_resource_definitions_test.go
		// {fileName: "custom_resource_definitions_v1beta1.bats"},
		// Converted to custom_resource_test.go
		// {fileName: "custom_resources_v1.bats"},
		// Converted to custom_resource_test.go
		// {fileName: "custom_resources_v1beta1.bats"},
		{fileName: "foo_corp.bats"},
		{
			fileName: "gatekeeper.bats",
			// TODO enable the test on autopilot clusters when GKE 1.21.3-gke.900 reaches regular/stable.
			skipAutopilotCluster: allTests,
		},
		// Converted to multiversion_test.go.
		// {fileName: "multiversion.bats"},
		// Converted to namespaces.go.
		// {
		//	fileName: "namespaces.bats",
		//	skipMultiRepo: testNums(
		//		1, // TODO: run again once polling period is lower
		//		5, // TODO: not clearing invalid management annotation
		//		6, // TODO: run again once polling period is lower
		//	),
		//	multiRepoIncompatible: testNums(
		//		3, // b/169155128 - namespace tombstoning that was used in MonoRepo for status, we decided to not implement this for CSMR
		//	),
		// },
		// Converted to policy_dir_test.go.
		//{fileName: "operator-no-policy-dir.bats"},
		// Converted to cluster_selectors_test.go.
		// {fileName: "per_cluster_addressing.bats"},
		// Converted to preserve_fields_test.go.
		// {fileName: "preserve_fields.bats"},
		{
			fileName:      "repoless.bats",
			skipMultiRepo: allTests, // TODO: adjust control knobs for CSMR
		},
		// Converted to resource_conditions_test.go.
		// {fileName: "resource_conditions.bats"},
		// Converted to policy_dir_test.go.
		//{fileName: "status_monitoring.bats"},
	}
	for idx := range testCases {
		tc := testCases[idx]
		tc.nomosDir = nomosDir
		t.Run(tc.fileName, tc.Run)
	}
}
