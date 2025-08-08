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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/client/restconfig"
	"kpt.dev/configsync/pkg/version"
)

func init() {
	flags.AddContexts(Cmd)
	Cmd.Flags().DurationVar(&flags.ClientTimeout, "timeout", restconfig.DefaultTimeout, "Timeout for connecting to each cluster")
}

var (
	// Cmd is the Cobra object representing the nomos version command.
	Cmd = &cobra.Command{
		Use:   "version",
		Short: "Prints the version of ACM for each cluster as well this CLI",
		Long: `Prints the version of Configuration Management installed on each cluster and the version
of the "nomos" client binary for debugging purposes.`,
		Example: `  nomos version`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Don't show usage on error, as argument validation passed.
			cmd.SilenceUsage = true

			allCfgs, err := version.AllKubectlConfigs()
			version.Print(cmd.Context(), allCfgs, os.Stdout)

			if err != nil {
				return fmt.Errorf("unable to parse kubectl config: %w", err)
			}
			return nil
		},
	}
)
