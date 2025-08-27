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
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	nomosparse "kpt.dev/configsync/cmd/nomos/parse"
	"kpt.dev/configsync/cmd/nomos/util"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
)

var (
	flat    bool
	outPath string
)

func init() {
	flags.AddClusters(Cmd)
	flags.AddPath(Cmd)
	flags.AddSkipAPIServerCheck(Cmd)
	flags.AddSourceFormat(Cmd)
	flags.AddOutputFormat(Cmd)
	flags.AddAPIServerTimeout(Cmd)
	Cmd.Flags().BoolVar(&flat, "flat", false,
		`If enabled, print all output to a single file`)
	Cmd.Flags().StringVar(&outPath, "output", flags.DefaultHydrationOutput,
		`Location to write hydrated configuration to.

If --flat is not enabled, writes each resource manifest as a
separate file. You may run "kubectl apply -fR" on the result to apply
the configuration to a cluster. If the repository declares any Cluster
resources, contains a subdirectory for each Cluster.

If --flat is enabled, writes to the, writes a single file holding all
resource manifests. You may run "kubectl apply -f" on the result to
apply the configuration to a cluster.`)
}

// Cmd is the Cobra object representing the hydrate command.
var Cmd = &cobra.Command{
	Use:   "hydrate",
	Short: "Compiles the local repository to the exact form that would be sent to the APIServer.",
	Long: `Compiles the local repository to the exact form that would be sent to the APIServer.

The output directory consists of one directory per declared Cluster, and defaultcluster/ for
clusters without declarations. Each directory holds the full set of configs for a single cluster,
which you could kubectl apply -fR to the cluster, or have Config Sync sync to the cluster.`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		sourceFormat := configsync.SourceFormat(flags.SourceFormat)
		if sourceFormat == "" {
			sourceFormat = configsync.SourceFormatHierarchy
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

		validateOpts, err := hydrate.ValidateOptions(rootDir, flags.APIServerTimeout)
		if err != nil {
			return err
		}
		validateOpts.FieldManager = util.FieldManager

		if sourceFormat == configsync.SourceFormatHierarchy {
			files = filesystem.FilterHierarchyFiles(rootDir, files)
		} else {
			// hydrate as a root repository to preview all the hydrated configs
			validateOpts.Scope = declared.RootScope
		}

		filePaths := reader.FilePaths{
			RootDir:   rootDir,
			PolicyDir: cmpath.RelativeOS(rootDir.OSPath()),
			Files:     files,
		}

		parseOpts := hydrate.ParseOptions{
			Parser:       parser,
			SourceFormat: sourceFormat,
			FilePaths:    filePaths,
		}

		var allObjects []ast.FileObject
		encounteredError := false
		numClusters := 0
		clusterFilterFunc := func(clusterName string, fileObjects []ast.FileObject, err status.MultiError) {
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
				util.PrintErrOrDie(fmt.Errorf("errors for Cluster %q: %w", clusterName, err))

				encounteredError = true

				if status.HasBlockingErrors(err) {
					return
				}
			}

			allObjects = append(allObjects, fileObjects...)
		}
		hydrate.ForEachCluster(cmd.Context(), parseOpts, validateOpts, clusterFilterFunc)

		multiCluster := numClusters > 1
		fileObjects := hydrate.GenerateFileObjects(multiCluster, allObjects...)
		if flat {
			err = hydrate.PrintFlatOutput(outPath, flags.OutputFormat, fileObjects)
		} else {
			err = hydrate.PrintDirectoryOutput(outPath, flags.OutputFormat, fileObjects)
		}
		if err != nil {
			return err
		}

		if encounteredError {
			os.Exit(1)
		}

		return nil
	},
}
