// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	repoNameMaxLen  = 63
	repoNameHashLen = 8
)

// SanitizeRepoName replaces all slashes with hyphens, and truncate the name.
// repo name may contain between 3 and 63 lowercase letters, digits and hyphens.
func SanitizeRepoName(repoPrefix, name string) string {
	fullName := "cs-e2e-" + repoPrefix + "-" + name
	hashBytes := sha1.Sum([]byte(fullName))
	hashStr := hex.EncodeToString(hashBytes[:])[:repoNameHashLen]

	if len(fullName) > repoNameMaxLen-1-repoNameHashLen {
		fullName = fullName[:repoNameMaxLen-1-repoNameHashLen]
	}
	return fmt.Sprintf("%s-%s", strings.ReplaceAll(fullName, "/", "-"), hashStr)
}
