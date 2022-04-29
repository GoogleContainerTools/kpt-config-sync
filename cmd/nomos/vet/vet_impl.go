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
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"kpt.dev/configsync/cmd/nomos/flags"
	nomosparse "kpt.dev/configsync/cmd/nomos/parse"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
)

// vet runs nomos vet with the specified options.
//
// root is the OS-specific path to the Nomos policy root.
//   If relative, it is assumed to be relative to the working directory.
// namespace, if non-emptystring, validates the repo as a CSMR Namespace
//   repository.
// sourceFormat is whether the repository is in the hierarchy or unstructured
//   format.
// skipAPIServer is whether to skip the API Server checks.
// allClusters is whether we are implicitly vetting every cluster.
// clusters is the set of clusters we are checking.
//   Only used if allClusters is false.
func runVet(ctx context.Context, namespace string, sourceFormat filesystem.SourceFormat) error {
	if sourceFormat == "" {
		if namespace == "" {
			// Default to hierarchical if --namespace is not provided.
			sourceFormat = filesystem.SourceFormatHierarchy
		} else {
			// Default to unstructured if --namespace is provided.
			sourceFormat = filesystem.SourceFormatUnstructured
		}
	}

	rootDir, needsHydrate, err := hydrate.ValidateHydrateFlags(sourceFormat)
	if err != nil {
		return err
	}

	if needsHydrate {
		// update rootDir to point to the hydrated output for further processing.
		if rootDir, err = hydrate.ValidateAndRunKustomize(rootDir.OSPath()); err != nil {
			return err
		}
		// delete the hydrated output directory in the end.
		defer func() {
			_ = os.RemoveAll(rootDir.OSPath())
		}()
	}

	files, err := nomosparse.FindFiles(rootDir)
	if err != nil {
		return err
	}

	parser := filesystem.NewParser(&reader.File{})

	options, err := hydrate.ValidateOptions(ctx, rootDir)
	if err != nil {
		return err
	}

	switch sourceFormat {
	case filesystem.SourceFormatHierarchy:
		if namespace != "" {
			// The user could technically provide --source-format=unstructured.
			// This nuance isn't necessary to communicate nor confusing to omit.
			return errors.Errorf("if --namespace is provided, --%s must be omitted or set to %s",
				reconcilermanager.SourceFormat, filesystem.SourceFormatUnstructured)
		}

		files = filesystem.FilterHierarchyFiles(rootDir, files)
	case filesystem.SourceFormatUnstructured:
		if namespace == "" {
			options = parse.OptionsForScope(options, declared.RootReconciler)
		} else {
			options = parse.OptionsForScope(options, declared.Scope(namespace))
		}
	default:
		return fmt.Errorf("unknown %s value %q", reconcilermanager.SourceFormat, sourceFormat)
	}

	filePaths := reader.FilePaths{
		RootDir:   rootDir,
		PolicyDir: cmpath.RelativeOS(rootDir.OSPath()),
		Files:     files,
	}

	// Track per-cluster vet errors.
	var allObjects []ast.FileObject
	var vetErrs []string
	numClusters := 0
	hydrate.ForEachCluster(parser, options, sourceFormat, filePaths, func(clusterName string, fileObjects []ast.FileObject, err status.MultiError) {
		clusterEnabled := flags.AllClusters()
		for _, cluster := range flags.Clusters {
			if clusterName == cluster {
				clusterEnabled = true
			}
		}
		if !clusterEnabled {
			return
		}
		numClusters++

		if err != nil {
			if clusterName == "" {
				clusterName = nomosparse.UnregisteredCluster
			}
			vetErrs = append(vetErrs, clusterErrors{
				name:       clusterName,
				MultiError: err,
			}.Error())
		}

		if keepOutput {
			allObjects = append(allObjects, fileObjects...)
		}
	})
	if keepOutput {
		multiCluster := numClusters > 1
		fileObjects := hydrate.GenerateFileObjects(multiCluster, allObjects...)
		if err := hydrate.PrintDirectoryOutput(outPath, flags.OutputFormat, fileObjects); err != nil {
			_ = util.PrintErr(err)
		}
	}
	if len(vetErrs) > 0 {
		return errors.New(strings.Join(vetErrs, "\n\n"))
	}

	fmt.Println("✅ No validation issues found.")
	return nil
}

// clusterErrors is the set of vet errors for a specific Cluster.
type clusterErrors struct {
	name string
	status.MultiError
}

func (e clusterErrors) Error() string {
	if e.name == "defaultcluster" {
		return e.MultiError.Error()
	}
	return fmt.Sprintf("errors for cluster %q:\n%v\n", e.name, e.MultiError.Error())
}
