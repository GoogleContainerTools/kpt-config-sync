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

package initialize

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/util/repo"
)

const (
	readmeFile         = "README.md"
	rootReadmeContents = `# Anthos Configuration Management Directory

This is the root directory for Anthos Configuration Management.

See [our documentation](https://cloud.google.com/anthos-config-management/docs/repo) for how to use each subdirectory.
`
	systemReadmeContents = `# System

This directory contains system configs such as the repo version and how resources are synced.
`
)

// defaultRepo returns a FileObject of an *Unstructured* representing a Repo
// object with problematic fields removed.
func defaultRepo() (ast.FileObject, error) {
	obj := repo.Default()
	// We have to convert to JSON and then to Unstructured or else printing with
	// YAMLPrinter will include default-initialized fields like creationTimestamp
	// and status, which we don't want.
	//
	// This is because YAMLPrinter:
	// 1) doesn't distinguish between unset fields and fields set to the default, and
	// 2) ignores JSON "omitempty" directives.
	jsn, err := json.Marshal(obj)
	if err != nil {
		return ast.FileObject{}, err
	}

	// Marshal JSON to the Unstructured format
	u := &unstructured.Unstructured{}
	err = u.UnmarshalJSON(jsn)
	if err != nil {
		return ast.FileObject{}, err
	}

	// Remove the fields from the Unstructured.
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(u.Object, "status")

	return ast.NewFileObject(u, cmpath.RelativeSlash("system/repo.yaml")), nil
}
