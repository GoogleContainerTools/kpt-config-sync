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

package util

import (
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

var (
	// SourceRetryBackoff sets retry timeout for `SourceCommitAndDirWithRetry`.
	SourceRetryBackoff = BackoffWithDurationLimit(5 * time.Minute)
	// HydratedRetryBackoff sets retry timeout for `readHydratedDirWithRetry`.
	HydratedRetryBackoff = BackoffWithDurationLimit(time.Minute)
)

// RetriableError represents a transient error that is retriable.
type RetriableError struct {
	err error
}

// NewRetriableError returns a RetriableError
func NewRetriableError(err error) error {
	return &RetriableError{err}
}

// Error implements the Error function of the interface.
func (r *RetriableError) Error() string {
	return r.err.Error()
}

// Is makes RetriableErrors comparable.
func (r *RetriableError) Is(target error) bool {
	if target == nil {
		return false
	}
	re, ok := target.(*RetriableError)
	if !ok {
		return false
	}
	return errors.Is(r.err, re.err)
}

var _ error = &RetriableError{}

// IsErrorRetriable returns if the error is retriable.
func IsErrorRetriable(err error) bool {
	_, ok := err.(*RetriableError)
	return ok
}

// BackoffWithDurationLimit returns backoff with a duration limit in 10 steps.
// Here is an example of the duration between steps:
//
//	1.055843837s, 2.085359785s, 4.229560375s, 8.324724174s, 16.295984061s,
//	34.325711987s, 1m5.465642392s, 2m18.625713221s, 4m24.712222056s, 9m18.97652295s.
func BackoffWithDurationLimit(duration time.Duration) wait.Backoff {
	return wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   2,
		Steps:    10,
		Cap:      duration,
		Jitter:   0.1,
	}
}

// RetryWithBackoff retries the function with the default backoff with a given retry limit.
func RetryWithBackoff(backoff wait.Backoff, f func() error) error {
	return retry.OnError(backoff, IsErrorRetriable, func() error {
		err := f()
		if err != nil {
			klog.Info(err)
		}
		return err
	})
}
