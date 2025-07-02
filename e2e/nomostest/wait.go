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

package nomostest

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// WaitOption is an optional parameter for Wait
type WaitOption func(wait *waitSpec)

type waitSpec struct {
	timeout time.Duration
}

// WaitTimeout provides the timeout option to Wait.
func WaitTimeout(timeout time.Duration) WaitOption {
	return func(wait *waitSpec) {
		wait.timeout = timeout
	}
}

// Wait provides a logged wait for condition to return nil with options for timeout.
// It fails the test on errors.
func Wait(t testing.NTB, opName string, timeout time.Duration, condition func() error, opts ...WaitOption) error {
	t.Helper()

	wait := waitSpec{
		timeout: timeout,
	}
	for _, opt := range opts {
		opt(&wait)
	}

	// Wait for the repository to report it is synced.
	took, err := retry.Retry(wait.timeout, condition)
	t.Logf("took %v to wait for %s", took, opName)
	return err
}

// WaitForConfigSyncReady validates if the config sync deployments are ready.
func WaitForConfigSyncReady(nt *NT) error {
	return ValidateMultiRepoDeployments(nt)
}

// WaitForNamespace waits for a namespace to exist and be ready to use
func WaitForNamespace(nt *NT, timeout time.Duration, namespace string) error {
	nt.T.Helper()

	// Wait for the repository to report it is synced.
	took, err := retry.Retry(timeout, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(kinds.Namespace())
		obj.SetName(namespace)
		if err := nt.KubeClient.Get(namespace, "", obj); err != nil {
			return fmt.Errorf("namespace %q GET failed: %w", namespace, err)
		}
		return testpredicates.StatusEquals(nt.Scheme, status.CurrentStatus)(obj)
	})
	nt.T.Logf("took %v to wait for namespace %q to be ready", took, namespace)
	return err
}

// WaitForNamespaces waits for namespaces to exist and be ready to use
func WaitForNamespaces(nt *NT, timeout time.Duration, namespaces ...string) error {
	nt.T.Helper()

	tg := taskgroup.New()
	for _, namespace := range namespaces {
		ns := namespace
		tg.Go(func() error {
			return WaitForNamespace(nt, timeout, ns)
		})
	}
	return tg.Wait()
}
