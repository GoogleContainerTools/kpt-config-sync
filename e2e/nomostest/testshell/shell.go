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
	"syscall"

	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"

	"github.com/pkg/errors"
)

// TestShell is a helper utility to execute shell commands in a test.
// Handles logging and injection of the KUBECONFIG as argument or env var.
type TestShell struct {
	// Context to use, if not specified by the method.
	Context context.Context
	// KubeConfigPath is the path to the kubeconfig file for the cluster,
	// used for kubectl commands.
	KubeConfigPath string
	// Logger for methods to use.
	Logger *testlogger.TestLogger
}

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
	prefix := []string{"--kubeconfig", tc.KubeConfigPath}
	args = append(prefix, args...)
	// Ensure field manager is specified
	if stringArrayContains(args, "apply") && !stringArrayContains(args, "--field-manager") {
		args = append(args, "--field-manager", testkubeclient.FieldManager)
	}
	tc.Logger.Debugf("kubectl %s", strings.Join(args, " "))
	out, err := exec.CommandContext(ctx, "kubectl", args...).CombinedOutput()
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

// Git is a convenience method for calling git.
// Returns STDOUT & STDERR combined, and an error if git exited abnormally.
func (tc *TestShell) Git(args ...string) ([]byte, error) {
	return tc.ExecWithDebug("git", args...)
}

// Docker is a convenience method for calling docker.
// Returns STDOUT & STDERR combined, and an error if docker exited abnormally.
func (tc *TestShell) Docker(args ...string) ([]byte, error) {
	return tc.ExecWithDebug("docker", args...)
}

// Helm is a convenience method for calling helm.
// Returns STDOUT & STDERR combined, and an error if helm exited abnormally.
func (tc *TestShell) Helm(args ...string) ([]byte, error) {
	return tc.ExecWithDebug("helm", args...)
}

// ExecWithDebug is a convenience method for invoking a subprocess with the
// KUBECONFIG environment variable and debug logging.
func (tc *TestShell) ExecWithDebug(name string, args ...string) ([]byte, error) {
	tc.Logger.Debugf("%s %s", name, strings.Join(args, " "))
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if !tc.Logger.IsDebugEnabled() {
			tc.Logger.Infof("%s %s", name, strings.Join(args, " "))
		}
		tc.Logger.Info(string(out))
		return out, err
	}
	return out, nil
}

// Command is a convenience method for invoking a subprocess with the
// KUBECONFIG environment variable set. Setting the environment variable
// directly in the test process is not thread safe.
func (tc *TestShell) Command(name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(tc.Context, name, args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("KUBECONFIG=%s", tc.KubeConfigPath))
	return cmd
}

func stringArrayContains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
