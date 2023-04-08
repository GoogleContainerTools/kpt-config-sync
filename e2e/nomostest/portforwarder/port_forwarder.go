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

package portforwarder

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/kinds"
)

// PortForwarder creates a port forwarding from localhost to a Deployment in the
// cluster. Assumes the Deployment has a single Pod, and keeps the port forward
// open if the Pod is deleted.
type PortForwarder struct {
	// kubeClient is used for list/get operations on k8s objects in the cluster
	kubeClient *testkubeclient.KubeClient
	// watcher is used to watch k8s objects in the cluster
	watcher *testwatcher.Watcher
	// logger is used for logging
	logger *testlogger.TestLogger
	// retryTimeout is used to determine the retry duration
	retryTimeout time.Duration
	// ctx should be a cancellable Context. Once ctx is cancelled, the port forward
	// will be closed and will no longer be recreated.
	ctx context.Context
	// kubeConfigPath is the path to the kube config file for the cluster
	kubeConfigPath string
	// ns is the Namespace of the Deployment/Pod
	ns string
	// deployment is the name of the Deployment. Assumes a single Pod replica.
	deployment string
	// port is the port on the Pod to forward to
	port string
	// onReadyFunc is a callback which will be invoked whenever the
	// PortForwarder becomes ready.
	onReadyFunc func(int, string)

	// mux protects the subprocess and ready flag
	mux sync.Mutex
	// subprocess is a reference to the currently running port-forward command
	subprocess *subprocess
	// ready is true if the deployment is ready and the port-forward is running.
	ready bool
}

type subprocess struct {
	// cmd is the port forward subprocess
	cmd *exec.Cmd
	// pod is the name of the Pod being forwarded to
	pod string
	// localPort is the local port which forwards to the Pod
	localPort int
}

// PortForwardOpt is an optional parameter for PortForwarder
type PortForwardOpt func(pf *PortForwarder)

// WithOnReady registers a callback that will be invoked whenever the
// PortForwarder becomes ready.
func WithOnReady(onReadyFunc func(int, string)) PortForwardOpt {
	return func(pf *PortForwarder) {
		pf.onReadyFunc = onReadyFunc
	}
}

// NewPortForwarder creates a new instance of a PortForwarder
func NewPortForwarder(
	ctx context.Context,
	kubeClient *testkubeclient.KubeClient,
	watcher *testwatcher.Watcher,
	logger *testlogger.TestLogger,
	retryTimeout time.Duration,
	kubeConfigPath, ns, deployment, port string,
	opts ...PortForwardOpt,
) *PortForwarder {
	pf := &PortForwarder{
		kubeClient:     kubeClient,
		watcher:        watcher,
		logger:         logger,
		ctx:            ctx,
		kubeConfigPath: kubeConfigPath,
		ns:             ns,
		deployment:     deployment,
		port:           port,
		retryTimeout:   retryTimeout,
	}
	for _, opt := range opts {
		opt(pf)
	}
	return pf
}

// LocalPort returns the current localhost port which forwards to the pod.
// lazily starts the port forward when called the first time.
func (pf *PortForwarder) LocalPort() (int, error) {
	pf.mux.Lock()
	defer pf.mux.Unlock()
	if pf.subprocess == nil {
		err := pf.start()
		if err != nil {
			return 0, err
		}
		return pf.subprocess.localPort, nil
	} else if !pf.ready {
		// TODO: block until ready?
		return 0, errors.Errorf("port-forward not ready to deployment %s/%s",
			pf.ns, pf.deployment)
	}
	return pf.subprocess.localPort, nil
}

// updateSubprocessAsync calls updateSubprocess with the lock
func (pf *PortForwarder) updateSubprocessAsync(proc *subprocess, ready bool) {
	pf.mux.Lock()
	defer pf.mux.Unlock()
	pf.updateSubprocess(proc, ready)
}

// updateSubprocess handles changes to the subprocess
func (pf *PortForwarder) updateSubprocess(proc *subprocess, ready bool) {
	switch {
	case ready:
		// Subprocess become ready
		pf.logger.Infof("updating port-forward localhost:%d -> %s:%d (ready: %v)", proc.localPort, proc.pod, pf.port, ready)
		pf.subprocess = proc
		pf.ready = true
		// Start watching for process exit, which will toggle ready back to false.
		go pf.waitAndUpdateSubprocess(proc)
		if pf.onReadyFunc != nil {
			// Execute the callback in a goroutine to allow it to use LocalPort
			// without creating a deadlock.
			go pf.onReadyFunc(proc.localPort, proc.pod)
		}

	case proc == nil:
		// Starting...
		pf.subprocess = nil
		pf.ready = false

	case proc == pf.subprocess:
		// Stopping...
		pf.logger.Infof("updating port-forward localhost:%d -> %s:%d (ready: %v)", proc.localPort, proc.pod, pf.port, ready)
		pf.ready = false
		// Kill the subprocess, if it's still running
		if err := proc.cmd.Process.Kill(); err != nil {
			if !errors.Is(err, os.ErrProcessDone) {
				pf.logger.Infof("Warning: failed to kill port-forward process to pod %s/%s: %v", pf.ns, proc.pod, err)
			}
		}
	}
	// default: !ready && proc != pf.subprocess
	// Subprocess became not ready, but a replacement has already started.
}

