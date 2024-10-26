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

package testshell

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"kpt.dev/configsync/e2e/nomostest/testlogger"
)

// TestShell is a helper utility to execute shell commands in a test.
// Handles logging and injection of the KUBECONFIG as argument or env var.
type TestShell struct {
	// Context to use, if not specified by the method.
	Context context.Context

	// Env to use for all commands, if non-nil.
	// Default: os.Environ()
	Env []string

	// Logger for methods to use.
	Logger *testlogger.TestLogger
}

func (tc *TestShell) execCommandWithDebug(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.CombinedOutput()
	if err != nil {
		if !tc.Logger.IsDebugEnabled() {
			tc.Logger.Infof("%s %s", cmd.Path, strings.Join(cmd.Args, " "))
		}
		tc.Logger.Info(string(out))
		return out, err
	}
	return out, nil
}

// ExecWithDebug is a convenience method for invoking a subprocess with the
// KUBECONFIG environment variable and debug logging.
func (tc *TestShell) ExecWithDebug(name string, args ...string) ([]byte, error) {
	tc.Logger.Debugf("%s %s", name, strings.Join(args, " "))
	return tc.execCommandWithDebug(tc.Command(name, args...))
}

// ExecWithDebugEnv is similar to ExecWithDebug but allows passing additional
// environment variables for the command execution.
func (tc *TestShell) ExecWithDebugEnv(name string, envVars map[string]string, args ...string) ([]byte, error) {
	tc.Logger.Debugf("%s %s %s", name, envVars, strings.Join(args, " "))
	cmd := tc.Command(name, args...)
	cmd.Env = tc.env()
	for k, v := range envVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	return tc.execCommandWithDebug(cmd)
}

// Command is a convenience method for invoking a subprocess with the
// KUBECONFIG environment variable set. Setting the environment variable
// directly in the test process is not thread safe.
func (tc *TestShell) Command(name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(tc.Context, name, args...)
	cmd.Env = tc.env()
	return cmd
}

func (tc *TestShell) env() []string {
	if tc.Env != nil {
		return tc.Env
	}
	return os.Environ()
}
