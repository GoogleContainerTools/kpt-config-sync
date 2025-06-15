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
	"fmt"
	"os"
)

// ExecParams contains all parameters needed to execute the initialize command
// This struct is completely independent of cobra command structures
type ExecParams struct {
	Path  string
	Force bool
}

// ExecuteInitialize executes the core initialize command logic without any cobra dependencies
// This function encapsulates all the business logic for the initialize command
func ExecuteInitialize(params ExecParams) error {
	err := Initialize(params.Path, params.Force)
	if err != nil {
		return err
	}

	// Print success message (equivalent to PostRunE in the original command)
	_, err = fmt.Fprintf(os.Stdout, "Done!\n")
	return err
}
