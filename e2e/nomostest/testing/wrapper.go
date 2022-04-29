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

const errorPrefix = "ERROR:"

// Wrapper implements NTB.
type Wrapper struct {
	t NTB
}

// New creates a new instance of the Testing Wrapper that provides additional
// functionality beyond the standard testing package.
func New(t NTB) *Wrapper {
	t.Helper()

	tw := &Wrapper{
		t: t,
	}

	return tw
}

// Cleanup registers a function to be called when the test and all its
// subtests complete. Cleanup functions will be called in last added,
// first called order.
func (w *Wrapper) Cleanup(f func()) {
	w.t.Cleanup(f)
}

// Error is equivalent to Log followed by Fail.
func (w *Wrapper) Error(args ...interface{}) {
	args = append([]interface{}{errorPrefix}, args...)

	w.t.Helper()
	w.t.Error(args...)
}

// Errorf is equivalent to Logf followed by Fail.
func (w *Wrapper) Errorf(format string, args ...interface{}) {
	format = "%s " + format
	args = append([]interface{}{errorPrefix}, args...)

	w.t.Helper()
	w.t.Errorf(format, args...)
}

// Fail marks the function as having failed but continues execution.
func (w *Wrapper) Fail() {
	w.t.Fail()
}

// FailNow marks the function as having failed and stops its execution
// by calling runtime.Goexit (which then runs all deferred calls in the
// current goroutine).
func (w *Wrapper) FailNow() {
	w.t.FailNow()
}

// Failed reports whether the function has failed.
func (w *Wrapper) Failed() bool {
	return w.t.Failed()
}

// Fatal is equivalent to Log followed by FailNow.
func (w *Wrapper) Fatal(args ...interface{}) {
	args = append([]interface{}{errorPrefix}, args...)

	w.t.Helper()
	w.t.Fatal(args...)
}

// Fatalf is equivalent to Logf followed by FailNow.
func (w *Wrapper) Fatalf(format string, args ...interface{}) {
	format = "%s " + format
	args = append([]interface{}{errorPrefix}, args...)

	w.t.Helper()
	w.t.Fatalf(format, args...)
}

// Helper marks the calling function as a test helper function.
// When printing file and line information, that function will be skipped.
// Helper may be called simultaneously from multiple goroutines.
func (w *Wrapper) Helper() {
	w.t.Helper()
}

// Log generates the output. It's always at the same stack depth.
func (w *Wrapper) Log(args ...interface{}) {
	w.t.Helper()
	w.t.Log(args...)
}

// Logf formats its arguments according to the format, analogous to Printf, and
// records the text in the error log.
func (w *Wrapper) Logf(format string, args ...interface{}) {
	w.t.Helper()
	w.t.Logf(format, args...)
}

// Name returns the name of the running test or benchmark.
func (w *Wrapper) Name() string {
	return w.t.Name()
}

// Skip is equivalent to Log followed by SkipNow.
func (w *Wrapper) Skip(args ...interface{}) {
	w.t.Skip(args...)
}

// SkipNow marks the test as having been skipped and stops its execution
// by calling runtime.Goexit.
func (w *Wrapper) SkipNow() {
	w.t.SkipNow()
}

// Skipf is equivalent to Logf followed by SkipNow.
func (w *Wrapper) Skipf(format string, args ...interface{}) {
	w.t.Skipf(format, args...)
}

// Skipped reports whether the test was skipped.
func (w *Wrapper) Skipped() bool {
	return w.t.Skipped()
}
