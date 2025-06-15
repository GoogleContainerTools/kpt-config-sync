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

package bugreport

import (
	"time"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// BugreportFlags holds all the flags specific to the bugreport command
type BugreportFlags struct {
	// ClientTimeout is the timeout for connecting to the cluster
	ClientTimeout time.Duration
}

// NewBugreportFlags creates a new instance of BugreportFlags with default values
func NewBugreportFlags() *BugreportFlags {
	return &BugreportFlags{
		ClientTimeout: restconfig.DefaultTimeout,
	}
}

// AddFlags adds all bugreport-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (bf *BugreportFlags) AddFlags(cmd *cobra.Command) {
	// Add bugreport-specific flags
	cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", bf.ClientTimeout, "Timeout for connecting to the cluster")
}
