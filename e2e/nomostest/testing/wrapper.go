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

package testing

import (
	"strings"
	"testing"
	"time"

	"kpt.dev/configsync/e2e"
)

const errorPrefix = "ERROR:"

// wrapper wraps testing.T to implement NTB with some custom behavior
type wrapper struct {
	*testing.T // embed struct for parent methods (e.g. Helper)
}

// NewShared creates a new instance of the Testing Wrapper that provides additional
// functionality beyond the standard testing package.
// It doesn't require the testFeature param.
func NewShared(t *FakeNTB) NTB {
	t.Helper()

	testFeaturesFlag := *e2e.TestFeatures
	if testFeaturesFlag != "" {
		features := strings.Split(testFeaturesFlag, ",")
		for _, f := range features {
			sanitizedFeature := strings.TrimSpace(f)
			if !KnownFeature(Feature(sanitizedFeature)) {
				t.Fatalf("Test failed because the test-features flag has an unknown feature %q", sanitizedFeature)
			}
		}
	}

	return t
}

// New creates a new instance of the Testing Wrapper that provides additional
// functionality beyond the standard testing package.
func New(t *testing.T, testFeature Feature) NTB {
	t.Helper()

	if !KnownFeature(testFeature) {
		t.Fatalf("Test failed because the feature %q is unknown", testFeature)
	}

	skip := true
	testFeaturesFlag := *e2e.TestFeatures
	if testFeaturesFlag == "" {
		skip = false
	} else {
		features := strings.Split(testFeaturesFlag, ",")
		for _, f := range features {
			sanitizedFeature := strings.TrimSpace(f)
			if !KnownFeature(Feature(sanitizedFeature)) {
				t.Fatalf("Test failed because the test-features flag has an unknown feature %q", sanitizedFeature)
			} else if Feature(f) == testFeature {
				skip = false
			}
		}
	}
	if skip {
		t.Skipf("Skip the test because the feature %q is not included in the test-features flag %q", testFeature, testFeaturesFlag)
	}

	return &wrapper{
		T: t,
	}
}

// Error is equivalent to Log followed by Fail.
func (w *wrapper) Error(args ...interface{}) {
	w.Helper()
	args = injectErrorPrefix(args...)
	args = injectTimePrefix(args...)
	w.T.Error(args...)
}

// Errorf is equivalent to Logf followed by Fail.
func (w *wrapper) Errorf(format string, args ...interface{}) {
	w.Helper()
	format, args = injectErrorPrefixF(format, args...)
	format, args = injectTimePrefixF(format, args...)
	w.T.Errorf(format, args...)
}

// Fatal is equivalent to Log followed by FailNow.
func (w *wrapper) Fatal(args ...interface{}) {
	w.Helper()
	args = injectErrorPrefix(args...)
	args = injectTimePrefix(args...)
	w.T.Fatal(args...)
}

// Fatalf is equivalent to Logf followed by FailNow.
func (w *wrapper) Fatalf(format string, args ...interface{}) {
	w.Helper()
	format, args = injectErrorPrefixF(format, args...)
	format, args = injectTimePrefixF(format, args...)
	w.T.Fatalf(format, args...)
}

// Log generates the output. It's always at the same stack depth.
func (w *wrapper) Log(args ...interface{}) {
	w.Helper()
	args = injectTimePrefix(args...)
	w.T.Log(args...)
}

// Logf formats its arguments according to the format, analogous to Printf, and
// records the text in the error log.
func (w *wrapper) Logf(format string, args ...interface{}) {
	w.Helper()
	format, args = injectTimePrefixF(format, args...)
	w.T.Logf(format, args...)
}

// Skip is equivalent to Log followed by SkipNow.
func (w *wrapper) Skip(args ...interface{}) {
	args = injectTimePrefix(args...)
	w.T.Skip(args...)
}

// Skipf is equivalent to Logf followed by SkipNow.
func (w *wrapper) Skipf(format string, args ...interface{}) {
	format, args = injectTimePrefixF(format, args...)
	w.T.Skipf(format, args...)
}

func injectTimePrefix(args ...interface{}) []interface{} {
	return append([]interface{}{time.Now().UTC()}, args...)
}

func injectErrorPrefix(args ...interface{}) []interface{} {
	return append([]interface{}{errorPrefix}, args...)
}

func injectTimePrefixF(format string, args ...interface{}) (string, []interface{}) {
	return "%s " + format, append([]interface{}{time.Now().UTC()}, args...)
}

func injectErrorPrefixF(format string, args ...interface{}) (string, []interface{}) {
	return "%s " + format, append([]interface{}{errorPrefix}, args...)
}
