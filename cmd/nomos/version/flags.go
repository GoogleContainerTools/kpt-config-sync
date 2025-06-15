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

package version

import (
	"time"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// Flags holds all the flags specific to the version command
type Flags struct {
	// ClientTimeout is the timeout for connecting to each cluster
	ClientTimeout time.Duration
}

// NewVersionFlags creates a new instance of Flags with default values
func NewVersionFlags() *Flags {
	return &Flags{
		ClientTimeout: restconfig.DefaultTimeout,
	}
}

// AddFlags adds all version-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (vf *Flags) AddFlags(cmd *cobra.Command) {
	// Add shared flags from the global flags package
	flags.AddContexts(cmd)

	// Add version-specific flags
	cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", vf.ClientTimeout, "Timeout for connecting to each cluster")
}
