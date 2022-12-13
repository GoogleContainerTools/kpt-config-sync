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

package mutate

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Func is a function which mutates an existing object into its desired state.
type Func func() error

// NoUpdateError tells the caller that no update is required.
// Use with `WithRetry` and `Status` by returning a NoUpdateError from the
// `mutate.Func`.
type NoUpdateError struct{}

// Error returns the error message string
func (nue *NoUpdateError) Error() string {
	return "no update required"
}

// WithRetry attempts to update an object until successful or update becomes
// unnecessary (`NoUpdateError` from the `mutate.Func`). Retries are quick, with
// no backoff.
//
//   - If the update errors due to an API status error, the object will
//     be re-read from the server, re-mutated, and re-updated.
//   - If the update errors due to a ResourceVersion conflict, the object will
//     be re-read from the server, re-mutated, and re-updated.
//   - If the update errors due to a UID conflict, that means the object has
//     been deleted and re-created. So an error will be returned.
//   - If the mutate.Func returns a *NoUpdateError, the update will be skipped
//     and retries will be stopped.
func WithRetry(ctx context.Context, c client.Client, obj client.Object, mutate Func) (bool, error) {
	// UID must be set already, so we can error if it changes.
	uid := obj.GetUID()
	if uid == "" {
		return false, fmt.Errorf("failed to update object: metadata.uid is empty: %s", objSummary(obj))
	}
	objKey := client.ObjectKeyFromObject(obj)
	// Wrapped status errors are retriable. All others are terminal.
	retriable := func(err error) bool {
		if _, ok := err.(status.Error); ok {
			return true
		}
		return false
	}
	retryErr := retry.OnError(retry.DefaultRetry, retriable, func() error {
		if err := mutate(); err != nil {
			return err
		}
		if updateErr := c.Update(ctx, obj); updateErr != nil {
			// If the update fails due to ResourceVersion conflict, get the latest
			// version of the object, remove the finalizer, and retry.
			if apierrors.IsConflict(updateErr) {
				fmt.Printf("WithRetry update conflict: %v", updateErr)
				getErr := c.Get(ctx, objKey, obj)
				if getErr != nil {
					// Return the GET error & retry
					return status.APIServerError(getErr,
						fmt.Sprintf("failed to get latest version of object: %s", objSummary(obj)))
				}
				if obj.GetUID() != uid {
					// Stop retrying
					return fmt.Errorf("failed to update object: metadata.uid has changed: %s", objSummary(obj))
				}
				// Retry with the updated object
			}
			// Return the UPDATE error & retry
			return status.APIServerError(updateErr,
				fmt.Sprintf("failed to update object: %s", objSummary(obj)))
		}
		return nil
	})
	if retryErr != nil {
		if _, ok := retryErr.(*NoUpdateError); ok {
			// No change necessary.
			return false, nil
		}
		return false, retryErr
	}
	return true, nil
}

// Status attempts to update an object's status, once.
//
//   - If the update errors due to a UID conflict, that means the object has
//     been deleted and re-created. So an error will be returned.
//   - If the mutate.Func returns a *NoUpdateError, the update will be skipped.
func Status(ctx context.Context, c client.Client, obj client.Object, mutate Func) (bool, error) {
	// UID must be set already, so we can error if it changes.
	uid := obj.GetUID()
	if uid == "" {
		return false, fmt.Errorf("failed to update object: metadata.uid is empty: %s", objSummary(obj))
	}
	objKey := client.ObjectKeyFromObject(obj)

	if getErr := c.Get(ctx, objKey, obj); getErr != nil {
		// Return the GET error
		return false, status.APIServerError(getErr,
			fmt.Sprintf("failed to get latest version of object: %s", objSummary(obj)))
	}
	if obj.GetUID() != uid {
		// Object replaced
		return false, fmt.Errorf("failed to update object status: the UID has changed: %s", objSummary(obj))
	}

	if err := mutate(); err != nil {
		if _, ok := err.(*NoUpdateError); ok {
			// No change necessary.
			return false, nil
		}
		return false, err
	}

	if updateErr := c.Status().Update(ctx, obj); updateErr != nil {
		// Return the UPDATE error
		return false, status.APIServerError(updateErr,
			fmt.Sprintf("failed to update object status: %s", objSummary(obj)))
	}
	return true, nil
}

func objSummary(obj client.Object) string {
	if uObj, ok := obj.(*unstructured.Unstructured); ok {
		return fmt.Sprintf("%T %s %s/%s", obj,
			uObj.GetObjectKind().GroupVersionKind(),
			obj.GetNamespace(), obj.GetName())
	}
	return fmt.Sprintf("%T %s/%s", obj,
		obj.GetNamespace(), obj.GetName())
}
