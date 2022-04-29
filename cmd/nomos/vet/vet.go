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
	"fmt"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/importer/filesystem"
)

var (
	namespaceValue string
	keepOutput     bool
	outPath        string
)

func init() {
	flags.AddClusters(Cmd)
	flags.AddPath(Cmd)
	flags.AddSkipAPIServerCheck(Cmd)
	flags.AddSourceFormat(Cmd)
	flags.AddOutputFormat(Cmd)
	Cmd.Flags().StringVar(&namespaceValue, "namespace", "",
		fmt.Sprintf(
			"If set, validate the repository as a Namespace Repo with the provided name. Automatically sets --source-format=%s",
			filesystem.SourceFormatUnstructured))

	Cmd.Flags().BoolVar(&keepOutput, "keep-output", false,
		`If enabled, keep the hydrated output`)

	Cmd.Flags().StringVar(&outPath, "output", flags.DefaultHydrationOutput,
		`Location of the hydrated output`)
}

// Cmd is the Cobra object representing the nomos vet command.
var Cmd = &cobra.Command{
	Use:   "vet",
	Short: "Validate an Anthos Configuration Management directory",
	Long: `Validate an Anthos Configuration Management directory
Checks for semantic and syntactic errors in an Anthos Configuration Management directory
that will interfere with applying resources. Prints found errors to STDERR and
returns a non-zero error code if any issues are found.
`,
	Example: `  nomos vet
  nomos vet --path=my/directory
  nomos vet --path=/path/to/my/directory`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		return runVet(cmd.Context(), namespaceValue, filesystem.SourceFormat(flags.SourceFormat))
	},
}
