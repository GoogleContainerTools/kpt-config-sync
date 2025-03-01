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

package vet

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/yaml"
)

func resetFlags() {
	// Flags are global state carried over between tests.
	// Cobra lazily evaluates flags only if they are declared, so unless these
	// are reset, successive calls to Cmd.Execute aren't guaranteed to be
	// independent.
	flags.Clusters = nil
	flags.Path = flags.PathDefault
	flags.SkipAPIServer = true
	flags.SourceFormat = string(configsync.SourceFormatHierarchy)
	namespaceValue = ""
	keepOutput = false
	outPath = flags.DefaultHydrationOutput
	flags.OutputFormat = flags.OutputYAML
}

var examplesDir = cmpath.RelativeSlash("../../../examples")

func TestVet_Acme(t *testing.T) {
	resetFlags()
	Cmd.SilenceUsage = true

	os.Args = []string{
		"vet", // this first argument does nothing, but is required to exist.
		"--path", examplesDir.Join(cmpath.RelativeSlash("acme")).OSPath(),
	}

	err := Cmd.Execute()
	require.NoError(t, err)
}

func TestVet_AcmeSymlink(t *testing.T) {
	resetFlags()
	Cmd.SilenceUsage = true

	dir := ft.NewTestDir(t)
	symDir := dir.Root().Join(cmpath.RelativeSlash("acme-symlink"))

	absExamples, err := filepath.Abs(examplesDir.Join(cmpath.RelativeSlash("acme")).OSPath())
	if err != nil {
		t.Fatal(err)
	}
	err = os.Symlink(absExamples, symDir.OSPath())
	if err != nil {
		t.Fatal(err)
	}

	os.Args = []string{
		"vet", // this first argument does nothing, but is required to exist.
		"--path", symDir.OSPath(),
	}

	err = Cmd.Execute()
	require.NoError(t, err)
}

func TestVet_FooCorp(t *testing.T) {
	resetFlags()
	Cmd.SilenceUsage = true

	os.Args = []string{
		"vet", // this first argument does nothing, but is required to exist.
		"--path", examplesDir.Join(cmpath.RelativeSlash("foo-corp-example/foo-corp")).OSPath(),
	}

	err := Cmd.Execute()
	require.NoError(t, err)
}

func TestVet_MultiCluster(t *testing.T) {
	Cmd.SilenceUsage = true

	repoPath := filepath.Join(examplesDir.OSPath(), "parse-errors/cluster-specific-collision")
	absRepoPath, err := filepath.Abs(repoPath)
	require.NoError(t, err)

	tcs := []struct {
		name      string
		args      []string
		wantError error
	}{
		{
			name: "detect collision when all clusters enabled",
			wantError: clusterErrors{
				name: "prod-cluster",
				MultiError: nonhierarchical.ClusterMetadataNameCollisionError(
					kinds.ClusterRole().GroupKind(), "clusterrole",
					k8sobjects.ClusterRoleObject(core.Name("clusterrole"),
						core.Annotation(metadata.SourcePathAnnotationKey,
							filepath.Join(absRepoPath, "cluster/default_clusterrole.yaml"))),
					k8sobjects.ClusterRoleObject(core.Name("clusterrole"),
						core.Annotation(metadata.SourcePathAnnotationKey,
							filepath.Join(absRepoPath, "cluster/prod_clusterrole.yaml"))),
				),
			},
		},
		{
			name: "detect collision in prod-cluster",
			args: []string{"--clusters", "prod-cluster"},
			wantError: clusterErrors{
				name: "prod-cluster",
				MultiError: nonhierarchical.ClusterMetadataNameCollisionError(
					kinds.ClusterRole().GroupKind(), "clusterrole",
					k8sobjects.ClusterRoleObject(core.Name("clusterrole"),
						core.Annotation(metadata.SourcePathAnnotationKey,
							filepath.Join(absRepoPath, "cluster/default_clusterrole.yaml"))),
					k8sobjects.ClusterRoleObject(core.Name("clusterrole"),
						core.Annotation(metadata.SourcePathAnnotationKey,
							filepath.Join(absRepoPath, "cluster/prod_clusterrole.yaml"))),
				),
			},
		},
		{
			name: "do not detect collision in dev-cluster",
			args: []string{"--clusters", "dev-cluster"},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()

			os.Args = append([]string{
				"vet", // this first argument does nothing, but is required to exist.
				"--path", repoPath,
			}, tc.args...)

			output := new(bytes.Buffer)
			Cmd.SetOut(output)
			Cmd.SetErr(output)

			err := Cmd.Execute()
			if tc.wantError == nil {
				require.NoError(t, err)
				require.Equal(t, "✅ No validation issues found.\n", output.String())
			} else {
				// Execute wraps the string output as a simple error.
				wantError := errors.New(tc.wantError.Error())
				require.Equal(t, wantError, err)
			}
		})
	}
}

