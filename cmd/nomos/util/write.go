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

package util

import (
	"io"
	"text/tabwriter"
)

// Status enums shared across nomos subcommands.
const (
	// ErrorMsg indicates that an error has occurred when retrieving or calculating a field value.
	ErrorMsg = "ERROR"
	// NotInstalledMsg indicates that ACM is not installed on a cluster.
	NotInstalledMsg = "NOT INSTALLED"
	// NotRunningMsg indicates that ACM is installed but not running on a cluster.
	NotRunningMsg = "NOT RUNNING"
	// NotConfiguredMsg indicates that ACM is installed but not configured for a cluster.
	NotConfiguredMsg = "NOT CONFIGURED"
	// UnknownMsg indicates that a field's value is unknown or unavailable.
	UnknownMsg = "UNKNOWN"
)

// NewWriter returns a standardized writer for the CLI for writing tabular output to the console.
func NewWriter(out io.Writer) *tabwriter.Writer {
	padding := 3
	return tabwriter.NewWriter(out, 0, 0, padding, ' ', 0)
}
