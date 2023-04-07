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

var errTerminal = errors.New("terminal PortForwarder error")

func isTerminalError(err error) bool {
	return errors.Is(err, errTerminal)
}

func newTerminalError(format string, args ...interface{}) error {
	return errors.Wrapf(errTerminal, format, args...)
}

// EventType is a type of event sent by PortForwarder
type EventType string

const (
	// ErrorEvent indicates a fatal error occurred which caused PortForwarder to exit
	ErrorEvent EventType = "ERROR"
	// InitEvent indicates PortForwarder was started and is ready
	InitEvent EventType = "INIT"
	// DoneEvent indicates PortForwarder exited gracefully
	DoneEvent EventType = "DONE"
)

// Event is a signal that PortForwarder emits on its event channel
type Event struct {
	Type    EventType
	Message string
}

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
	// onReadyCallback is a callback which will be invoked whenever the Pod changes and the
	// port-forward is recreated.
	onReadyCallback func(int, string)
	// mux is a mutex to synchronize getting/setting the port-forward value
	mux sync.Mutex
	// localPort is the local port which forwards to the Pod
	localPort int
	// pod is the name of the current Pod that is being forwarded to
	pod string
	// cmd is the port forward subprocess
	cmd *exec.Cmd
	// isReady indicates whether the port forward is currently ready
	isReady bool
	// isStarted indicates whether the PortForwarder has been started
	isStarted bool
}

// PortForwardOpt is an optional parameter for PortForwarder
type PortForwardOpt func(pf *PortForwarder)

// WithOnReadyCallback registers a callback that will be invoked whenever the
// deployment's underlying pod changes and the port-forward is recreated.
// The callback must not invoke LocalPort from inside the callback, as this will
// lead to a deadlock.
func WithOnReadyCallback(onReadyFunc func(int, string)) PortForwardOpt {
	return func(pf *PortForwarder) {
		pf.onReadyCallback = onReadyFunc
	}
}

// NewPortForwarder creates a new instance of a PortForwarder
func NewPortForwarder(
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
	if !pf.isReady {
		return pf.localPort, fmt.Errorf("PortForwarder for Deployment %s/%s is not ready", pf.ns, pf.deployment)
	}
	return pf.localPort, nil
}

func (pf *PortForwarder) update(cmd *exec.Cmd, port int, pod string) {
	pf.logger.Infof("updating port-forward %s:%d -> %s:%d", pf.pod, pf.localPort, pod, port)
	pf.mux.Lock()
	defer pf.mux.Unlock()
	pf.cmd = cmd
	pf.localPort = port
	pf.pod = pod
	pf.isReady = true
	if pf.onReadyCallback != nil {
		pf.onReadyCallback(port, pod)
	}
}

func (pf *PortForwarder) notReady() {
	pf.mux.Lock()
	defer pf.mux.Unlock()
	pf.isReady = false
}

// portForwardToPod establishes a port forwarding to the provided pod name. This
// should normally only be used as a helper function for start
func (pf *PortForwarder) portForwardToPod(pod string) error {
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
	pf.logger.Infof("took %v to wait for port-forward to pod %s/%s (localhost:%d) (pid:%d)", took, pf.ns, pod, localPort, cmd.Process.Pid)
	pf.update(cmd, localPort, pod)
	return nil
}

func (pf *PortForwarder) portForwardToDeployment() error {
	select {
	case <-pf.ctx.Done():
		// Don't bother, subprocess already terminated/terminating
		return errors.Wrap(pf.ctx.Err(), "failed to start port forward process")
	default:
	}
	pf.logger.Infof("starting port-forward process for %s/%s", pf.ns, pf.deployment)
	pod, err := pf.kubeClient.GetDeploymentPod(pf.deployment, pf.ns, pf.retryTimeout)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to get deployment pod for %s/%s", pf.ns, pf.deployment))
	}
	err = pf.portForwardToPod(pod.Name)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to start port forward for %s/%s", pf.ns, pf.deployment))
	}
	return nil
}

