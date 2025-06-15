// Copyright 2025 Google LLC
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
	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configsync"
)

// localFlags holds the vet command flags
var localFlags = NewFlags()

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
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		// Create execution parameters from parsed flags
		params := ExecParams{
			Clusters:         flags.Clusters,
			Path:             flags.Path,
			SkipAPIServer:    flags.SkipAPIServer,
			SourceFormat:     configsync.SourceFormat(flags.SourceFormat),
			OutputFormat:     flags.OutputFormat,
			APIServerTimeout: flags.APIServerTimeout,
			Namespace:        localFlags.NamespaceValue,
			KeepOutput:       localFlags.KeepOutput,
			MaxObjectCount:   localFlags.Threshold,
			OutPath:          localFlags.OutPath,
		}

		// Execute the vet command logic
		return ExecuteVet(cmd.Context(), cmd.OutOrStderr(), params)
	},
}

func init() {
	// Initialize flags for the vet command
	// This separation keeps flag definitions isolated from command execution logic
	localFlags.AddFlags(Cmd)
}
