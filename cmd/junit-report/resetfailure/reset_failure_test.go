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

package resetfailure

import (
	"io"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var (
	source   = "../../../e2e/testdata/junit-report.xml"
	expected = "../../../e2e/testdata/junit-report-updated.xml"
)

func TestResetFailure(t *testing.T) {
	tmpFile, err := os.CreateTemp(os.TempDir(), "junit-report-test-*.xml")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err = os.RemoveAll(tmpFile.Name()); err != nil {
			t.Error(err)
		}
	})

	report, err := os.OpenFile(source, os.O_RDONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = io.Copy(tmpFile, report); err != nil {
		t.Fatal(err)
	}

	if err = ResetFailure(tmpFile.Name()); err != nil {
		t.Fatalf("expected no error, but got %v", err)
	}

	updatedContent, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}

	expectedContent, err := os.ReadFile(expected)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(updatedContent, expectedContent); diff != "" {
		t.Fatalf("expected no diff, got %s", diff)
	}
}
