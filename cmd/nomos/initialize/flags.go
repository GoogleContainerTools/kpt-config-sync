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

package initialize

import (
	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
)

// Flags holds all the flags specific to the initialize command
type Flags struct {
	// Force determines whether to write to directory even if nonempty, overwriting conflicting files
	Force bool
}

// NewFlags creates a new instance of Flags with default values
func NewFlags() *Flags {
	return &Flags{
		Force: false,
	}
}

// AddFlags adds all initialize-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (inf *Flags) AddFlags(cmd *cobra.Command) {
	// Add shared flags from the global flags package
	flags.AddPath(cmd)

	// Add initialize-specific flags
	cmd.Flags().BoolVar(&inf.Force, "force", inf.Force,
		"write to directory even if nonempty, overwriting conflicting files")
}
