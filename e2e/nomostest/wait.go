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
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// WaitOption is an optional parameter for Wait
type WaitOption func(wait *waitSpec)

type waitSpec struct {
	timeout time.Duration
	// failOnError is the flag to control whether to fail the test or not when errors occur.
	failOnError bool
}

// WaitTimeout provides the timeout option to Wait.
func WaitTimeout(timeout time.Duration) WaitOption {
	return func(wait *waitSpec) {
		wait.timeout = timeout
	}
}

// WaitNoFail sets failOnError to false so the Wait function only logs the error but not fails the test.
func WaitNoFail() WaitOption {
	return func(wait *waitSpec) {
		wait.failOnError = false
	}
}

// Wait provides a logged wait for condition to return nil with options for timeout.
// It fails the test on errors.
func Wait(t testing.NTB, opName string, timeout time.Duration, condition func() error, opts ...WaitOption) {
	t.Helper()

	wait := waitSpec{
		timeout:     timeout,
		failOnError: true,
	}
	for _, opt := range opts {
		opt(&wait)
	}

	// Wait for the repository to report it is synced.
	took, err := Retry(wait.timeout, condition)
	if err != nil {
		t.Logf("failed after %v to wait for %s", took, opName)
		if wait.failOnError {
			t.Fatal(err)
		}
	}
	t.Logf("took %v to wait for %s", took, opName)
}

// WaitForNotFound waits for the passed object to be fully deleted.
// Immediately fails the test if the object is not deleted within the timeout.
func WaitForNotFound(nt *NT, gvk schema.GroupVersionKind, name, namespace string, opts ...WaitOption) {
	nt.T.Helper()

	Wait(nt.T, fmt.Sprintf("wait for %q %v to be not found", name, gvk), nt.DefaultWaitTimeout, func() error {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		return nt.ValidateNotFound(name, namespace, u)
	}, opts...)
}

// WaitForCurrentStatus waits for the passed object to reconcile.
func WaitForCurrentStatus(nt *NT, gvk schema.GroupVersionKind, name, namespace string, opts ...WaitOption) {
	nt.T.Helper()

	Wait(nt.T, fmt.Sprintf("wait for %q %v to reach current status", name, gvk), nt.DefaultWaitTimeout, func() error {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(gvk)
		return nt.Validate(name, namespace, u, StatusEquals(nt, kstatus.CurrentStatus))
	}, opts...)
}

// WaitForConfigSyncReady validates if the config sync deployments are ready.
func WaitForConfigSyncReady(nt *NT, nomos ntopts.Nomos) error {
	return ValidateMultiRepoDeployments(nt)
}

// WaitForNamespace waits for a namespace to exist and be ready to use
func WaitForNamespace(nt *NT, timeout time.Duration, namespace string) {
	nt.T.Helper()

	// Wait for the repository to report it is synced.
	took, err := Retry(timeout, func() error {
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
