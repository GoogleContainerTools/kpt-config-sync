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
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// WaitOption is an optional parameter for Wait
type WaitOption func(wait *waitSpec)

// WaitFailureStrategy indicates how to handle validation failures
type WaitFailureStrategy int

const (
	// WaitFailureStrategyLog calls Testing.Log when there's a validation failure
	WaitFailureStrategyLog WaitFailureStrategy = iota // Log
	// WaitFailureStrategyError calls Testing.Error when there's a validation failure
	WaitFailureStrategyError // Error
	// WaitFailureStrategyFatal calls Testing.Fatal when there's a validation failure
	WaitFailureStrategyFatal // Fatal
)

type waitSpec struct {
	timeout time.Duration
	// failureStrategy specifies how to handle failure
	failureStrategy WaitFailureStrategy
}

// WaitTimeout provides the timeout option to Wait.
func WaitTimeout(timeout time.Duration) WaitOption {
	return func(wait *waitSpec) {
		wait.timeout = timeout
	}
}

// WaitStrategy configures how the Wait function handles failure.
func WaitStrategy(strategy WaitFailureStrategy) WaitOption {
	return func(wait *waitSpec) {
		wait.failureStrategy = strategy
	}
}

// Wait provides a logged wait for condition to return nil with options for timeout.
// It fails the test on errors.
func Wait(t testing.NTB, opName string, timeout time.Duration, condition func() error, opts ...WaitOption) {
	t.Helper()

	wait := waitSpec{
		timeout:         timeout,
		failureStrategy: WaitFailureStrategyFatal,
	}
	for _, opt := range opts {
		opt(&wait)
	}

	// Wait for the repository to report it is synced.
	took, err := retry.Retry(wait.timeout, condition)
	if err != nil {
		t.Logf("failed after %v to wait for %s", took, opName)
		switch wait.failureStrategy {
		case WaitFailureStrategyFatal:
			t.Fatal(err)
		case WaitFailureStrategyError:
			t.Error(err)
		}
	}
	t.Logf("took %v to wait for %s", took, opName)
}

// WaitForConfigSyncReady validates if the config sync deployments are ready.
func WaitForConfigSyncReady(nt *NT) error {
	return ValidateMultiRepoDeployments(nt)
}

// WaitForNamespace waits for a namespace to exist and be ready to use
func WaitForNamespace(nt *NT, timeout time.Duration, namespace string) {
	nt.T.Helper()

	// Wait for the repository to report it is synced.
	took, err := retry.Retry(timeout, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   corev1.SchemeGroupVersion.Group,
			Version: corev1.SchemeGroupVersion.Version,
			Kind:    "Namespace",
		})
		obj.SetName(namespace)
		if err := nt.Get(namespace, "", obj); err != nil {
			return fmt.Errorf("namespace %q GET failed: %w", namespace, err)
		}
		result, err := status.Compute(obj)
		if err != nil {
			return fmt.Errorf("namespace %q status could not be computed: %w", namespace, err)
		}
		if result.Status != status.CurrentStatus {
			return fmt.Errorf("namespace %q not reconciled: status is %q: %s", namespace, result.Status, result.Message)
		}
		return nil
	})
	if err != nil {
		nt.T.Logf("failed after %v to wait for namespace %q to be ready", took, namespace)
		nt.T.Fatal(err)
	}
	nt.T.Logf("took %v to wait for namespace %q to be ready", took, namespace)
}

// WaitForNamespaces waits for namespaces to exist and be ready to use
func WaitForNamespaces(nt *NT, timeout time.Duration, namespaces ...string) {
	nt.T.Helper()

	var wg sync.WaitGroup
	for _, namespace := range namespaces {
		wg.Add(1)
		go func(t time.Duration, ns string) {
			defer wg.Done()
			WaitForNamespace(nt, t, ns)
		}(timeout, namespace)
	}
	wg.Wait()
}