func TestVet_Threshold(t *testing.T) {
	Cmd.SilenceUsage = true

	tcs := []struct {
		name      string
		objects   []ast.FileObject
		args      []string
		wantError error
	}{
		{
			name: "unstructured, no objects",
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
			},
		},
		{
			name: "unstructured, 1 object",
			objects: []ast.FileObject{
				k8sobjects.FileObject(k8sobjects.DeploymentObject(), "deployment.yaml"),
			},
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
			},
		},
		{
			name: "unstructured, 1001 objects, threshold unspecified",
			objects: func() []ast.FileObject {
				objs := make([]ast.FileObject, 1001)
				for i := range objs {
					name := fmt.Sprintf("deployment-%d", i)
					fileName := fmt.Sprintf("deployment-%d.yaml", i)
					objs[i] = k8sobjects.FileObject(
						k8sobjects.DeploymentObject(
							core.Name(name),
							core.Namespace("example")),
						fileName)
				}
				return objs
			}(),
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
			},
		},
		{
			name: "unstructured, 1001 objects, threshold default",
			objects: func() []ast.FileObject {
				objs := make([]ast.FileObject, 1001)
				for i := range objs {
					name := fmt.Sprintf("deployment-%d", i)
					fileName := fmt.Sprintf("deployment-%d.yaml", i)
					objs[i] = k8sobjects.FileObject(
						k8sobjects.DeploymentObject(
							core.Name(name),
							core.Namespace("example")),
						fileName)
				}
				return objs
			}(),
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
				"--threshold",
			},
			wantError: system.MaxObjectCountError(1000, 1001),
		},
		{
			name: "unstructured, 1001 objects, threshold 1000",
			objects: func() []ast.FileObject {
				objs := make([]ast.FileObject, 1001)
				for i := range objs {
					name := fmt.Sprintf("deployment-%d", i)
					fileName := fmt.Sprintf("deployment-%d.yaml", i)
					objs[i] = k8sobjects.FileObject(
						k8sobjects.DeploymentObject(
							core.Name(name),
							core.Namespace("example")),
						fileName)
				}
				return objs
			}(),
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
				"--threshold=1000",
			},
			wantError: system.MaxObjectCountError(1000, 1001),
		},
		{
			name: "unstructured, 1001 objects, threshold 1001",
			objects: func() []ast.FileObject {
				objs := make([]ast.FileObject, 1001)
				for i := range objs {
					name := fmt.Sprintf("deployment-%d", i)
					fileName := fmt.Sprintf("deployment-%d.yaml", i)
					objs[i] = k8sobjects.FileObject(
						k8sobjects.DeploymentObject(
							core.Name(name),
							core.Namespace("example")),
						fileName)
				}
				return objs
			}(),
			args: []string{
				"--source-format", string(configsync.SourceFormatUnstructured),
				"--threshold=1001",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()

			tmpDirPath, err := os.MkdirTemp("", "nomos-test-")
			require.NoError(t, err)
			t.Cleanup(func() {
				err := os.RemoveAll(tmpDirPath)
				require.NoError(t, err)
			})

			// Write objects to temp dir as yaml files
			for _, fileObj := range tc.objects {
				filePath := filepath.Join(tmpDirPath, fileObj.OSPath())
				err = os.MkdirAll(filepath.Dir(filePath), os.ModePerm)
				require.NoError(t, err)
				fileData, err := yaml.Marshal(fileObj.Unstructured)
				require.NoError(t, err)
				err = os.WriteFile(filePath, fileData, 0644)
				require.NoError(t, err)
			}

			os.Args = append([]string{
				"vet", // this first argument does nothing, but is required to exist.
				"--path", tmpDirPath,
			}, tc.args...)

			output := new(bytes.Buffer)
			Cmd.SetOut(output)
			Cmd.SetErr(output)

			err = Cmd.Execute()
			if tc.wantError == nil {
				require.NoError(t, err)
				require.Equal(t, "✅ No validation issues found.\n", output.String())
			} else {
				// Execute wraps the string output as a simple error.
				wantError := errors.New(tc.wantError.Error())
				require.Equal(t, wantError, err)
			}
		})
	}
}
