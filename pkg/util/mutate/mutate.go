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
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/pkg/kinds"
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

// Spec attempts to update an object until successful or update becomes
// unnecessary (`NoUpdateError` from the `mutate.Func`). Retries are quick, with
// no backoff.
// Returns an error if the status update fails OR if the mutate func fails.
func Spec(ctx context.Context, c client.Client, obj client.Object, mutateFn Func, opts ...client.UpdateOption) (bool, error) {
	client := &specClient{client: c, updateOptions: opts}
	return withRetry(ctx, client, obj, mutateFn)
}

// Status attempts to update the status of an object until successful or update
// becomes unnecessary (`NoUpdateError` from the `mutate.Func`). Retries are
// quick, with no backoff.
// Returns an error if the status update fails OR if the mutate func fails OR if
// the generation changes before the status update succeeds.
func Status(ctx context.Context, c client.Client, obj client.Object, mutateFn Func, opts ...client.UpdateOption) (bool, error) {
	oldGen := obj.GetGeneration()
	mutateFn2 := func() error {
		newGen := obj.GetGeneration()
		if obj.GetGeneration() != oldGen {
			// If the spec changed, stop trying to update the status.
			// When this happens, the controller should re-reconcile with the new spec.
			return fmt.Errorf("generation changed while attempting to update status: %d -> %d",
				oldGen, newGen)
		}
		return mutateFn()
	}
	client := &statusClient{client: c, updateOptions: convertUpdateOptions(opts)}
	return withRetry(ctx, client, obj, mutateFn2)
}

// withRetry attempts to update an object until successful or update becomes
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
func withRetry(ctx context.Context, c updater, obj client.Object, mutate Func) (bool, error) {
	// UID must be set already, so we can error if it changes.
	uid := obj.GetUID()
	if uid == "" {
		return false, c.WrapError(ctx, obj, errors.New("metadata.uid is empty"))
	}
	// Mutate the object from the server
	if err := mutate(); err != nil {
		if noUpdateErr := (&NoUpdateError{}); errors.As(err, &noUpdateErr) {
			// No change necessary.
			return false, nil
		}
		return false, c.WrapError(ctx, obj, fmt.Errorf("mutating object: %w", err))
	}
	retryErr := retry.OnError(retry.DefaultRetry, RetriableOrConflict, func() error {
		// If ResourceVersion is NOT set, request the latest object from the server.
		// Otherwise, assume the object was previously retrieved.
		if obj.GetResourceVersion() == "" {
			if getErr := c.Get(ctx, obj); getErr != nil {
				// Return the GET error & retry
				return fmt.Errorf("failed to get latest version of object: %w", getErr)
			}
			if obj.GetUID() != uid {
				// Stop retrying
				return errors.New("metadata.uid has changed: object may have been re-created")
			}
			// Mutate the latest object from the server
			if err := mutate(); err != nil {
				// Stop retrying
				return fmt.Errorf("failed to mutate object: %w", err)
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
		err := c.WrapError(ctx, obj, retryErr)
		if statusErr := apierrors.APIStatus(nil); errors.As(retryErr, &statusErr) {
			// The error (or an error it wraps) is a known status error from K8s
			return false, status.APIServerErrorWrap(err, obj)
		}
		return false, err
	}
	return true, nil
}

// updater is a simplified client interface for performing reads and writes on
// an object, without options.
type updater interface {
	// Get the specified object.
	Get(context.Context, client.Object) error

	// Update the specified object.
	Update(context.Context, client.Object) error

	// WrapError returns the specified error wrapped with extra context specific
	// to this updater.
	WrapError(context.Context, client.Object, error) error
}

type specClient struct {
	client        client.Client
	updateOptions []client.UpdateOption
}

// Get the current spec of the specified object.
func (c *specClient) Get(ctx context.Context, obj client.Object) error {
	return c.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)
}

// Update the spec of the specified object.
func (c *specClient) Update(ctx context.Context, obj client.Object) error {
	return c.client.Update(ctx, obj, c.updateOptions...)
}

// WrapError returns the specified error wrapped with extra context specific
// to this updater.
func (c *specClient) WrapError(_ context.Context, obj client.Object, err error) error {
	return fmt.Errorf("failed to update object: %s: %w", kinds.ObjectSummary(obj), err)
}

type statusClient struct {
	client        client.Client
	updateOptions []client.SubResourceUpdateOption
}

// Get the current status of the specified object.
func (c *statusClient) Get(ctx context.Context, obj client.Object) error {
	return c.client.Get(ctx, client.ObjectKeyFromObject(obj), obj)
}

// Update the status of the specified object.
func (c *statusClient) Update(ctx context.Context, obj client.Object) error {
	return c.client.Status().Update(ctx, obj, c.updateOptions...)
}

// WrapError returns the specified error wrapped with extra context specific
// to this updater.
func (c *statusClient) WrapError(_ context.Context, obj client.Object, err error) error {
	return fmt.Errorf("failed to update object status: %s: %w", kinds.ObjectSummary(obj), err)
}

// convertUpdateOptions converts []client.UpdateOption to []client.SubResourceUpdateOption
func convertUpdateOptions(optList []client.UpdateOption) []client.SubResourceUpdateOption {
	if len(optList) == 0 {
		return nil
	}
	opts := &client.UpdateOptions{}
	opts.ApplyOptions(optList)
	return []client.SubResourceUpdateOption{
		&client.SubResourceUpdateOptions{UpdateOptions: *opts},
	}
}
