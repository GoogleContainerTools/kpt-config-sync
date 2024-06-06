// Copyright 2024 Google LLC
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

package metadata

import (
	"fmt"
	"hash/fnv"

	"k8s.io/apimachinery/pkg/util/validation"
)

const maxLabelLength = validation.LabelValueMaxLength // 63

// PackageID is a label value that uniquely identifies a RootSync or RepoSync.
// PACKAGE_ID = <PACKAGE_ID_FULL[0:53]>-<hex(fnv(PACKAGE_ID_FULL))>
// PACKAGE_ID_FULL = <NAME>.<NAMESPACE>.<RootSync|RepoSync>
// Design: go/config-sync-watch-filter
func PackageID(syncName, syncNamespace, syncKind string) string {
	packageID := fmt.Sprintf("%s.%s.%s", syncName, syncNamespace, syncKind)
	if len(packageID) <= maxLabelLength {
		return packageID
	}
	// fnv32a has slightly better avalanche characteristics than fnv32
	hasher := fnv.New32a()
	hasher.Write([]byte(fmt.Sprintf("%s.%s.%s", syncName, syncNamespace, syncKind)))
	// Converting 32-bit fnv to hex results in at most 8 characters.
	// Rarely it's fewer, so pad the prefix with zeros to make it consistent.
	suffix := fmt.Sprintf("%08x", hasher.Sum32())
	packageIDLen := maxLabelLength - len(suffix) - 1
	packageIDShort := packageID[0 : packageIDLen-1]
	return fmt.Sprintf("%s-%s", packageIDShort, suffix)
}
