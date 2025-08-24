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

package version

import (
	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
)

func init() {
	// Initialize flags for the version command
	// This separation keeps flag definitions isolated from command execution logic
	flags.AddClientTimeout(Cmd)
}

// Cmd is the Cobra object representing the nomos version command.
var Cmd = &cobra.Command{
	Use:   "version",
	Short: "Prints the version of ACM for each cluster as well this CLI",
	Long: `Prints the version of Configuration Management installed on each cluster and the version
of the "nomos" client binary for debugging purposes.`,
	Example: `  nomos version`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		// Create execution parameters from parsed flags
		params := ExecParams{
			Contexts:      flags.Contexts,
			ClientTimeout: flags.ClientTimeout,
		}

		// Execute the version command logic
		return ExecuteVersion(cmd.Context(), params)
	},
}
