// Copyright 2025 Google LLC
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

package testcontroller

import (
	"context"
	"flag"
	"log"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// RunTestSuite runs a test suite with setup and cleanup.
func RunTestSuite(m *testing.M, setupFunc, cleanupFunc func() error) int {
	defer func() {
		if err := cleanupFunc(); err != nil {
			log.Printf("Error: test suite cleanup failed: %v", err)
		}
	}()
	if err := setupFunc(); err != nil {
		log.Printf("Error: test suite setup failed: %v", err)
		return 1 // skip tests
	}
	return m.Run()
}

// NewTestLogger builds a logr.Logger using the -v=# command flag from klog.
// This is useful for using to configured controllers and the controller-manager.
// Must be called after `klog.InitFlags` and `flag.Parse`.
// To see all logs, use:
// go test kpt.dev/configsync/<package-path> -v -args -v=5
func NewTestLogger(t *testing.T) logr.Logger {
	// Value is always a Getter, but needs to be cast for reverse-compat.
	// And klog.InitFlags registers the "v" flag as a klog.Level.
	klogLevel := flag.Lookup("v").Value.(flag.Getter).Get().(klog.Level)
	return testr.NewWithOptions(t, testr.Options{
		// klog allos configurable headers, which don't map well to a timestamp
		// bool, so just disable them.
		LogTimestamp: false,
		// klog.Level is an int32 but Verbosity wants an int.
		Verbosity: int(klogLevel),
	})
}

// StartTestManager starts the controller-manager in a go routine.
// Call the returned function in a defer to stop the manager and wait until it exits.
// This avoids async error logs from the test environment stopping before the
// controller-manager.
func StartTestManager(t *testing.T, mgr manager.Manager) func() {
	ctx := t.Context()

	ctx, cancel := context.WithCancel(ctx)
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		err := mgr.Start(ctx)
		assert.NoError(t, err)
	}()
	// Wait for the manager to be elected/ready, or the test to be stopped.
	select {
	case <-mgr.Elected():
	case <-ctx.Done():
	}
	return func() {
		cancel()
		// Wait for the manager to stop
		<-doneCh
	}
}
