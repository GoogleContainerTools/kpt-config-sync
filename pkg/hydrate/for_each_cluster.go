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

// ForEachCluster hydrates an AllConfigs for each declared cluster and executes the passed function
// on the result.
//
// parser is the ConfigParser which returns a set of FileObjects and a possible MultiError
//   when Parse is called.
// getSyncedCRDs is the set of CRDs synced the the cluster used for APIServer checks.
// enableAPIServerChecks is whether to call Parse with APIServer checks enabled.
// apiResources is how to read cached API resources from the disk.
// filePaths is the list of absolute file paths to parse and the absolute and
//   relative paths of the Nomos root.
// f is a function with three arguments:
//    clusterName, the name of the Cluster the Parser was called with.
//    fileObjects, the FileObjects which Parser.Parse returned.
//    err, the MultiError which Parser.Parse returned, if there was one.
//
// Per standard ForEach conventions, ForEachCluster has no return value.
func ForEachCluster(parser filesystem.ConfigParser, options validate.Options,
	sourceFormat filesystem.SourceFormat, filePaths reader.FilePaths,
	f func(clusterName string, fileObjects []ast.FileObject, err status.MultiError)) {
	clusterRegistry, errs := parser.ReadClusterRegistryResources(filePaths, sourceFormat)
	clustersObjects, err2 := selectors.FilterClusters(clusterRegistry)
	clusterNames, err3 := parser.ReadClusterNamesFromSelector(filePaths)
	errs = status.Append(errs, err2, err3)

	// Hydrate for empty string cluster name. This is the default configuration.
	options.ClusterName = defaultCluster
	defaultFileObjects, err2 := parser.Parse(filePaths)
	errs = status.Append(errs, err2)

	if sourceFormat == filesystem.SourceFormatHierarchy {
		defaultFileObjects, err2 = validate.Hierarchical(defaultFileObjects, options)
	} else {
		defaultFileObjects, err2 = validate.Unstructured(defaultFileObjects, options)
	}
	errs = status.Append(errs, err2)

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
		options.ClusterName = cluster
		fileObjects, errs := parser.Parse(filePaths)

		if sourceFormat == filesystem.SourceFormatHierarchy {
			fileObjects, err2 = validate.Hierarchical(fileObjects, options)
		} else {
			fileObjects, err2 = validate.Unstructured(fileObjects, options)
		}

		errs = status.Append(errs, err2)
		f(cluster, fileObjects, errs)
	}
}
