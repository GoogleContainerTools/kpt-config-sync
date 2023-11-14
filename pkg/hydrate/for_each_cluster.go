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

package hydrate

import (
	"context"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate"
)

const (
	// We assume users will not name any cluster "defaultcluster".
	defaultCluster = "defaultcluster"
)

// ParseOptions includes information needed by the parsing step in nomos CLI.
// It is different from pkg/parse/opts.go used in the reconciler container on the server side.
type ParseOptions struct {
	// Parser is an interface to read configs from a filesystem.
	Parser filesystem.ConfigParser
	// SourceFormat specifies how the Parser should parse the source configs.
	SourceFormat filesystem.SourceFormat
	// FilePaths encapsulates the list of absolute file paths to read and the
	// absolute and relative path of the root directory.
	FilePaths reader.FilePaths
}

// ClusterFilterFunc is the type alias for the function that filters objects for selected clusters.
type ClusterFilterFunc func(clusterName string, fileObjects []ast.FileObject, err status.MultiError)

// ForEachCluster hydrates an AllConfigs for each declared cluster and executes
// the passed function on the result.
//
// parser is the ConfigParser which returns a set of FileObjects and a possible
// MultiError when Parse is called.
//
// getSyncedCRDs is the set of CRDs synced the the cluster used for APIServer checks.
// enableAPIServerChecks is whether to call Parse with APIServer checks enabled.
// apiResources is how to read cached API resources from the disk.
// filePaths is the list of absolute file paths to parse and the absolute and
// relative paths of the Nomos root.
//
// f is a function with three arguments:
// - clusterName, the name of the Cluster the Parser was called with.
// - fileObjects, the FileObjects which Parser.Parse returned.
// - err, the MultiError which Parser.Parse returned, if there was one.
//
// Per standard ForEach conventions, ForEachCluster has no return value.
func ForEachCluster(ctx context.Context, parseOpts ParseOptions, validateOpts validate.Options, f ClusterFilterFunc) {
	var errs status.MultiError
	clusterRegistry, err := parseOpts.Parser.ReadClusterRegistryResources(parseOpts.FilePaths, parseOpts.SourceFormat)
	errs = status.Append(errs, err)
	clustersObjects, err := selectors.FilterClusters(clusterRegistry)
	errs = status.Append(errs, err)
	clusterNames, err := parseOpts.Parser.ReadClusterNamesFromSelector(parseOpts.FilePaths)
	errs = status.Append(errs, err)

	// Hydrate for empty string cluster name. This is the default configuration.
	validateOpts.ClusterName = defaultCluster
	defaultFileObjects, err := parseOpts.Parser.Parse(parseOpts.FilePaths)
	errs = status.Append(errs, err)

	if parseOpts.SourceFormat == filesystem.SourceFormatHierarchy {
		defaultFileObjects, err = validate.Hierarchical(defaultFileObjects, validateOpts)
	} else {
		defaultFileObjects, err = validate.Unstructured(ctx, nil, defaultFileObjects, validateOpts)
	}
	errs = status.Append(errs, err)

	f(defaultCluster, defaultFileObjects, errs)

	// Hydrate for clusters selected by the cluster selectors.
	clusters := map[string]bool{}
	for _, cluster := range clustersObjects {
		if _, found := clusters[cluster.Name]; !found {
			clusters[cluster.Name] = true
		}
	}
	for _, cluster := range clusterNames {
		if _, found := clusters[cluster]; !found {
			clusters[cluster] = true
		}
	}

	for cluster := range clusters {
		validateOpts.ClusterName = cluster
		fileObjects, err := parseOpts.Parser.Parse(parseOpts.FilePaths)
		errs = status.Append(errs, err)

		if parseOpts.SourceFormat == filesystem.SourceFormatHierarchy {
			fileObjects, err = validate.Hierarchical(fileObjects, validateOpts)
		} else {
			fileObjects, err = validate.Unstructured(ctx, nil, fileObjects, validateOpts)
		}

		errs = status.Append(errs, err)
		f(cluster, fileObjects, errs)
	}
}
