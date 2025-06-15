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

// Flags holds all the flags specific to the vet command
type Flags struct {
	// NamespaceValue specifies the namespace for validation
	NamespaceValue string
	// KeepOutput determines whether to keep hydrated output
	KeepOutput bool
	// Threshold sets the maximum object count
	Threshold int
	// OutPath specifies the output location for hydrated output
	OutPath string
}

// NewFlags creates a new instance of Flags with default values
func NewFlags() *Flags {
	return &Flags{
		NamespaceValue: "",
		KeepOutput:     false,
		Threshold:      system.DefaultMaxObjectCountDisabled,
		OutPath:        flags.DefaultHydrationOutput,
	}
}

// AddFlags adds all vet-specific flags to the command
// This function centralizes flag definitions and keeps them separate from command logic
func (vf *Flags) AddFlags(cmd *cobra.Command) {
	// Add shared flags from the global flags package
	flags.AddClusters(cmd)
	flags.AddPath(cmd)
	flags.AddSkipAPIServerCheck(cmd)
	flags.AddSourceFormat(cmd)
	flags.AddOutputFormat(cmd)
	flags.AddAPIServerTimeout(cmd)

	// Add vet-specific flags
	cmd.Flags().StringVar(&vf.NamespaceValue, "namespace", vf.NamespaceValue,
		fmt.Sprintf(
			"If set, validate the repository as a Namespace Repo with the provided name. Automatically sets --source-format=%s",
			configsync.SourceFormatUnstructured))

	cmd.Flags().BoolVar(&vf.KeepOutput, "keep-output", vf.KeepOutput,
		`If enabled, keep the hydrated output`)

	// The --threshold flag has three modes:
	// 1. When `--threshold` is not specified, the validation is disabled.
	// 2. When `--threshold` is specified with no value, the validation is enabled, using the default value, same as `--threshold=1000`.
	// 3. When `--threshold=1000` is specified with a value, the validation is enabled, using the specified maximum.
	cmd.Flags().IntVar(&vf.Threshold, "threshold", vf.Threshold,
		fmt.Sprintf(`Maximum objects allowed per repository; errors if exceeded. Omit or set to %d to disable. `, system.DefaultMaxObjectCountDisabled)+
			fmt.Sprintf(`Provide flag without value for default (%d), or use --threshold=N for a specific limit.`, system.DefaultMaxObjectCount))
	// Using NoOptDefVal allows the flag to be specified without a value, but
	// changes flag parsing such that the key and value must be in the same
	// argument, like `--threshold=1000`, instead of `--threshold 1000`.
	cmd.Flags().Lookup("threshold").NoOptDefVal = strconv.Itoa(system.DefaultMaxObjectCount)

	cmd.Flags().StringVar(&vf.OutPath, "output", vf.OutPath,
		`Location of the hydrated output`)
}
