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
	"fmt"
	"os"
	"sync"
)

// NTB partially implements testing.TB
// It is used by the shared setup steps to invoke the per-test functions.
type NTB interface {
	Cleanup(func())
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Fail()
	FailNow()
	Failed() bool
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Helper()
	Log(args ...interface{})
	Logf(format string, args ...interface{})
	Name() string
	Skip(args ...interface{})
	SkipNow()
	Skipf(format string, args ...interface{})
	Skipped() bool
}

// FakeNTB implements NTB with standard print out and exit.
// It is used in `e2e/testcases/main_test.go` when `--share-test-env` is turned on.
type FakeNTB struct {
	mu     sync.RWMutex
	failed bool
}

// Error is equivalent to Log followed by Fail.
func (t *FakeNTB) Error(args ...interface{}) {
	t.Log(args...)
	t.Fail()
}

// Errorf is equivalent to Logf followed by Fail.
func (t *FakeNTB) Errorf(format string, args ...interface{}) {
	t.Logf(format, args...)
	t.Fail()
}

// Fail marks the function as having failed but continues execution.
func (t *FakeNTB) Fail() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failed = true
}

// FailNow marks the function as having failed and stops its execution
// by calling runtime.Goexit (which then runs all deferred calls in the
// current goroutine).
func (t *FakeNTB) FailNow() {
	t.Fail()
	os.Exit(1)
}

// Failed reports whether the function has failed.
func (t *FakeNTB) Failed() bool {
	return t.failed
}

// Fatal is equivalent to Log followed by FailNow.
func (t *FakeNTB) Fatal(args ...interface{}) {
	t.Log(args...)
	t.FailNow()
}

// Fatalf is equivalent to Logf followed by FailNow.
func (t *FakeNTB) Fatalf(format string, args ...interface{}) {
	t.Logf(format, args...)
	t.FailNow()
}

// Name returns an empty string.
func (t *FakeNTB) Name() string {
	return ""
}

// Helper is a no-op function.
func (t *FakeNTB) Helper() {
}

// Cleanup is a no-op function.
func (t *FakeNTB) Cleanup(func()) {
}

// Skip is a no-op function.
func (t *FakeNTB) Skip(...interface{}) {
}

// SkipNow is a no-op function.
func (t *FakeNTB) SkipNow() {
}

// Skipf is a no-op function.
func (t *FakeNTB) Skipf(string, ...interface{}) {
}

// Skipped is a no-op function.
func (t *FakeNTB) Skipped() bool {
	return false
}

// Log generates the output. It's always at the same stack depth.
func (t *FakeNTB) Log(args ...interface{}) {
	fmt.Println(args...)
}

// Logf formats its arguments according to the format, analogous to Printf, and
// records the text in the error log.
func (t *FakeNTB) Logf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
	fmt.Println()
}
