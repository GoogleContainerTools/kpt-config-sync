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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/net"
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

// Retriable identifies Kubernetes apiserver errors that are normally transient
// and safely retriable.
func Retriable(err error) bool {
	return apierrors.IsInternalError(err) ||
		apierrors.IsServiceUnavailable(err) ||
		net.IsConnectionRefused(err)
}

// RetriableOrConflict identifies Kubernetes apiserver errors that are either
// retriable or a conflict (retriable with GET + mutation).
func RetriableOrConflict(err error) bool {
	return Retriable(err) || apierrors.IsConflict(err)
}

// WithRetry attempts to update an object until successful or update becomes
// unnecessary (`NoUpdateError` from the `mutate.Func`). Retries are quick, with
// no backoff.
//
//   - If the update errors due to a `Retriable“ API status error, the object
//     will be re-read from the server, re-mutated, and re-updated.
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
		return false, fmt.Errorf("failed to update object: %s: metadata.uid is empty", objSummary(obj))
	}
	// Mutate the object from the server
	if err := mutate(); err != nil {
		if noUpdateErr := (&NoUpdateError{}); errors.As(err, &noUpdateErr) {
			// No change necessary.
			return false, nil
		}
		return false, errors.Wrap(err, "failed to mutate object")
	}
	objKey := client.ObjectKeyFromObject(obj)
	retryErr := retry.OnError(retry.DefaultRetry, RetriableOrConflict, func() error {
		// If ResourceVersion is NOT set, request the latest object from the server.
		// Otherwise, assume the object was previously retrieved.
		if obj.GetResourceVersion() == "" {
			if getErr := c.Get(ctx, objKey, obj); getErr != nil {
				// Return the GET error & retry
				return errors.Wrap(getErr, "failed to get latest version of object")
			}
			if obj.GetUID() != uid {
				// Stop retrying
				return errors.New("metadata.uid has changed: object may have been re-created")
			}
			// Mutate the latest object from the server
			if err := mutate(); err != nil {
				// Stop retrying
				return errors.Wrap(err, "failed to mutate object")
			}
		}
		if updateErr := c.Update(ctx, obj); updateErr != nil {
			// If the update fails due to ResourceVersion conflict, get the latest
			// version of the object, remove the finalizer, and retry.
			if apierrors.IsConflict(updateErr) {
				// Clear ResourceVersion to trigger GET on next retry
				obj.SetResourceVersion("")
			}
			// Return the UPDATE error & retry
			return updateErr
		}
		// Success! Stop retrying.
		return nil
	})
	if retryErr != nil {
		if noUpdateErr := (&NoUpdateError{}); errors.As(retryErr, &noUpdateErr) {
			// No change necessary.
			return false, nil
		}
		if statusErr := apierrors.APIStatus(nil); errors.As(retryErr, &statusErr) {
			// The error (or an error it wraps) is a known status error from K8s
			return false, status.APIServerError(retryErr, "failed to update object", obj)
		}
		return false, errors.Wrapf(retryErr, "failed to update object: %s", objSummary(obj))
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
		return false, fmt.Errorf("failed to update object: %s: metadata.uid is empty", objSummary(obj))
	}
	// Wrap inner part in a closure to simplify consistent error handling.
	// Errors from `Status`` and `WithRetry` should look similar, even tho
	// `Status` only makes one attempt.
	retryErr := func() error {
		// If ResourceVersion is NOT set, request the latest object from the server.
		// Otherwise, assume the object was previously retrieved.
		if obj.GetResourceVersion() == "" {
			objKey := client.ObjectKeyFromObject(obj)
			if getErr := c.Get(ctx, objKey, obj); getErr != nil {
				return errors.Wrap(getErr, "failed to get latest version of object")
			}
			if obj.GetUID() != uid {
				return errors.New("metadata.uid has changed: object may have been re-created")
			}
		}
		if err := mutate(); err != nil {
			return errors.Wrap(err, "failed to mutate object")
		}
		return c.Status().Update(ctx, obj)
	}()
	if retryErr != nil {
		if noUpdateErr := (&NoUpdateError{}); errors.As(retryErr, &noUpdateErr) {
			// No change necessary.
			return false, nil
		}
		if statusErr := apierrors.APIStatus(nil); errors.As(retryErr, &statusErr) {
			// The error (or an error it wraps) is a known status error from K8s
			return false, status.APIServerError(retryErr, "failed to update object status", obj)
		}
		return false, errors.Wrapf(retryErr, "failed to update object status: %s", objSummary(obj))
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
