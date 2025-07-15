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

package status

import (
	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
)

// localFlags holds the status command flags
var localFlags = NewFlags()

// Cmd runs a loop that fetches ACM objects from all available clusters and prints a summary of the
// status of Config Management for each cluster.
var Cmd = &cobra.Command{
	Use: "status",
	// TODO: make Configuration Management a constant (for product renaming)
	Short: `Prints the status of all clusters with Configuration Management installed.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		// Create execution parameters from parsed flags
		params := ExecutionParams{
			Contexts:        flags.Contexts,
			ClientTimeout:   flags.ClientTimeout,
			PollingInterval: localFlags.PollingInterval,
			Namespace:       localFlags.Namespace,
			ResourceStatus:  localFlags.ResourceStatus,
			Name:            localFlags.Name,
		}

		// Execute the status command logic
		return ExecuteStatus(cmd.Context(), params)
	},
}

func init() {
	// Initialize flags for the status command
	// This separation keeps flag definitions isolated from command execution logic
	localFlags.AddFlags(Cmd)
}
