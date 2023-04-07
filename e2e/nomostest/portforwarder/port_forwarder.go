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
	// setPortCallback is a callback which will be invoked whenever the Pod changes and the
	// port-forward is recreated.
	setPortCallback func(int, string)
	// mux is a mutex to synchronize getting/setting the port-forward value
	mux sync.Mutex
	// localPort is the local port which forwards to the Pod
	localPort int
	// pod is the name of the current Pod that is being forwarded to
	pod string
	// cmd is the port forward subprocess
	cmd *exec.Cmd
}

// PortForwardOpt is an optional parameter for PortForwarder
type PortForwardOpt func(pf *PortForwarder)

// WithSetPortCallback registers a callback that will be invoked whenever the
// deployment's underlying pod changes and the port-forward is recreated.
func WithSetPortCallback(setFn func(int, string)) PortForwardOpt {
	return func(pf *PortForwarder) {
		pf.setPortCallback = setFn
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
	if pf.localPort == 0 {
		err := pf.start()
		return pf.localPort, err
	}
	return pf.localPort, nil
}

func (pf *PortForwarder) setPort(cmd *exec.Cmd, port int, pod string, async bool) {
	pf.logger.Infof("updating port-forward %s:%d -> %s:%d", pf.pod, pf.localPort, pod, port)
	if async {
		pf.mux.Lock()
	}
	pf.cmd = cmd
	pf.localPort = port
	pf.pod = pod
	if async {
		pf.mux.Unlock()
	}
	if pf.setPortCallback != nil {
		pf.setPortCallback(port, pod)
	}
}

// portForwardToPod establishes a port forwarding to the provided pod name. This
// should normally only be used as a helper function for start
func (pf *PortForwarder) portForwardToPod(pod string, async bool) error {
	pf.logger.Infof("starting port-forward process for %s/%s %s", pf.ns, pod, pf.port)
	cmd := exec.CommandContext(pf.ctx, "kubectl", "--kubeconfig", pf.kubeConfigPath, "port-forward",
		"-n", pf.ns, pod, pf.port)

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Start()
	if err != nil {
		return err
	}
	if stderr.Len() != 0 {
		return fmt.Errorf(stderr.String())
	}

	// Log command exit
	go func() {
		err := cmd.Wait()
		if err != nil {
			pf.logger.Infof("command exited for port-forward to pod %s/%s: %v", pf.ns, pod, err)
		} else {
			pf.logger.Infof("command exited for port-forward to pod %s/%s: no error", pf.ns, pod)
		}
	}()

	localPort := 0
	// In CI, 1% of the time this takes longer than 20 seconds, so 30 seconds seems
	// like a reasonable amount of time to wait.
	took, err := retry.Retry(30*time.Second, func() error {
		select {
		case <-pf.ctx.Done():
			return pf.ctx.Err()
		default:
		}
		s := stdout.String()
		if !strings.Contains(s, "\n") {
			return fmt.Errorf("nothing written to stdout for kubectl port-forward, stdout=%s", s)
		}

		line := strings.Split(s, "\n")[0]

		// Sample output:
		// Forwarding from 127.0.0.1:44043
		_, err = fmt.Sscanf(line, "Forwarding from 127.0.0.1:%d", &localPort)
		if err != nil {
			return fmt.Errorf("unable to parse port-forward output: %q", s)
		}
		return nil
	})
	if err != nil {
		return err
	}
	pf.logger.Infof("took %v to wait for port-forward to pod %s/%s (localhost:%d)", took, pf.ns, pod, localPort)
	pf.setPort(cmd, localPort, pod, async)
	return nil
}

func (pf *PortForwarder) portForwardToDeployment(async bool) error {
	select {
	case <-pf.ctx.Done():
		// Don't bother, subprocess already terminated/terminating
		return errors.Wrap(pf.ctx.Err(), "failed to start port forward process")
	default:
	}
	// Kill previous port forward process. This isn't strictly necessary since the
	// subprocess will be killed with the context, and we get a random port every
	// time. However, this cleans up subprocesses in the interim.
	if pf.cmd != nil {
		pf.logger.Infof("stopping port-forward process for %s/%s", pf.ns, pf.deployment)
		if err := pf.cmd.Process.Kill(); err != nil && errors.Is(err, os.ErrProcessDone) {
			return errors.Wrap(err, "failed to kill port forward process")
		}
	}
	pf.logger.Infof("starting port-forward process for %s/%s", pf.ns, pf.deployment)
	pod, err := pf.kubeClient.GetDeploymentPod(pf.deployment, pf.ns, pf.retryTimeout)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to get deployment pod for %s/%s", pf.ns, pf.deployment))
	}
	err = pf.portForwardToPod(pod.Name, async)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to start port forward for %s/%s", pf.ns, pf.deployment))
	}
	return nil
}

// start creates a port forward to a deployment's pod and keeps it open,
// even if the pod is recreated.
// Once ctx is cancelled, the port forward will be closed and will no longer be recreated
func (pf *PortForwarder) start() error {
	if err := pf.portForwardToDeployment(false); err != nil {
		return err
	}
	// Restart the port forward if the process exits prematurely (e.g. due to pod eviction)
	go func() {
		for {
			if err := pf.watcher.WatchForNotFound(kinds.Pod(), pf.pod, pf.ns, testwatcher.WatchTimeout(0), testwatcher.WatchContext(pf.ctx)); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// stop retrying, the subprocess will be killed with the context.
					return
				}
				// Watch automatically retries, so this shouldn't happen unless
				// there's a terminal error.
				pf.logger.Infof("Watch exited, restarting port-forward for %s/%s: %v", pf.ns, pf.deployment, err)
			}
			if err := pf.portForwardToDeployment(true); err != nil {
				pf.logger.Info(err)
				// this is a potentially fatal error since we failed to restart the port
				// forward. we log the error and return in case the test is already done
				// with the port forward. the test will otherwise error the next time it
				// tries to use the local port.
				return
			}
		}
	}()
	return nil
}
