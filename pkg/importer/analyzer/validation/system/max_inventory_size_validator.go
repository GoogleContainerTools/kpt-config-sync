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

// DefaultMaxInventorySizeBytes is the default maximum byte size for a json
// object in etcd.
const DefaultMaxInventorySizeBytes = 1572864 // 1.5MiB in OSS Kubernetes, 3145728 (3MiB) in GKE

// MaxInventorySizeCode is the error code for MaxInventorySizeError
const MaxInventorySizeCode = "1071"

var maxInventorySizeErrorBuilder = status.NewErrorBuilder(MaxInventorySizeCode)

// MaxInventorySizeError reports that the source will create an inventory object
// that is larger than can be stored in etcd.
func MaxInventorySizeError(max, found int) status.Error {
	return maxInventorySizeErrorBuilder.
		Sprintf(`Maximum inventory size exceeded. `+
			`Estimated inventory object size is %d bytes, but the maximum in etcd defaults to %d bytes. `+
			`To fix, split the resources across multiple repositories.`,
			found, max).
		Build()
}
