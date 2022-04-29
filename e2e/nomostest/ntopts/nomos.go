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

package ntopts

import (
	"kpt.dev/configsync/pkg/importer/filesystem"
)

// Nomos configures options for installing Nomos on the test cluster.
type Nomos struct {
	filesystem.SourceFormat

	// MultiRepo indicates that NT should setup and test multi-repo behavior
	// rather than mono-repo behavior.
	MultiRepo bool

	// UpstreamURL upstream URL of repo we need to use for seeding
	UpstreamURL string
}

// Unstructured will set the option for unstructured repo.
func Unstructured(opts *New) {
	opts.Nomos.SourceFormat = filesystem.SourceFormatUnstructured
}
