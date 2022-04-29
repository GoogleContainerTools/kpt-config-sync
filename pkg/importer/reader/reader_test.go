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

package reader_test

import (
	"fmt"
	"os"
	"path"
	"testing"

	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
)

func TestFileReader_Read_NotExist(t *testing.T) {
	dir := ft.NewTestDir(t)
	fps := dir.FilePaths("no-exist")

	r := reader.File{}

	objs, err := r.Read(fps)
	if err != nil || len(objs) > 0 {
		t.Errorf("got Read(nonexistent path) = %+v, %v; want nil, nil", objs, err)
	}
}

func TestFileReader_Read_BadPermissionsParent(t *testing.T) {
	// If we're root, this test will fail, because we'll have read access anyway.
	if os.Geteuid() == 0 {
		t.Skipf("%s will fail running with EUID==0", t.Name())
	}

	tmpRelative := "tmp.yaml"

	dir := ft.NewTestDir(t,
		ft.FileContents(tmpRelative, ""),
		ft.Chmod(tmpRelative, 0000),
	)
	fps := dir.FilePaths(tmpRelative)

	r := reader.File{}

	objs, err := r.Read(fps)
	if err == nil || len(objs) > 0 {
		t.Errorf("got Read(bad permissions on parent dir) = %+v, %v; want nil, error", objs, err)
	}
}

func TestFileReader_Read_BadPermissionsChild(t *testing.T) {
	// If we're root, this test will fail, because we'll have read access anyway.
	if os.Geteuid() == 0 {
		t.Skipf("%s will fail running with EUID==0", t.Name())
	}

	subDir := "namespaces"
	tmpRelative := path.Join(subDir, "tmp.yaml")

	dir := ft.NewTestDir(t,
		ft.FileContents(tmpRelative, ""),
		ft.Chmod(subDir, 0000),
	)
	fps := dir.FilePaths(tmpRelative)

	r := reader.File{}

	objs, err := r.Read(fps)
	if err == nil || len(objs) > 0 {
		t.Errorf("got Read(bad permissions on child dir) = %+v, %v; want nil, error", objs, err)
	}
}

func TestFileReader_Read_ValidMetadata(t *testing.T) {
	testCases := []struct {
		name     string
		metadata string
	}{
		{
			name: "no labels/annotations",
		},
		{
			name:     "empty labels",
			metadata: "labels:",
		},
		{
			name:     "empty annotations",
			metadata: "annotations:",
		},
		{
			name:     "empty map labels",
			metadata: "labels: {}",
		},
		{
			name:     "empty map annotations",
			metadata: "annotations: {}",
		},
	}

	nsFile := "namespace.yaml"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := ft.NewTestDir(t,
				ft.FileContents(nsFile, fmt.Sprintf(`
apiVersion: v1
kind: Namespace
metadata:
  name: foo
  %s
`, tc.metadata)))
			fps := dir.FilePaths(nsFile)
			r := reader.File{}
			_, err := r.Read(fps)

			if err != nil {
				t.Fatalf("got Read() = %v, want nil", err)
			}
		})
	}
}

func TestFileReader_Read_InvalidAnnotations(t *testing.T) {
	nsFile := "namespace.yaml"
	dir := ft.NewTestDir(t,
		ft.FileContents(nsFile, `
apiVersion: v1
kind: Namespace
metadata:
  name: foo
  annotations: a
`))
	fps := dir.FilePaths(nsFile)
	r := reader.File{}
	_, err := r.Read(fps)

	if err == nil {
		t.Fatal("got Read() = nil, want err")
	}
	errs := err.Errors()
	if len(errs) != 1 {
		t.Fatalf("got Read() = %d errors, want 1 err", len(errs))
	}

	if _, isResourceError := errs[0].(status.ResourceError); !isResourceError {
		t.Fatalf("got Read() = %T, want ResourceError", errs[0])
	}
}

func TestFileReader_Read_ValidObject(t *testing.T) {
	nsFile := "namespace.yaml"
	dir := ft.NewTestDir(t,
		ft.FileContents(nsFile, `
apiVersion: configmanagement.gke.io/v1
kind: Namespace
metadata:
  name: ns
spec:
  version: "1.0"
`))
	fps := dir.FilePaths(nsFile)
	r := reader.File{}
	_, err := r.Read(fps)

	if err != nil {
		t.Fatalf("got Read() = %v, want nil", err)
	}
}

func TestFileReader_Read_HiddenFolder(t *testing.T) {
	testCases := []struct {
		fileName     string
		fileContents string
	}{
		{
			fileName: "/.github/namespace.yaml",
		},
		{
			fileName: "/.gitlab/namespace.yml",
			fileContents: `
apiVersion: configmanagement.gke.io/v1
kind: Namespace
metadata:
name: ns
spec:
version: "1.0"
`,
		},
		{
			fileName:     ".gitlab/gitlab-ci.yml",
			fileContents: "foo: bar",
		},
		{
			fileName:     ".github/workflows/github-actions.yml",
			fileContents: "This is just unformatted text",
		},
		{
			fileName: ".gitlab-ci.yml",
			fileContents: `
variables:
	FAKE_VARIABLE: "not real"
workflow:
	rules:`,
		},
	}

	for _, tc := range testCases {
		dir := ft.NewTestDir(t,
			ft.FileContents(tc.fileName, tc.fileContents))

		fps := dir.FilePaths(tc.fileName)
		r := reader.File{}
		fileObj, err := r.Read(fps)

		if fileObj != nil || err != nil {
			t.Fatalf("got Read() = %v, %v, want nil, nil", fileObj, err)
		}
	}
}
