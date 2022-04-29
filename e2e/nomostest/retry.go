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
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

// Retry calls the passed function until it returns nil, or the passed timeout
// expires.
//
// Retries once per second until timeout expires.
// Returns how long the function retried, and the last error if the command
// timed out.
func Retry(timeout time.Duration, fn func() error) (time.Duration, error) {
	start := time.Time{}
	diff := timeout
	err := retry.OnError(backoff(timeout), defaultErrorFilter, func() error {
		if start.IsZero() {
			start = time.Now()
		}
		err := fn()
		if err == nil {
			diff = time.Since(start)
		}
		return err
	})
	return diff, err
}

// backoff returns a wait.Backoff that retries exactly once per second until
// timeout expires.
func backoff(timeout time.Duration) wait.Backoff {
	// These are e2e tests and we aren't doing any sort of load balancing, so
	// for now we don't need to let all aspects of the backoff be configurable.

	// This creates a constant backoff that always retries after exactly one
	// second. See documentation for wait.Backoff for full explanation.
	//
	// No, we don't want to increase the interval each time we poll.
	// The test environment is not competing for bandwidth in any way where that
	// would help.
	return wait.Backoff{
		Duration: time.Second,
		Steps:    int(timeout / time.Second),
	}
}

// defaultErrorFilter returns false if the error's type indicates continuing
// will not produce a positive result.
func defaultErrorFilter(err error) bool {
	// The type expected by a Predicate is incorrect.
	return !errors.Is(err, ErrWrongType) &&
		// The type isn't registered in the Client's schema.
		!runtime.IsNotRegisteredError(err) &&
		// The type wasn't available on the API Server when the Client was created.
		!meta.IsNoMatchError(err)
}
