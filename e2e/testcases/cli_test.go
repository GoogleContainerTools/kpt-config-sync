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
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func recursiveDiff(file1, file2 string) ([]byte, error) {
	out, err := exec.Command("diff",
		"-B",         // Ignore empty lines (e.g. space after license)
		"-I", "^#.*", // Ignore comments (e.g. licenses)
		"-r", file1, file2).CombinedOutput()
	return out, err
}

func TestNomosInitVet(t *testing.T) {
	// Ensure that the following sequence of commands succeeds:
	//
	// 1) git init
	// 2) nomos init
	// 3) nomos vet --no-api-server-check
	tw, opts := nomostest.NewTestWrapper(t, nomostesting.NomosCLI)
	tmpDir := opts.TmpDir

	out, err := exec.Command("git", "init", tmpDir).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("nomos", "init", fmt.Sprintf("--path=%s", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("nomos", "vet", "--no-api-server-check", fmt.Sprintf("--path=%s", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}
}

func TestNomosInitHydrate(t *testing.T) {
	// Ensure that the following sequence of commands succeeds:
	//
	// 1) git init
	// 2) nomos init
	// 3) nomos vet --no-api-server-check
	// 4) nomos hydrate --no-api-server-check
	// 5) nomos vet --no-api-server-check --path=<hydrated-dir>
	tw, opts := nomostest.NewTestWrapper(t, nomostesting.NomosCLI)
	tmpDir := opts.TmpDir

	out, err := exec.Command("git", "init", tmpDir).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("nomos", "init", fmt.Sprintf("--path=%s", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	err = hydrate.PrintFile(fmt.Sprintf("%s/namespaces/foo/ns.yaml", tmpDir),
		flags.OutputYAML,
		[]*unstructured.Unstructured{
			fake.UnstructuredObject(kinds.Namespace(), core.Name("foo"), core.Annotation(metadata.HNCManagedBy, "controller1")),
			fake.UnstructuredObject(kinds.ConfigMap(), core.Name("cm1"), core.Namespace("foo")),
		})
	if err != nil {
		tw.Fatal(err)
	}

	out, err = exec.Command("nomos", "vet", "--no-api-server-check", fmt.Sprintf("--path=%s", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("nomos", "hydrate", "--no-api-server-check",
		fmt.Sprintf("--path=%s", tmpDir), fmt.Sprintf("--output=%s/compiled", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("nomos", "hydrate", "--no-api-server-check", "--flat",
		fmt.Sprintf("--path=%s", tmpDir), fmt.Sprintf("--output=%s/compiled.yaml", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	out, err = exec.Command("cat", fmt.Sprintf("%s/compiled.yaml", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}

	expectedYaml := []byte(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
  namespace: foo
---
apiVersion: v1
kind: Namespace
metadata:
  annotations:
    hnc.x-k8s.io/managed-by: controller1
  name: foo
`)

	if diff := cmp.Diff(string(expectedYaml), string(out)); diff != "" {
		tw.Errorf("nomos hydrate diff: %s", diff)
	}

	out, err = exec.Command("nomos", "vet", "--no-api-server-check", "--source-format=unstructured",
		fmt.Sprintf("--path=%s/compiled", tmpDir)).CombinedOutput()
	if err != nil {
		tw.Log(string(out))
		tw.Error(err)
	}
}

func TestNomosHydrateWithInvalidNamespaceSelectorsHierarchical(t *testing.T) {
	configPath := "../testdata/invalid-hierarchy-namespace-selectors"
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)
	args := []string{
		"vet",
		"--no-api-server-check",
		"--path", configPath,
	}

	out, err := nt.Shell.Command("nomos", args...).CombinedOutput()
	assert.Error(t, err)
	msg := string(out)
	assert.Contains(t, msg, "KNV1014: NamespaceSelector MUST NOT use the dynamic mode with the hierarchy source format")
	assert.Contains(t, msg, "KNV1014: Unknown mode defined in NamespaceSelector")

	args = []string{
		"hydrate",
		"--no-api-server-check",
		"--path", configPath,
		"--output", nt.TmpDir,
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	assert.Error(t, err)
	msg = string(out)
	assert.Contains(t, msg, "KNV1014: NamespaceSelector MUST NOT use the dynamic mode with the hierarchy source format")
	assert.Contains(t, msg, "KNV1014: Unknown mode defined in NamespaceSelector")
}

func TestNomosHydrateWithInvalidNamespaceSelectorsUnstructured(t *testing.T) {
	configPath := "../testdata/invalid-unstructured-namespace-selectors"
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)
	args := []string{
		"vet",
		"--no-api-server-check",
		"--path", configPath,
		"--source-format", string(filesystem.SourceFormatUnstructured),
	}

	out, err := nt.Shell.Command("nomos", args...).CombinedOutput()
	assert.Error(t, err)
	msg := string(out)
	assert.NotContains(t, msg, "KNV1014: NamespaceSelector MUST NOT use the dynamic mode with the hierarchy source format")
	assert.Contains(t, msg, "KNV1014: Unknown mode defined in NamespaceSelector")

	args = []string{
		"hydrate",
		"--no-api-server-check",
		"--path", configPath,
		"--source-format", string(filesystem.SourceFormatUnstructured),
		"--output", nt.TmpDir,
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	assert.Error(t, err)
	msg = string(out)
	assert.NotContains(t, msg, "KNV1014: NamespaceSelector MUST NOT use the dynamic mode with the hierarchy source format")
	assert.Contains(t, msg, "KNV1014: Unknown mode defined in NamespaceSelector")
}

func TestNomosHydrateWithClusterSelectorsHierarchical(t *testing.T) {
	configPath := "../../examples/hierarchy-repo-with-cluster-selectors"
	testNomosHydrateWithClusterSelectors(t, configPath, filesystem.SourceFormatHierarchy)
}

func TestNomosHydrateWithClusterSelectorsDefaultSourceFormat(t *testing.T) {
	configPath := "../../examples/hierarchy-repo-with-cluster-selectors"
	testNomosHydrateWithClusterSelectors(t, configPath, "")
}

func TestNomosHydrateWithClusterSelectorsUnstructured(t *testing.T) {
	configPath := "../../examples/unstructured-repo-with-cluster-selectors"
	testNomosHydrateWithClusterSelectors(t, configPath, filesystem.SourceFormatUnstructured)
}

func testNomosHydrateWithClusterSelectors(t *testing.T, configPath string, sourceFormat filesystem.SourceFormat) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)

	expectedCompiledDir := "../../examples/repo-with-cluster-selectors-compiled"
	compiledDir := fmt.Sprintf("%s/%s/compiled", nt.TmpDir, sourceFormat)
	clusterDevCompiledDir := fmt.Sprintf("%s/cluster-dev", compiledDir)
	clusterStagingCompiledDir := fmt.Sprintf("%s/cluster-staging", compiledDir)
	clusterProdCompiledDir := fmt.Sprintf("%s/cluster-prod", compiledDir)

	compiledWithAPIServerCheckDir := fmt.Sprintf("%s/%s/compiled-with-api-server-check", nt.TmpDir, sourceFormat)

	compiledDirWithoutClustersFlag := fmt.Sprintf("%s/%s/compiled-without-clusters-flag", nt.TmpDir, sourceFormat)
	expectedCompiledWithoutClustersFlagDir := "../../examples/repo-with-cluster-selectors-compiled-without-clusters-flag"

	compiledJSONDir := fmt.Sprintf("%s/%s/compiled-json", nt.TmpDir, sourceFormat)
	compiledJSONWithoutClustersFlagDir := fmt.Sprintf("%s/%s/compiled-json-without-clusters-flag", nt.TmpDir, sourceFormat)
	expectedCompiledJSONDir := "../../examples/repo-with-cluster-selectors-compiled-json"
	expectedCompiledWithoutClustersFlagJSONDir := "../../examples/repo-with-cluster-selectors-compiled-json-without-clusters-flag"

	// Test `nomos vet --no-api-server-check`
	args := []string{
		"vet",
		"--no-api-server-check",
		"--path", configPath,
	}

	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err := nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos vet --no-api-server-check --clusters=cluster-dev`
	args = []string{
		"vet",
		"--no-api-server-check",
		"--path", configPath,
		"--clusters=cluster-dev",
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos vet`
	args = []string{
		"vet",
		"--path", configPath,
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos hydrate --no-api-server-check --clusters=cluster-dev,cluster-prod,cluster-staging`
	args = []string{
		"hydrate",
		"--no-api-server-check",
		"--path", configPath,
		"--clusters", "cluster-dev,cluster-prod,cluster-staging",
		"--output", compiledDir,
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledDir, expectedCompiledDir)
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos hydrate --clusters=cluster-dev,cluster-prod,cluster-staging`
	args = []string{
		"hydrate",
		"--path", configPath,
		"--clusters", "cluster-dev,cluster-prod,cluster-staging",
		"--output", compiledWithAPIServerCheckDir,
	}

	if sourceFormat == filesystem.SourceFormatUnstructured {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledWithAPIServerCheckDir, expectedCompiledDir)
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos hydrate`
	args = []string{
		"hydrate",
		"--path", configPath,
		"--output", compiledDirWithoutClustersFlag,
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledDirWithoutClustersFlag, expectedCompiledWithoutClustersFlagDir)
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(fmt.Sprintf("%s/cluster-dev", compiledDirWithoutClustersFlag), fmt.Sprintf("%s/cluster-dev", compiledDir))
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(fmt.Sprintf("%s/cluster-staging", compiledDirWithoutClustersFlag), fmt.Sprintf("%s/cluster-staging", compiledDir))
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(fmt.Sprintf("%s/cluster-prod", compiledDirWithoutClustersFlag), fmt.Sprintf("%s/cluster-prod", compiledDir))
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos hydrate --format=json --clusters=cluster-dev,cluster-prod,cluster-staging`
	args = []string{
		"hydrate",
		"--format=json",
		"--path", configPath,
		"--clusters", "cluster-dev,cluster-prod,cluster-staging",
		"--output", compiledJSONDir,
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledJSONDir, expectedCompiledJSONDir)
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos hydrate --format=json`
	args = []string{
		"hydrate",
		"--format=json",
		"--path", configPath,
		"--output", compiledJSONWithoutClustersFlagDir,
	}
	if sourceFormat != "" {
		args = append(args, "--source-format", string(sourceFormat))
	}
	out, err = nt.Shell.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledJSONWithoutClustersFlagDir, expectedCompiledWithoutClustersFlagJSONDir)
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos vet --no-api-server-check --source-format=unstructured` on the hydrated configs
	out, err = nt.Shell.Command("nomos", "vet", "--no-api-server-check", "--source-format=unstructured", "--path", clusterDevCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = nt.Shell.Command("nomos", "vet", "--no-api-server-check", "--source-format=unstructured", "--path", clusterStagingCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = nt.Shell.Command("nomos", "vet", "--no-api-server-check", "--source-format=unstructured", "--path", clusterProdCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos vet --source-format=unstructured` on the hydrated configs
	out, err = nt.Shell.Command("nomos", "vet", "--source-format=unstructured", "--path", clusterDevCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = nt.Shell.Command("nomos", "vet", "--source-format=unstructured", "--path", clusterStagingCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = nt.Shell.Command("nomos", "vet", "--source-format=unstructured", "--path", clusterProdCompiledDir).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}
}

func testSyncFromNomosHydrateOutput(nt *nomostest.NT, config string) {
	if err := nt.ValidateNotFound("bookstore1", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("bookstore2", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy(config, "acme"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add cluster-dev configs"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("bookstore1", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("bookstore2", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("quota", "bookstore1", &corev1.ResourceQuota{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("quota", "bookstore2", &corev1.ResourceQuota{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("cm-all", "bookstore1", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("cm-dev-staging", "bookstore1", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("cm-prod", "bookstore1", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("cm-dev", "bookstore1", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("cm-disabled", "bookstore1", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("cm-all", "bookstore2", &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
}

func TestSyncFromNomosHydrateOutputYAMLDir(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.Unstructured)
	testSyncFromNomosHydrateOutput(nt, "../../examples/repo-with-cluster-selectors-compiled/cluster-dev/.")
}

func TestSyncFromNomosHydrateOutputJSONDir(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.Unstructured)
	testSyncFromNomosHydrateOutput(nt, "../../examples/repo-with-cluster-selectors-compiled-json/cluster-dev/.")
}

func testSyncFromNomosHydrateOutputFlat(t *testing.T, sourceFormat filesystem.SourceFormat, outputFormat string) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.Unstructured)

	configPath := fmt.Sprintf("../../examples/%s-repo-with-cluster-selectors", sourceFormat)
	compiledConfigFile := fmt.Sprintf("%s/compiled.%s", nt.TmpDir, outputFormat)

	args := []string{
		"hydrate",
		"--no-api-server-check",
		"--flat",
		"--path", configPath,
		"--format", outputFormat,
		"--clusters=cluster-dev",
		"--output", compiledConfigFile,
	}

	if sourceFormat == filesystem.SourceFormatUnstructured {
		args = append(args, "--source-format", string(sourceFormat))
	}

	out, err := exec.Command("nomos", args...).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	testSyncFromNomosHydrateOutput(nt, compiledConfigFile)
}

func TestSyncFromNomosHydrateHierarchicalOutputWithClusterSelectorJSONFlat(t *testing.T) {
	testSyncFromNomosHydrateOutputFlat(t, filesystem.SourceFormatHierarchy, "json")
}

func TestSyncFromNomosHydrateUnstructuredOutputWithClusterSelectorJSONFlat(t *testing.T) {
	testSyncFromNomosHydrateOutputFlat(t, filesystem.SourceFormatUnstructured, "json")
}

func TestSyncFromNomosHydrateHierarchicalOutputWithClusterSelectorYAMLFlat(t *testing.T) {
	testSyncFromNomosHydrateOutputFlat(t, filesystem.SourceFormatHierarchy, "yaml")
}

func TestSyncFromNomosHydrateUnstructuredOutputWithClusterSelectorYAMLFlat(t *testing.T) {
	testSyncFromNomosHydrateOutputFlat(t, filesystem.SourceFormatUnstructured, "yaml")
}

func TestNomosHydrateWithUnknownScopedObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)

	compiledDirWithoutAPIServerCheck := fmt.Sprintf("%s/compiled-without-api-server-check", nt.TmpDir)
	compiledDirWithAPIServerCheck := fmt.Sprintf("%s/compiled-with-api-server-check", nt.TmpDir)

	kubevirtPath := "../../examples/kubevirt"

	// Test `nomos vet --no-api-server-check`
	out, err := nt.Shell.Command("nomos", "vet", "--source-format=unstructured", "--no-api-server-check", fmt.Sprintf("--path=%s", kubevirtPath)).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	expectedErrorPrefix := `KNV1021: No CustomResourceDefinition is defined for the type "VirtualMachine.kubevirt.io" in the cluster.`

	// Verify that `nomos vet` returns a KNV1021 error.
	out, err = nt.Shell.Command("nomos", "vet", "--source-format=unstructured", fmt.Sprintf("--path=%s", kubevirtPath)).CombinedOutput()
	if err == nil {
		nt.T.Error(fmt.Errorf("`nomos vet --path=%s` expects an error, got nil", kubevirtPath))
	} else {
		if !strings.HasPrefix(string(out), "Error: "+expectedErrorPrefix) {
			nt.T.Error(fmt.Errorf("`nomos vet --path=%s` expects only one KNV1021 error, got %v", kubevirtPath, string(out)))
		}
	}

	// Verify that `nomos hydrate --no-api-server-check` generates no error, and the output dir includes all the objects no matter their scopes.
	out, err = nt.Shell.Command("nomos", "hydrate", "--source-format=unstructured", "--no-api-server-check",
		fmt.Sprintf("--path=%s", "../../examples/kubevirt"),
		fmt.Sprintf("--output=%s", compiledDirWithoutAPIServerCheck)).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	out, err = recursiveDiff(compiledDirWithoutAPIServerCheck, "../../examples/kubevirt-compiled")
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Verify that `nomos hydrate` generates a KNV1021 error, and the output dir includes all the objects no matter their scopes.
	out, err = nt.Shell.Command("nomos", "hydrate", "--source-format=unstructured",
		fmt.Sprintf("--path=%s", "../../examples/kubevirt"),
		fmt.Sprintf("--output=%s", compiledDirWithAPIServerCheck)).CombinedOutput()
	if err == nil {
		nt.T.Error(fmt.Errorf("`nomo hydrate --path=%s` expects an error, got nil", kubevirtPath))
	} else {
		if !strings.HasPrefix(string(out), `errors for Cluster "defaultcluster": `+expectedErrorPrefix) {
			nt.T.Error(fmt.Errorf("`nomos hydrate --path=%s` expects only one KNV1021 error, got %v", kubevirtPath, string(out)))
		}
	}

	out, err = recursiveDiff(compiledDirWithAPIServerCheck, "../../examples/kubevirt-compiled")
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Test `nomos vet --no-api-server-check` on the hydrated configs.
	out, err = nt.Shell.Command("nomos", "vet", "--no-api-server-check", "--source-format=unstructured", fmt.Sprintf("--path=%s", compiledDirWithoutAPIServerCheck)).CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Error(err)
	}

	// Verify that `nomos vet` on the hydrated configs returns a KNV1021 error.
	out, err = nt.Shell.Command("nomos", "vet", "--source-format=unstructured", fmt.Sprintf("--path=%s", compiledDirWithoutAPIServerCheck)).CombinedOutput()
	if err == nil {
		nt.T.Error(fmt.Errorf("`nomos vet --path=%s` expects an error, got nil", compiledDirWithoutAPIServerCheck))
	} else {
		if !strings.HasPrefix(string(out), "Error: "+expectedErrorPrefix) {
			nt.T.Error(fmt.Errorf("`nomos vet --path=%s` expects only one KNV1021 error, got %v", compiledDirWithoutAPIServerCheck, string(out)))
		}
	}
}

func TestNomosHydrateAndVetDryRepos(t *testing.T) {
	testCases := []struct {
		name            string
		path            string
		outPath         string
		sourceFormat    string
		outFormat       string
		expectedOutPath string
		expectedErrMsg  string
	}{
		{
			name:           "invalid output format",
			outFormat:      "invalid",
			expectedErrMsg: fmt.Sprintf("format argument must be %q or %q", flags.OutputYAML, flags.OutputJSON),
		},
		{
			name:           "must use 'unstructured' format for DRY repos",
			path:           "../testdata/hydration/helm-components",
			sourceFormat:   string(filesystem.SourceFormatHierarchy),
			expectedErrMsg: fmt.Sprintf("%s must be %s when Kustomization is needed", reconcilermanager.SourceFormat, filesystem.SourceFormatUnstructured),
		},
		{
			name:           "hydrate error: a DRY repo without kustomization.yaml",
			path:           "../testdata/hydration/dry-repo-without-kustomization",
			sourceFormat:   string(filesystem.SourceFormatUnstructured),
			expectedErrMsg: `Object 'Kind' is missing in`,
		},
		{
			name:           "hydrate error: deprecated Group and Kind",
			path:           "../testdata/hydration/deprecated-GK",
			sourceFormat:   string(filesystem.SourceFormatUnstructured),
			expectedErrMsg: "The config is using a deprecated Group and Kind. To fix, set the Group and Kind to \"Deployment.apps\"",
		},
		{
			name:           "hydrate error: duplicate resources",
			path:           "../testdata/hydration/resource-duplicate",
			sourceFormat:   string(filesystem.SourceFormatUnstructured),
			expectedErrMsg: "may not add resource with an already registered id",
		},
		{
			name:            "hydrate a DRY repo with helm components",
			path:            "../testdata/hydration/helm-components",
			outPath:         "helm-components/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/compiled/helm-components",
		},
		{
			name:            "hydrate a DRY repo with kustomize components",
			path:            "../testdata/hydration/kustomize-components",
			outPath:         "kustomize-components/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/compiled/kustomize-components",
		},
		{
			name:            "hydrate a DRY repo with helm overlay",
			path:            "../testdata/hydration/helm-overlay",
			outPath:         "helm-overlay/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/compiled/helm-overlay",
		},
		{
			name:            "hydrate a DRY repo with remote base",
			path:            "../testdata/hydration/remote-base",
			outPath:         "remote-base/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/compiled/remote-base",
		},
		{
			name:            "hydrate a DRY repo with relative path",
			path:            "../testdata/hydration/relative-path/overlays/dev",
			outPath:         "relative-path/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/compiled/relative-path",
		},
		{
			name:            "hydrate a WET repo",
			path:            "../testdata/hydration/wet-repo",
			outPath:         "wet-repo/compiled",
			sourceFormat:    string(filesystem.SourceFormatUnstructured),
			expectedOutPath: "../testdata/hydration/wet-repo",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tw, opts := nomostest.NewTestWrapper(t, nomostesting.NomosCLI)
			tmpDir := opts.TmpDir
			outputPath := filepath.Join(tmpDir, flags.DefaultHydrationOutput)
			args := []string{"--no-api-server-check"}
			if len(tc.sourceFormat) > 0 {
				args = append(args, "--source-format", tc.sourceFormat)
			}
			if len(tc.path) > 0 {
				args = append(args, "--path", tc.path)
			}
			if len(tc.outFormat) > 0 {
				args = append(args, "--format", tc.outFormat)
			}
			if len(tc.outPath) > 0 {
				outputPath = filepath.Join(tmpDir, tc.outPath)
				args = append(args, "--output", outputPath)
			}
			if err := os.MkdirAll(outputPath, 0755); err != nil {
				t.Fatal(err)
			}
			// test 'nomos hydrate'
			hydrateArgs := []string{"hydrate"}
			hydrateArgs = append(hydrateArgs, args...)
			out, err := exec.Command("nomos", hydrateArgs...).CombinedOutput()

			// 'nomos hydrate' and 'nomos vet' might pull remote Helm charts locally.
			// Below deletes the generated charts after the test.
			chartsDir := filepath.Join(tc.path, "charts")
			if _, err := os.Stat(chartsDir); os.IsNotExist(err) {
				defer func() {
					_ = os.RemoveAll(chartsDir)
				}()
			}
			if len(tc.expectedErrMsg) != 0 && err == nil {
				tw.Errorf("%s: expected error '%s', but got no error", tc.name, tc.expectedErrMsg)
			}
			if len(tc.expectedErrMsg) == 0 && err != nil {
				tw.Errorf("%s: expected no error, but got '%s'", tc.name, string(out))
			}
			if len(tc.expectedErrMsg) != 0 && !strings.Contains(string(out), tc.expectedErrMsg) {
				tw.Errorf("%s: expected error '%s', but got '%s'", tc.name, tc.expectedErrMsg, string(out))
			}

			if len(tc.expectedErrMsg) == 0 {
				out, err = recursiveDiff(outputPath, tc.expectedOutPath)
				if err != nil {
					tw.Log(string(out))
					tw.Errorf("%s: %v", tc.name, err)
				}
			}

			// test 'nomos vet'
			args = append(args, "--keep-output")
			// test JSON output format
			if tc.outFormat == "" || tc.outFormat == flags.OutputYAML {
				args = append(args, "--format", flags.OutputJSON)
			}
			// use a different output folder
			outputPath = strings.ReplaceAll(outputPath, "compiled", "compiled-json")
			if err := os.MkdirAll(outputPath, 0755); err != nil {
				t.Fatal(err)
			}
			args = append(args, "--output", outputPath)

			vetArgs := []string{"vet"}
			vetArgs = append(vetArgs, args...)
			out, err = exec.Command("nomos", vetArgs...).CombinedOutput()
			if len(tc.expectedErrMsg) != 0 && err == nil {
				tw.Errorf("%s: expected error '%s', but got no error", tc.name, tc.expectedErrMsg)
			}
			if len(tc.expectedErrMsg) == 0 && err != nil {
				tw.Errorf("%s: expected no error, but got '%s'", tc.name, string(out))
			}
			if len(tc.expectedErrMsg) != 0 && !strings.Contains(string(out), tc.expectedErrMsg) {
				tw.Errorf("%s: expected error '%s', but got '%s'", tc.name, tc.expectedErrMsg, string(out))
			}

			if len(tc.expectedErrMsg) == 0 {
				// update the expected output folder
				tc.expectedOutPath = strings.ReplaceAll(tc.expectedOutPath, "compiled", "compiled-json")
				if strings.Contains(outputPath, "wet-repo") {
					tc.expectedOutPath = "../testdata/hydration/compiled-json/wet-repo"
				}
				out, err = recursiveDiff(outputPath, tc.expectedOutPath)
				if err != nil {
					tw.Log(string(out))
					tw.Errorf("%s: %v", tc.name, err)
				}
			}
		})
	}
}

func TestNomosVetNamespaceRepo(t *testing.T) {
	testCases := []struct {
		name           string
		namespace      string
		path           string
		sourceFormat   string
		expectedErrMsg string
	}{
		{
			name:           "nomos vet a namespace repo should fail when source-format is set to hierarchy",
			namespace:      "tenant-a",
			sourceFormat:   string(filesystem.SourceFormatHierarchy),
			expectedErrMsg: "Error: if --namespace is provided, --source-format must be omitted or set to unstructured",
		},
		{
			name:      "nomos vet should automatically validate a namespace repo with the unstructured mode if source-format is not set",
			namespace: "tenant-a",
			path:      "../testdata/hydration/compiled/remote-base/tenant-a",
		},
		{
			name:         "nomos vet should automatically validate a namespace repo with the unstructured mode if source-format is set to unstructured",
			namespace:    "tenant-a",
			path:         "../testdata/hydration/compiled/remote-base/tenant-a",
			sourceFormat: string(filesystem.SourceFormatUnstructured),
		},
		{
			name:      "nomos vet should validate a DRY namespace repo",
			namespace: "test-ns",
			path:      "../testdata/hydration/namespace-repo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tw, _ := nomostest.NewTestWrapper(t, nomostesting.NomosCLI)
			args := []string{"vet", "--no-api-server-check", "--namespace", tc.namespace}
			if tc.sourceFormat != "" {
				args = append(args, "--source-format", tc.sourceFormat)
			}
			if tc.path != "" {
				args = append(args, "--path", tc.path)
			}

			out, err := exec.Command("nomos", args...).CombinedOutput()
			if len(tc.expectedErrMsg) != 0 && err == nil {
				tw.Errorf("%s: expected error '%s', but got no error", tc.name, tc.expectedErrMsg)
			}
			if len(tc.expectedErrMsg) == 0 && err != nil {
				tw.Errorf("%s: expected no error, but got '%s'", tc.name, string(out))
			}
			if len(tc.expectedErrMsg) != 0 && !strings.Contains(string(out), tc.expectedErrMsg) {
				tw.Errorf("%s: expected error '%s', but got '%s'", tc.name, tc.expectedErrMsg, string(out))
			}
		})
	}
}

// TestNomosBugreport
//
// tests the `nomos bugreport` by running it against a
// cluster and making sure that the right minimum set of files
// is output.  Changing the contents of the archive will make
// this test fail.
func TestNomosBugreport(t *testing.T) {
	var bugReportZipName, bugReportDirName string
	nt := nomostest.New(t, nomostesting.NomosCLI)

	// get bugreport
	cmd := nt.Shell.Command("nomos", "bugreport")
	cmd.Dir = nt.TmpDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	// locate zip file
	bugReportZipName, err = getBugReportZipName(nt.TmpDir, nt)
	if err != nil {
		nt.T.Fatal(err)
	}

	// unzip
	bugReportDirName = strings.TrimSuffix(bugReportZipName, filepath.Ext(bugReportZipName))
	err = unzip(nt.TmpDir, bugReportZipName)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log(fmt.Sprintf("unzipped bugreport to %s", bugReportDirName))

	// get current cluster context
	out, err = nt.Shell.Command("kubectl", "config", "current-context").CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	context := strings.TrimSpace(string(out))

	// gather all of the files that are generally
	// collected by nomos bugreport.
	// if you do add additional file extensions these need to be added here
	extensions := []string{".txt", ".json", ".yaml", ".log"}
	files := glob(bugReportDirName, func(s string) bool {
		extension := filepath.Ext(s)
		return slices.Contains(extensions, extension)
	})

	generalBugReportFiles := []string{"version.txt", "status.txt"}

	multiRepoBugReportFiles := []string{
		"cluster/configmanagement/clusterselectors.json",
		"cluster/configmanagement/clusterselectors.yaml",
		"cluster/configmanagement/namespaceselectors.json",
		"cluster/configmanagement/namespaceselectors.yaml",
		"cluster/configmanagement/config-sync-validating-webhhook-configuration.json",
		"cluster/configmanagement/config-sync-validating-webhhook-configuration.yaml",
		"namespaces/kube-system/pods.json",
		"namespaces/kube-system/pods.yaml",
		"namespaces/config-management-system/pods.json",
		"namespaces/config-management-system/pods.yaml",
		"namespaces/config-management-system/ConfigMaps.json",
		"namespaces/config-management-system/ConfigMaps.yaml",
		"namespaces/config-management-system/ResourceGroup-root-sync.json",
		"namespaces/config-management-system/ResourceGroup-root-sync.yaml",
		"namespaces/config-management-system/RootSync-root-sync.json",
		"namespaces/config-management-system/RootSync-root-sync.yaml",
		"namespaces/config-management-system/root-reconciler.*/git-sync.log",
		"namespaces/config-management-system/root-reconciler.*/otel-agent.log",
		"namespaces/config-management-system/root-reconciler.*/reconciler.log",
		"namespaces/config-management-monitoring/pods.json",
		"namespaces/config-management-monitoring/pods.yaml",
		"namespaces/config-management-monitoring/otel-collector.*/otel-collector.log",
		"namespaces/gatekeeper-system/pods.json",
		"namespaces/gatekeeper-system/pods.yaml",
		"namespaces/resource-group-system/pods.json",
		"namespaces/resource-group-system/pods.yaml",
	}

	// check expected files exist in folder
	var errs status.MultiError
	errs = checkFileExists(fmt.Sprintf("%s/processed/%s", bugReportDirName, context), generalBugReportFiles, files)
	errs = status.Append(errs, checkFileExists(fmt.Sprintf("%s/raw/%s", bugReportDirName, context), multiRepoBugReportFiles, files))
	if errs != nil {
		nt.T.Fatal(fmt.Sprintf("did not find all expected files in bug report zip file: %v", errs))
	}
	nt.T.Log("Found all expected files in bugreport zip")
}

// TestNomosImage makes sure that the nomos image is produced and it contains
// the nomos binary and basic dependencies.
func TestNomosImage(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI,
		ntopts.SkipConfigSyncInstall, ntopts.RequireKind(t))

	version := nomostest.VersionFromManifest(t)

	out, err := nt.Shell.ExecWithDebug("docker", "run", "-i", "--rm",
		fmt.Sprintf("%s/nomos:%s", e2e.DefaultImagePrefix, version))
	if err != nil {
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out), version) {
		nt.T.Fatalf("expected to find version string in output:\n%s\n", string(out))
	}

	out, err = nt.Shell.ExecWithDebug("docker", "run", "--rm", "--entrypoint", "kustomize",
		fmt.Sprintf("%s/nomos:%s", e2e.DefaultImagePrefix, version), "version")
	if err != nil {
		nt.T.Fatal(err)
	}
	if !strings.Contains(string(out), hydrate.KustomizeVersion) {
		nt.T.Fatalf("expected to find kustomize version string %s in output:\n%s\n",
			hydrate.KustomizeVersion, string(out))
	}

	out, err = nt.Shell.ExecWithDebug("docker", "run", "--rm", "--entrypoint", "helm",
		fmt.Sprintf("%s/nomos:%s", e2e.DefaultImagePrefix, version), "version")
	if err != nil {
		nt.T.Fatal(err)
	}
	if !strings.Contains(string(out), hydrate.HelmVersion) {
		nt.T.Fatalf("expected to find helm version string %s in output:\n%s\n",
			hydrate.HelmVersion, string(out))
	}
}

// getBugReportZipName find and returns the zip name of bugreport under test dir
// or error if no bugreport zip found
func getBugReportZipName(dir string, nt *nomostest.NT) (string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		nt.T.Fatal(err)
	}

	for _, file := range files {
		bugReportRegex, _ := regexp.Compile("bug_report_.*.zip")
		if bugReportRegex.MatchString(file.Name()) {
			nt.T.Logf("found zip file %s", file.Name())
			return fmt.Sprintf("%s/%s", dir, file.Name()), nil
		}
	}
	return "", fmt.Errorf("could not find bugreport zip file in test directory")
}

// unzip all files in bugreport zip to its dir
func unzip(dir, zipName string) error {
	archive, err := zip.OpenReader(zipName)
	if err != nil {
		return err
	}
	defer func(archive *zip.ReadCloser) {
		err := archive.Close()
		if err != nil {
			klog.Fatal(err)
		}
	}(archive)

	for _, f := range archive.File {
		klog.Infof(fmt.Sprintf("processing and unzipping %s", f.Name))
		filePath := path.Join(dir, f.Name)
		if f.FileInfo().IsDir() {
			fmt.Println("creating directory...")
			if err := os.MkdirAll(filePath, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return err
		}

		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		fileInArchive, err := f.Open()
		if err != nil {
			return err
		}

		if _, err := io.Copy(dstFile, fileInArchive); err != nil {
			return err
		}

		_ = dstFile.Close()
		_ = fileInArchive.Close()
	}
	return nil
}

// glob find and return all files matching given pattern
func glob(dir string, fn func(string) bool) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(s string, _ fs.DirEntry, _ error) error {
		if fn(s) {
			files = append(files, s)
		}
		return nil
	})
	return files
}

// checkFileExists check if all files in targetFiles with path prefix can be found in allFiles
func checkFileExists(prefix string, targetFiles, allFiles []string) status.MultiError {
	var err status.MultiError
	for _, targetFile := range targetFiles {
		found := false
		for _, file := range allFiles {
			if match, _ := regexp.MatchString(fmt.Sprintf("%s/%s", prefix, targetFile), file); match {
				found = true
				break
			}
		}
		if !found {
			err = status.Append(err, fmt.Errorf("file not found %s", targetFile))
		}
	}
	return err
}

func TestNomosStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI)

	// get status
	cmd := nt.Shell.Command("nomos", "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out), "SYNCED") {
		nt.T.Fatalf("Expected to find sync status in string output:\n%s\n", string(out))
	}
}

func TestNomosVersion(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI)

	// get version
	cmd := nt.Shell.Command("nomos", "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out), "config-sync") {
		nt.T.Fatalf("Expected to find config-sync component in output:\n%s\n", string(out))
	}
}

func TestNomosStatusNameFilter(t *testing.T) {
	bookinfoNS := "bookinfo"
	bookinfoRS := "bookinfo-repo-sync"
	crontab := "crontab-sync"
	bookRepo := nomostest.RepoSyncNN(bookinfoNS, bookinfoRS)
	crontabRepo := nomostest.RepoSyncNN(bookinfoNS, crontab)
	nt := nomostest.New(
		t,
		nomostesting.NomosCLI,
		ntopts.Unstructured,
		ntopts.RootRepo(crontab),
		ntopts.RepoSyncPermissions(policy.RepoSyncAdmin()),
		ntopts.NamespaceRepo(bookRepo.Namespace, bookRepo.Name),
		ntopts.NamespaceRepo(crontabRepo.Namespace, crontabRepo.Name),
	)

	// get status with only name filter crontab-sync
	// expect to find crontab-sync root-sync and crontab-sync repo-sync
	cmd := nt.Shell.Command("nomos", "status", "--name", "crontab-sync")
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out), "<root>:crontab-sync") {
		nt.T.Fatalf("Expected to find root-sync crontab-sync component in output:\n%s\n", string(out))
	}

	if !strings.Contains(string(out), "bookinfo:crontab-sync") {
		nt.T.Fatalf("Expected to find repo-sync crontab-sync component in output:\n%s\n", string(out))
	}

	if strings.Contains(string(out), "<root>:root-sync") {
		nt.T.Fatalf("Expected to not find root-sync component in output:\n%s\n", string(out))
	}

	// get status with namespace=c-m-s
	// expect to find both root-sync
	cmd2 := nt.Shell.Command("nomos", "status", "--namespace", "config-management-system")
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out2))
		nt.T.Fatal(err)
	}

	if strings.Contains(string(out2), "bookinfo:bookinfo-repo-sync") {
		nt.T.Fatalf("Expected to not find bookinfo-repo-sync component in output:\n%s\n", string(out2))
	}

	if !strings.Contains(string(out2), "<root>:root-sync") {
		nt.T.Fatalf("Expected to find root-sync component in output:\n%s\n", string(out2))
	}

	if !strings.Contains(string(out2), "<root>:crontab-sync") {
		nt.T.Fatalf("Expected to find crontab-sync component in output:\n%s\n", string(out2))
	}

	// get status with namespace bookinfo
	// expect to find bookinfo-repo-sync and bookinf:crontab-sync
	cmd3 := nt.Shell.Command("nomos", "status", "--namespace", "bookinfo")
	out3, err := cmd3.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out3))
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out3), "bookinfo:bookinfo-repo-sync") {
		nt.T.Fatalf("Expected to find repo-sync component in output:\n%s\n", string(out3))
	}

	if !strings.Contains(string(out3), "bookinfo:crontab-sync") {
		nt.T.Fatalf("Expected to find bookinfo:crontab-sync component in output:\n%s\n", string(out3))
	}

	if strings.Contains(string(out3), "<root>:root-sync") {
		nt.T.Fatalf("Expected to not find root-sync component in output:\n%s\n", string(out3))
	}

	// get status with name and namespace
	cmd4 := nt.Shell.Command("nomos", "status", "--namespace", "bookinfo", "--name", "bookinfo-repo-sync")
	out4, err := cmd4.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out4))
		nt.T.Fatal(err)
	}

	if !strings.Contains(string(out4), "bookinfo:bookinfo-repo-sync") {
		nt.T.Fatalf("Expected to find bookinfo:bookinfo-repo-sync component in output:\n%s\n", string(out4))
	}

	if strings.Contains(string(out4), "bookinfo:crontab-sync") {
		nt.T.Fatalf("Expected to not find bookinfo:crontab-sync component in output:\n%s\n", string(out4))
	}
}

