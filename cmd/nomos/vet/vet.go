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

package vet

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
)

var (
	namespaceValue string
	keepOutput     bool
	threshold      int
	outPath        string
)

func init() {
	flags.AddClusters(Cmd)
	flags.AddPath(Cmd)
	flags.AddSkipAPIServerCheck(Cmd)
	flags.AddSourceFormat(Cmd)
	flags.AddOutputFormat(Cmd)
	flags.AddAPIServerTimeout(Cmd)
	Cmd.Flags().StringVar(&namespaceValue, "namespace", "",
		fmt.Sprintf(
			"If set, validate the repository as a Namespace Repo with the provided name. Automatically sets --source-format=%s",
			configsync.SourceFormatUnstructured))

	Cmd.Flags().BoolVar(&keepOutput, "keep-output", false,
		`If enabled, keep the hydrated output`)

	// The --threshold flag has three modes:
	// 1. When `--threshold` is not specified, the validation is disabled.
	// 2. When `--threshold` is specified with no value, the validation is enabled, using the default value, same as `--threshold=1000`.
	// 3. When `--threshold=1000` is specified with a value, the validation is enabled, using the specified maximum.
	Cmd.Flags().IntVar(&threshold, "threshold", system.DefaultMaxObjectCountDisabled,
		fmt.Sprintf(`Maximum objects allowed per repository; errors if exceeded. Omit or set to %d to disable. `, system.DefaultMaxObjectCountDisabled)+
			fmt.Sprintf(`Provide flag without value for default (%d), or use --threshold=N for a specific limit.`, system.DefaultMaxObjectCount))
	// Using NoOptDefVal allows the flag to be specified without a value, but
	// changes flag parsing such that the key and value must be in the same
	// argument, like `--threshold=1000`, instead of `--threshold 1000`.
	Cmd.Flags().Lookup("threshold").NoOptDefVal = strconv.Itoa(system.DefaultMaxObjectCount)

	Cmd.Flags().StringVar(&outPath, "output", flags.DefaultHydrationOutput,
		`Location of the hydrated output`)
}

// Cmd is the Cobra object representing the nomos vet command.
var Cmd = &cobra.Command{
	Use:   "vet",
	Short: "Validate an Anthos Configuration Management directory",
	Long: `Validate an Anthos Configuration Management directory
Checks for semantic and syntactic errors in an Anthos Configuration Management directory
that will interfere with applying resources. Prints found errors to STDERR and
returns a non-zero error code if any issues are found.
`,
	Example: `  nomos vet
  nomos vet --path=my/directory
  nomos vet --path=/path/to/my/directory`,
	Args: cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, _ []string) error {
		// Don't show usage on error, as argument validation passed.
		cmd.SilenceUsage = true

		return runVet(cmd.Context(), cmd.OutOrStderr(), vetOptions{
			Namespace:        namespaceValue,
			SourceFormat:     configsync.SourceFormat(flags.SourceFormat),
			APIServerTimeout: flags.APIServerTimeout,
			MaxObjectCount:   threshold,
		})
	},
}