// Start creates a port forward to a deployment's pod and keeps it open,
// even if the pod is recreated.
// Once ctx is cancelled, the port forward will be closed and will no longer be recreated.
// Returns an unbuffered channel that returns instances of Event. Consumers must
// read from this channel. The channel will be closed when the PortForwarder stops.
func (pf *PortForwarder) Start(ctx context.Context) chan Event {
	pf.mux.Lock()
	defer pf.mux.Unlock()
	// defend against the PortForwarder being started more than once
	if pf.isStarted {
		panic("PortForwarder already started")
	}
	pf.isStarted = true
	pf.ctx = ctx
	eventChan := make(chan Event)
	// Restart the port forward if the process exits prematurely (e.g. due to pod eviction)
	go func() {
		defer close(eventChan)
		for { // loop forever until context is done or a terminal error
			err := pf.startAndWait(eventChan)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				pf.logger.Infof("PortForwarder for pod %s/%s received done signal", pf.ns, pf.pod)
				eventChan <- Event{Type: DoneEvent, Message: err.Error()}
				return
			} else if err != nil && isTerminalError(err) {
				err = errors.Wrapf(err, "PortForwarder for Pod %s/%s encountered a fatal error", pf.ns, pf.pod)
				pf.logger.Info(err)
				eventChan <- Event{Type: ErrorEvent, Message: err.Error()}
				return
			}
			if err != nil {
				pf.logger.Infof("port-forward must be restarted for Pod %s/%s: %v", pf.ns, pf.pod, err)
			} else {
				pf.logger.Infof("port-forward must be restarted for Pod %s/%s: nil error", pf.ns, pf.pod)
			}
		}
	}()
	return eventChan
}

// startAndWait will start the port-forward and block until seeing an indicator
// that the port-forward is no longer healthy. If that occurs, it will return.
func (pf *PortForwarder) startAndWait(eventChan chan Event) error {
	defer pf.notReady()
	if err := pf.portForwardToDeployment(); err != nil {
		return err
	}
	eventChan <- Event{
		Type:    InitEvent,
		Message: fmt.Sprintf("PortForwarder listening on localhost:%d", pf.localPort),
	}
	return pf.waitForFailure()
}

// waitForFailure waits for any failure condition which indicates the port-forward
// must be restarted. The port-forward must be restarted if:
// - the subprocess is killed
// - the pod is restarted
func (pf *PortForwarder) waitForFailure() (err error) {
	ctx, cancel := context.WithCancel(pf.ctx)
	cmdChan := wrapWithChannel(func() error {
		return pf.waitForCmdFailure(ctx)
	})
	podChan := wrapWithChannel(func() error {
		return pf.waitForPodFailure(ctx)
	})
	defer func() {
		// before we return, cancel and wait for goroutines to exit
		cancel()
		// If any channel returns a TerminalError, return it instead of original error.
		// This informs upstream logic that a fatal error has occurred, and we must exit the retry loop.
		cmdErr, ok := <-cmdChan
		if ok && isTerminalError(cmdErr) {
			err = cmdErr
		}
		podErr, ok := <-podChan
		if ok && isTerminalError(podErr) {
			err = podErr
		}
	}()
	// wait for any of the channels to return. a happy little race
	select {
	case err = <-cmdChan:
		return err
	case err = <-podChan:
		return err
	}
}

// waitForCmdFailure waits for the port-forward subprocess to be killed until ctx is closed.
// If ctx is closed, the subprocess will be killed before returning.
// An error is returned to trigger port-forward restart.
func (pf *PortForwarder) waitForCmdFailure(ctx context.Context) error {
	// cmd.Wait does not accept a context, so create a wrapper that cancels with the ctx
	cmdWaitChan := wrapWithChannel(pf.cmd.Wait)
	var chanErr error
	select {
	case <-ctx.Done():
		// stop waiting for the command to exit, so we can kill it
		chanErr = ctx.Err()
	case err := <-cmdWaitChan:
		pf.logger.Infof("port-forward process for %s/%s exited: %v", pf.ns, pf.pod, err)
		return err
	}
	pf.logger.Infof("killing port-forward process for %s/%s", pf.ns, pf.pod)
	// kill the subprocess. This should force the cmd.Wait goroutine to return, if it hasn't already.
	if err := pf.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return newTerminalError("failed to kill port-forward process: %v", err)
	}
	// wait for cmdWaitChan to close, i.e. cmd.Wait returns
	<-cmdWaitChan
	return chanErr
}

// waitForPodFailure waits for the Pod to be NotFound until ctx is closed.
// If the Pod is NotFound, an error is returned to trigger port-forward restart.
// Otherwise, returns the error from WatchForNotFound.
func (pf *PortForwarder) waitForPodFailure(ctx context.Context) error {
	err := pf.watcher.WatchForNotFound(kinds.Pod(), pf.pod, pf.ns,
		testwatcher.WatchTimeout(0), testwatcher.WatchContext(ctx))
	if err == nil {
		// Pod not found, return an error
		return fmt.Errorf("%s/%s Pod not found", pf.ns, pf.pod)
	}
	return err
}

// wrapWithChannel takes a blocking function and invokes it asynchronously in a
// goroutine. A channel is returned which will be written to when the blocking
// function returns.
func wrapWithChannel(fn func() error) chan error {
	errorCh := make(chan error)
	go func() {
		defer close(errorCh)
		errorCh <- fn()
	}()
	return errorCh
}
