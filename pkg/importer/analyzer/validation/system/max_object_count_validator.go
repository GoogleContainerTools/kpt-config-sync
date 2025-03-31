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

package system

import (
	"kpt.dev/configsync/pkg/status"
)

// DefaultMaxObjectCount is the default maximum number of objects allowed in a
// single inventory, if the validator is enabled.
//
// This is only used by nomos vet, not the reconciler. It will not block syncing.
//
// The value 1000 was chosen as a compromise between scale and safety.
// Technically we know form e2e tests that the inventory can actually hold at
// least 5000 objects without needing to disable the status, however, this is
// unsafe to do in production because each of those objects could error, which
// adds error conditions to the inventory status, which can significantly
// increase the size of the inventory.
const DefaultMaxObjectCount int = 1000

// DefaultMaxObjectCountDisabled is a sentinel value to disable max object count
// validation. It's used when the --threshold flag is not specified.
const DefaultMaxObjectCountDisabled int = 0

// MaxObjectCountCode is the error code for MaxObjectCount
const MaxObjectCountCode = "1070"

var maxObjectCountErrorBuilder = status.NewErrorBuilder(MaxObjectCountCode)

// MaxObjectCountError reports that the source includes more than the maximum
// number of objects.
func MaxObjectCountError(maxN, foundN int) status.Error {
	return maxObjectCountErrorBuilder.
		Sprintf(`Maximum number of objects exceeded. Found %d, but expected no more than %d. `+
			`Reduce the number of objects being synced to this cluster in your source of truth `+
			`to prevent your ResourceGroup inventory object from exceeding the etcd object size limit. `+
			`For instructions on how to break up a repository into multiple repositories, see `+
			`https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/how-to/breaking-up-repo`,
			foundN, maxN).
		Build()
}