func TestApiResourceFormatting(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI)

	// expected column name
	var columnName = []string{"NAME", "SHORTNAMES", "APIVERSION", "NAMESPACED", "KIND"}

	cmd := nt.Shell.Command("kubectl", "api-resources")
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		nt.T.Fatal(err)
	}

	header := strings.Fields(strings.Split(string(out), "\n")[0])

	assert.Equal(t, columnName, header)
}

func TestNomosMigrate(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)

	nt.T.Cleanup(func() {
		// Restore state of Config Sync installation after test
		if err := nomostest.InstallConfigSync(nt); err != nil {
			nt.T.Fatal(err)
		}
	})
	nt.T.Cleanup(func() {
		// Delete the ConfigManagement operator in case the test failed early.
		// If this lingers around it could cause issues for subsequent tests.
		cmDeployment := fake.DeploymentObject(
			core.Namespace(configsync.ControllerNamespace),
			core.Name("config-management"),
		)
		if err := nt.KubeClient.Delete(cmDeployment, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Fatal(err)
		}
		if err := nt.Watcher.WatchForNotFound(kinds.Deployment(), "config-management", configsync.ControllerNamespace); err != nil {
			nt.T.Error(err)
		}
	})

	nt.T.Log("Installing ConfigManagement")
	nt.MustKubectl("apply", "-f", "../testdata/configmanagement/1.18.0/config-management-operator.yaml")

	if err := nt.Watcher.WatchForCurrentStatus(kinds.CustomResourceDefinitionV1(), "configmanagements.configmanagement.gke.io", ""); err != nil {
		nt.T.Fatal(err)
	}

	cmObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configmanagement.gke.io/v1",
			"kind":       "ConfigManagement",
			"metadata": map[string]interface{}{
				"name": "config-management",
			},
			"spec": map[string]interface{}{
				"enableMultiRepo": true,
			},
		},
	}

	nt.T.Log("Configuring ConfigManagement for multi-repo mode")
	if err := nt.KubeClient.Create(cmObj); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.Watcher.WatchObject(kinds.Deployment(), "reconciler-manager", configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerImageEquals(
				"reconciler-manager",
				"gcr.io/config-management-release/reconciler-manager:v1.18.0-rc.3",
			),
		})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Running nomos migrate to migrate from ConfigManagement to OSS install")
	_, err = nt.Shell.Command("nomos", "migrate", "--remove-configmanagement").CombinedOutput()
	// TODO: this breaks the XML parsing of the junit report
	//nt.T.Log(string(out))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for legacy resources to be NotFound...")
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			util.ACMOperatorDeployment, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			"git-importer", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.CustomResourceDefinitionV1(),
			util.ConfigManagementCRDName, "")
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for expected resources to be current...")
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			util.ReconcilerManagerName, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			configmanagement.RGControllerName, configmanagement.RGControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			core.RootReconcilerName(configsync.RootSyncName), configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(),
			configsync.RootSyncName, configsync.ControllerNamespace)
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestNomosMigrateMonoRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.NomosCLI, ntopts.SkipConfigSyncInstall)

	nt.T.Cleanup(func() {
		// Restore state of Config Sync installation after test.
		// This also emulates upgrading to the current version after migrating
		if err := nomostest.InstallConfigSync(nt); err != nil {
			nt.T.Fatal(err)
		}
	})
	nt.T.Cleanup(func() {
		// Delete the ConfigManagement operator in case the test failed early.
		// If this lingers around it could cause issues for subsequent tests.
		cmDeployment := fake.DeploymentObject(
			core.Namespace(configsync.ControllerNamespace),
			core.Name("config-management"),
		)
		if err := nt.KubeClient.Delete(cmDeployment, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
		if err := nt.Watcher.WatchForNotFound(kinds.Deployment(), "config-management", configsync.ControllerNamespace); err != nil {
			nt.T.Error(err)
		}
		// Ensure the RootSync is deleted since it's not registered with nt
		rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
		if err := nt.KubeClient.Delete(rs, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
		if err := nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace); err != nil {
			nt.T.Error(err)
		}
	})

	nt.T.Log("Installing ConfigManagement")
	nt.MustKubectl("apply", "-f", "../testdata/configmanagement/1.18.0/config-management-operator.yaml")

	if err := nt.Watcher.WatchForCurrentStatus(kinds.CustomResourceDefinitionV1(), util.ConfigManagementCRDName, ""); err != nil {
		nt.T.Fatal(err)
	}

	cmObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configmanagement.gke.io/v1",
			"kind":       "ConfigManagement",
			"metadata": map[string]interface{}{
				"name": "config-management",
			},
			"spec": map[string]interface{}{
				"enableMultiRepo": true,
			},
		},
	}

	// Initialize with multi-repo mode to take over ownership of multi-repo objects.
	// This ensures everything gets cleaned up when switching to mono-repo mode.
	nt.T.Log("Configuring ConfigManagement for multi-repo mode")
	if err := nt.KubeClient.Create(cmObj); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.Watcher.WatchObject(kinds.Deployment(), "reconciler-manager", configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerImageEquals(
				"reconciler-manager",
				"gcr.io/config-management-release/reconciler-manager:v1.18.0-rc.3",
			),
		})
	if err != nil {
		nt.T.Fatal(err)
	}

	cmObj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configmanagement.gke.io/v1",
			"kind":       "ConfigManagement",
			"metadata": map[string]interface{}{
				"name": "config-management",
			},
			"spec": map[string]interface{}{
				"enableMultiRepo": false,
				"sourceFormat":    "unstructured",
				"git": map[string]interface{}{
					"syncRepo":   "https://github.com/config-sync-examples/namespace-repo-bookinfo",
					"syncBranch": "main",
					"secretType": "none",
				},
			},
		},
	}
	expectedRootSyncSpec := v1beta1.RootSyncSpec{
		SourceType:   string(v1beta1.GitSource),
		SourceFormat: string(filesystem.SourceFormatUnstructured),
		Override:     &v1beta1.RootSyncOverrideSpec{},
		Git: &v1beta1.Git{
			Repo:      "https://github.com/config-sync-examples/namespace-repo-bookinfo",
			Branch:    "main",
			Auth:      configsync.AuthNone,
			Revision:  "HEAD",
			Dir:       ".",
			Period:    metav1.Duration{Duration: 15 * time.Second},
			SecretRef: &v1beta1.SecretReference{},
		},
	}

	nt.T.Log("Configuring ConfigManagement for legacy mono-repo mode")
	if err := nt.KubeClient.Apply(cmObj); err != nil {
		nt.T.Fatal(err)
	}

	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			util.ACMOperatorDeployment, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			"git-importer", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			util.ReconcilerManagerName, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Namespace(),
			configmanagement.RGControllerNamespace, "")
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Running nomos migrate to migrate from ConfigManagement to OSS install")
	_, err = nt.Shell.Command("nomos", "migrate", "--remove-configmanagement").CombinedOutput()
	// TODO: this breaks the XML parsing of the junit report
	//nt.T.Log(string(out))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for legacy resources to be NotFound...")
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			util.ACMOperatorDeployment, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(),
			"git-importer", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.CustomResourceDefinitionV1(),
			util.ConfigManagementCRDName, "")
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for expected resources to be current...")
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			util.ReconcilerManagerName, configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			configmanagement.RGControllerName, configmanagement.RGControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(),
			core.RootReconcilerName(configsync.RootSyncName), configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(),
			configsync.RootSyncName, configsync.ControllerNamespace,
			[]testpredicates.Predicate{
				testpredicates.RootSyncSpecEquals(expectedRootSyncSpec),
			})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Waiting for RootSync to be synced")
	err = nt.WatchForSync(kinds.RootSyncV1Beta1(),
		configsync.RootSyncName, configsync.ControllerNamespace,
		nomostest.RemoteRootRepoSha1Fn, nomostest.RootSyncHasStatusSyncCommit,
		&nomostest.SyncDirPredicatePair{
			Dir:       expectedRootSyncSpec.Dir,
			Predicate: nomostest.RootSyncHasStatusSyncDirectory,
		}, testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}
}
