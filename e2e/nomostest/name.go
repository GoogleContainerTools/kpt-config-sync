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

package nomostest

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/e2e/nomostest/testing"
)

// re splits strings at word boundaries. Test names always begin with "Test",
// so we know we aren't missing any of the text.
var re = regexp.MustCompile(`[A-Z][^A-Z]*`)

// TestClusterName returns the name of the test cluster.
func TestClusterName(t testing.NTB) string {
	t.Helper()

	// Kind seems to allow a max cluster name length of 49.  If we exceed that, hash the
	// name, truncate to 40 chars then append 8 hash digits (32 bits).
	const nameLimit = 49
	const hashChars = 8
	n := testDirName(t)
	// handle legacy testcase filenames
	n = strings.ReplaceAll(n, "_", "-")

	if nameLimit < len(n) {
		hashBytes := sha1.Sum([]byte(n))
		hashStr := hex.EncodeToString(hashBytes[:])
		n = fmt.Sprintf("%s-%s", n[:nameLimit-1-hashChars], hashStr[:hashChars])
	}

	if errs := validation.IsDNS1123Subdomain(n); len(errs) > 0 {
		t.Fatalf("transformed test name %q into %q, which is not a valid Kind cluster name: %+v",
			t.Name(), n, errs)
	}
	return n
}

func testDirName(t testing.NTB) string {
	t.Helper()

	n := t.Name()
	// Capital letters are forbidden in Kind cluster names, so convert to
	// kebab-case.
	words := re.FindAllString(n, -1)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}

	n = strings.Join(words, "-")
	n = strings.ReplaceAll(n, "/", "--")
	return n
}
