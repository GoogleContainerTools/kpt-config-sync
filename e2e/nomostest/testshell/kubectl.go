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

package testshell

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"

	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
)

// Kubectl is a convenience method for calling kubectl against the
// currently-connected cluster. Returns STDOUT, and an error if kubectl exited
// abnormally.
//
// If you want to fail the test immediately on failure, use MustKubectl.
func (tc *TestShell) Kubectl(args ...string) ([]byte, error) {
	return tc.KubectlContext(tc.Context, args...)
}

// KubectlContext is similar to TestShell.Kubectl but allows using a context to
// cancel (kill signal) the kubectl command.
func (tc *TestShell) KubectlContext(ctx context.Context, args ...string) ([]byte, error) {
	// Ensure field manager is specified
	if stringArrayContains(args, "apply") && !stringArrayContains(args, "--field-manager") {
		args = append(args, "--field-manager", testkubeclient.FieldManager)
	}
	tc.Logger.Debugf("kubectl %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Env = tc.env()
	out, err := cmd.CombinedOutput()
	if err != nil {
		// log output, if not deliberately cancelled
		if isSignalExitError(err, syscall.SIGKILL) && ctx.Err() == context.Canceled {
			tc.Logger.Debugf("command cancelled: kubectl %s", strings.Join(args, " "))
		} else {
			if !tc.Logger.IsDebugEnabled() {
				tc.Logger.Infof("kubectl %s", strings.Join(args, " "))
			}
			tc.Logger.Info(string(out))
			tc.Logger.Infof("kubectl error: %v", err)
		}
		return out, err
	}
	return out, nil
}

// isSignalExitError returns true if the error is an ExitError caused by the
// specified signal.
func isSignalExitError(err error, sig syscall.Signal) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	if exitErr.ProcessState == nil {
		return false
	}
	status, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus) // unix/posix
	if !ok {
		return false
	}
	if !status.Signaled() {
		return false
	}
	return status.Signal() == sig
}

func stringArrayContains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
