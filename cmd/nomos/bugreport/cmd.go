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

package bugreport

import (
	"fmt"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configmanagement"
)

// Cmd retrieves readers for all relevant nomos container logs and cluster state commands and writes them to a zip file
var Cmd = &cobra.Command{
	Use:   "bugreport",
	Short: fmt.Sprintf("Generates a zip file of relevant %v debug information.", configmanagement.CLIName),
	Long:  "Generates a zip file in your current directory containing an aggregate of the logs and cluster state for debugging purposes.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		// Create execution parameters from parsed flags
		params := ExecParams{
			ClientTimeout: flags.ClientTimeout,
		}

		// Execute the bugreport command logic
		return ExecuteBugreport(cmd.Context(), params)
	},
}

func init() {
	// Initialize flags for the bugreport command
	// This separation keeps flag definitions isolated from command execution logic
	flags.AddClientTimeout(Cmd)
}
