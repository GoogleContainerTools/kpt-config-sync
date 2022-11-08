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

// Package git provides functionality related to Git repos.
package git

import (
	"fmt"
	"io/ioutil"

	"kpt.dev/configsync/pkg/api/configsync"
)

const (
	// ErrorFile is the file name of the git-sync error file.
	ErrorFile = "error.json"
)

// SyncError returns the error details from the error file generated by the git-sync|oci-sync process.
func SyncError(container, filepath, label string) string {
	content, err := ioutil.ReadFile(filepath)
	if err != nil {
		return fmt.Sprintf("Unable to load %s: %v. Please check %s logs for more info: kubectl logs -n %s -l %s -c %s", filepath, err, container, configsync.ControllerNamespace, label, container)
	}
	if len(content) == 0 {
		return fmt.Sprintf("%s is empty. Please check %s logs for more info: kubectl logs -n %s -l %s -c %s", filepath, container, configsync.ControllerNamespace, label, container)
	}
	return fmt.Sprintf("Error in the %s container: %s", container, string(content))
}
