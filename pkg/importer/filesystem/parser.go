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

// Package filesystem provides functionality to read Kubernetes objects from a filesystem tree
// and converting them to Nomos Custom Resource Definition objects.
package filesystem

import (
	"strings"

	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// Parser reads files on disk and builds Nomos Config objects to be reconciled by the Syncer.
type Parser struct {
	reader reader.Reader
}

var _ ConfigParser = &Parser{}

// NewParser creates a new Parser using the given Reader and parser options.
func NewParser(reader reader.Reader) *Parser {
	return &Parser{
		reader: reader,
	}
}

// Parse parses file tree rooted at root and builds policy CRDs from supported Kubernetes policy resources.
// Resources are read from the following directories:
//
// clusterName is the spec.clusterName of the cluster's ConfigManagement.
// enableAPIServerChecks, if true, contacts the API Server if it is unable to
//   determine whether types are namespace- or cluster-scoped.
// getSyncedCRDs is a callback that returns the CRDs on the API Server.
// filePaths is the list of absolute file paths to parse and the absolute and
//   relative paths of the Nomos root.
// It is an error for any files not to be present.
func (p *Parser) Parse(filePaths reader.FilePaths) ([]ast.FileObject, status.MultiError) {
	return p.reader.Read(filePaths)
}

// filterTopDir returns the set of files contained in the top directory of root
//   along with the absolute and relative paths of root.
// Assumes all files are within root.
func filterTopDir(filePaths reader.FilePaths, topDir string) reader.FilePaths {
	rootSplits := filePaths.RootDir.Split()
	var result []cmpath.Absolute
	for _, f := range filePaths.Files {
		if f.Split()[len(rootSplits)] != topDir {
			continue
		}
		result = append(result, f)
	}
	return reader.FilePaths{
		RootDir:   filePaths.RootDir,
		PolicyDir: filePaths.PolicyDir,
		Files:     result,
	}
}

// ReadClusterRegistryResources reads the manifests declared in clusterregistry/ for hierarchical format.
// For unstructured format, it reads all files.
func (p *Parser) ReadClusterRegistryResources(filePaths reader.FilePaths, format SourceFormat) ([]ast.FileObject, status.MultiError) {
	if format == SourceFormatHierarchy {
		return p.reader.Read(filterTopDir(filePaths, repo.ClusterRegistryDir))
	}
	return p.reader.Read(filePaths)
}

// ReadClusterNamesFromSelector returns the list of cluster names specified in the `cluster-name-selector` annotation.
func (p *Parser) ReadClusterNamesFromSelector(filePaths reader.FilePaths) ([]string, status.MultiError) {
	var clusters []string
	objs, err := p.Parse(filePaths)
	if err != nil {
		return clusters, err
	}

	for _, obj := range objs {
		if annotation, found := obj.GetAnnotations()[metadata.ClusterNameSelectorAnnotationKey]; found {
			names := strings.Split(annotation, ",")
			for _, name := range names {
				clusters = append(clusters, strings.TrimSpace(name))
			}
		}
	}
	return clusters, nil
}
