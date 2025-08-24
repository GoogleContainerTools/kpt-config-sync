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
	"time"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// Flags holds all the flags specific to the status command
type Flags struct {
	// PollingInterval is the interval for continuous polling
	PollingInterval time.Duration
	// Namespace filters the output by specified namespace
	Namespace string
	// ResourceStatus determines whether to show detailed resource status
	ResourceStatus bool
	// Name filters the output by specified RootSync or RepoSync name
	Name string
}

// NewFlags creates a new instance of Flags with default values
func NewFlags() *Flags {
	return &Flags{
		PollingInterval: 0 * time.Second,
		Namespace:       "",
		ResourceStatus:  true,
		Name:            "",
	}
}

// AddFlags adds all status-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (sf *Flags) AddFlags(cmd *cobra.Command) {
	// Add shared flags from the global flags package
	flags.AddContexts(cmd)

	// Add status-specific flags
	cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", restconfig.DefaultTimeout,
		"Sets the timeout for connecting to each cluster. Defaults to 15 seconds. Example: --timeout=30s")
	cmd.Flags().DurationVar(&sf.PollingInterval, "poll", sf.PollingInterval,
		"Continuously polls for status updates at the specified interval. If not provided, the command runs only once. Example: --poll=30s for polling every 30 seconds")
	cmd.Flags().StringVar(&sf.Namespace, "namespace", sf.Namespace,
		"Filters the status output by the specified RootSync or RepoSync namespace. If not provided, displays status for all RootSync and RepoSync objects.")
	cmd.Flags().BoolVar(&sf.ResourceStatus, "resources", sf.ResourceStatus,
		"Displays detailed status for individual resources managed by RootSync or RepoSync objects. Defaults to true.")
	cmd.Flags().StringVar(&sf.Name, "name", sf.Name,
		"Filters the status output by the specified RootSync or RepoSync name.")
}