// waitAndUpdateSubprocess waits for the subprocess to exit, then marks the
// PortForwarder as not ready.
func (pf *PortForwarder) waitAndUpdateSubprocess(proc *subprocess) {
	err := proc.cmd.Wait()
	if err != nil {
		pf.logger.Infof("command exited for port-forward to pod %s/%s: %v", pf.ns, proc.pod, err)
	} else {
		pf.logger.Infof("command exited for port-forward to pod %s/%s: no error", pf.ns, proc.pod)
	}
	pf.updateSubprocessAsync(proc, false)
}

// portForwardToPod establishes port forwarding to the provided pod name.
// Subfunction of portForwardToDeployment. Call with lock engaged.
func (pf *PortForwarder) portForwardToPod(pod string) (*subprocess, error) {
	pf.logger.Infof("starting port-forward process for %s/%s %s", pf.ns, pod, pf.port)
	cmd := exec.CommandContext(pf.ctx, "kubectl", "--kubeconfig", pf.kubeConfigPath, "port-forward",
		"-n", pf.ns, pod, pf.port)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	if stderr.Len() != 0 {
		return nil, errors.Errorf("stderr: %s", stderr.String())
	}

	// Detect early termination
	exitCh := make(chan error)
	go func() {
		defer close(exitCh)
		// cmd.Wait() can only be called once, so use cmd.Process.Wait().
		state, err := cmd.Process.Wait()
		if err == nil && !state.Success() {
			// Simulate error from cmd.Wait()
			err = &exec.ExitError{ProcessState: state}
		}
		exitCh <- err
	}()

	localPort := 0
	// Wait for port-forward to start and parse the port from stdout
	took, err := retry.Retry(30*time.Second, func() error {
		select {
		case <-pf.ctx.Done():
			return errors.Wrap(pf.ctx.Err(), "exited before becoming ready")
		case err := <-exitCh:
			return errors.Wrap(err, "exited before becoming ready")
		default:
		}
		s := stdout.String()
		if !strings.Contains(s, "\n") {
			return errors.Errorf("no lines written to stdout for kubectl port-forward, stdout: %s", s)
		}

		line := strings.Split(s, "\n")[0]

		// Sample output:
		// Forwarding from 127.0.0.1:44043
		_, err = fmt.Sscanf(line, "Forwarding from 127.0.0.1:%d", &localPort)
		if err != nil {
			return errors.Errorf("unable to parse port-forward output: %q", s)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// We can't stop cmd.Process.Wait(), but we can at least consume the error
	// when it's done, to let its goroutine return.
	// The caller can start their own wait, if they want.
	go func() {
		<-exitCh
	}()
	pf.logger.Infof("took %v to wait for port-forward to pod %s/%s (localhost:%d)", took, pf.ns, pod, localPort)
	return &subprocess{
		cmd:       cmd,
		localPort: localPort,
		pod:       pod,
	}, nil
}

// portForwardToDeployment establishes port forwarding to the deployment's pod.
// Will wait until the deployment pod is ready.
// Subfunction of start. Call with lock engaged.
func (pf *PortForwarder) portForwardToDeployment() (*subprocess, error) {
	select {
	case <-pf.ctx.Done():
		// Don't bother, subprocess already terminated/terminating
		return nil, errors.Wrap(pf.ctx.Err(), "failed to start port forward process")
	default:
	}
	pf.logger.Infof("starting port-forward process for %s/%s", pf.ns, pf.deployment)
	pod, err := pf.kubeClient.GetDeploymentPod(pf.deployment, pf.ns, pf.retryTimeout)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get pod for deployment %s/%s", pf.ns, pf.deployment)
	}
	proc, err := pf.portForwardToPod(pod.Name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to start port forward for deployment %s/%s", pf.ns, pf.deployment)
	}
	return proc, nil
}

// start establishes port forwarding to a deployment's pod and keeps it open,
// even if the pod is recreated.
// Once ctx is cancelled, the subprocess will be killed and no longer recreated.
// Subfunction of LocalPort. Call with lock engaged.
func (pf *PortForwarder) start() error {
	pf.updateSubprocess(nil, false)
	proc, err := pf.portForwardToDeployment()
	if err != nil {
		return err
	}
	pf.updateSubprocess(proc, true)
	go pf.watchAndRestart(proc)
	return nil
}

// watchAndRestart watches the pod being forwarded to and restarts
// port forwarding if the pod becomes not found (e.g. due to pod crash or
// eviction).
// Subfunction of start. Call as a background goroutine.
func (pf *PortForwarder) watchAndRestart(proc *subprocess) {
	// When returning, mark the port-forward as no longer ready, even if it's still running.
	defer pf.updateSubprocessAsync(proc, false)
	for {
		if err := pf.watcher.WatchForNotFound(kinds.Pod(), proc.pod, pf.ns, testwatcher.WatchTimeout(0), testwatcher.WatchContext(pf.ctx)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// stop retrying, the subprocess will be killed with the context.
				return
			}
			// Watch automatically retries, so this shouldn't happen unless
			// there's a terminal error.
			pf.logger.Infof("Watch exited, restarting port-forward for deployment %s/%s: %v", pf.ns, pf.deployment, err)
		}
		pf.updateSubprocessAsync(proc, false)
		var err error
		proc, err = pf.portForwardToDeployment()
		if err != nil {
			// this is a potentially fatal error since we failed to restart the port
			// forward. we log the error and return in case the test is already done
			// with the port forward. the test will otherwise error the next time it
			// tries to use the local port.
			pf.logger.Infof("Warning: %v", err)
			return
		}
		pf.updateSubprocessAsync(proc, true)
	}
}
