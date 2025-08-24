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

package hydrate

import (
	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configsync"
)

// localFlags holds the hydrate command flags
var localFlags = NewFlags()

func init() {
	// Initialize flags for the hydrate command
	// This separation keeps flag definitions isolated from command execution logic
	localFlags.AddFlags(Cmd)
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

		// Create execution parameters from parsed flags
		params := ExecParams{
			Clusters:         flags.Clusters,
			Path:             flags.Path,
			SkipAPIServer:    flags.SkipAPIServer,
			SourceFormat:     configsync.SourceFormat(flags.SourceFormat),
			OutputFormat:     flags.OutputFormat,
			APIServerTimeout: flags.APIServerTimeout,
			Flat:             localFlags.Flat,
			OutPath:          localFlags.OutPath,
		}

		// Execute the hydrate command logic
		return ExecuteHydrate(cmd.Context(), params)
	},
}
