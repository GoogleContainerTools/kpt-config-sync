// Copyright 2023 Google LLC
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

package testlogger

import "kpt.dev/configsync/e2e/nomostest/testing"

// TestLogger wraps testing.NTB to add optional debug logging, without exposing
// the ability to error or fatally terminate the test.
type TestLogger struct {
	// T is the test environment for the current test.
	t testing.NTB
	// DebugEnabled enables debug logging when true
	debugEnabled bool
}

// New constructs a new TestLogger
func New(t testing.NTB, debugEnabled bool) *TestLogger {
	return &TestLogger{
		t:            t,
		debugEnabled: debugEnabled,
	}
}

// IsDebugEnabled returns true if debug is enabled for this test
func (tl *TestLogger) IsDebugEnabled() bool {
	return tl.debugEnabled
}

// Debug only prints the log message if debug is enabled.
// Use for verbose logs that can be enabled by developers, but won't show in CI.
func (tl *TestLogger) Debug(args ...interface{}) {
	if tl.debugEnabled {
		tl.t.Log(args...)
	}
}

// Debugf only prints the log message, with Sprintf-like formatting, if debug is
// enabled.
// Use for verbose logs that can be enabled by developers, but won't show in CI.
func (tl *TestLogger) Debugf(format string, args ...interface{}) {
	if tl.debugEnabled {
		tl.t.Logf(format, args...)
	}
}

// Info prints the message to the test log.
func (tl *TestLogger) Info(args ...interface{}) {
	tl.t.Log(args...)
}

// Infof prints the message to the test log, with Sprintf-like formatting.
func (tl *TestLogger) Infof(format string, args ...interface{}) {
	tl.t.Logf(format, args...)
}
