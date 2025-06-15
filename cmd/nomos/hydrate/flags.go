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
)

// Flags holds all the flags specific to the hydrate command
type Flags struct {
	// Flat determines whether to print all output to a single file
	Flat bool
	// OutPath specifies the location to write hydrated configuration to
	OutPath string
}

// NewFlags creates a new instance of HydrateFlags with default values
func NewFlags() *Flags {
	return &Flags{
		Flat:    false,
		OutPath: flags.DefaultHydrationOutput,
	}
}

// AddFlags adds all hydrate-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (hf *Flags) AddFlags(cmd *cobra.Command) {
	// Add shared flags from the global flags package
	flags.AddClusters(cmd)
	flags.AddPath(cmd)
	flags.AddSkipAPIServerCheck(cmd)
	flags.AddSourceFormat(cmd)
	flags.AddOutputFormat(cmd)
	flags.AddAPIServerTimeout(cmd)

	// Add hydrate-specific flags
	cmd.Flags().BoolVar(&hf.Flat, "flat", hf.Flat,
		`If enabled, print all output to a single file`)
	cmd.Flags().StringVar(&hf.OutPath, "output", hf.OutPath,
		`Location to write hydrated configuration to.

If --flat is not enabled, writes each resource manifest as a
separate file. You may run "kubectl apply -fR" on the result to apply
the configuration to a cluster. If the repository declares any Cluster
resources, contains a subdirectory for each Cluster.

If --flat is enabled, writes to the, writes a single file holding all
resource manifests. You may run "kubectl apply -f" on the result to
apply the configuration to a cluster.`)
}
