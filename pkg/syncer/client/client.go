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

// Package client contains an enhanced client.
package client

import (
	"context"
	"fmt"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/go-cmp/cmp"
	"github.com/prometheus/client_golang/prometheus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	m "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client extends the controller-runtime client by exporting prometheus metrics and retrying updates.
type Client struct {
	client.Client
	latencyMetric *prometheus.HistogramVec
	MaxTries      int
}

// New returns a new Client.
func New(client client.Client, latencyMetric *prometheus.HistogramVec) *Client {
	return &Client{
		Client:        client,
		MaxTries:      5,
		latencyMetric: latencyMetric,
	}
}

// clientUpdateFn is a Client function signature for updating an entire resource or a resource's status.
// Note: Update and Status Update now have a different option signature, so the
// options argument is currently omitted. Refactoring may be required if we need
// to support options.
type clientUpdateFn func(ctx context.Context, obj client.Object) error

// update is a function that updates the state of an API object. The argument is expected to be a copy of the object,
// so no there is no need to worry about mutating the argument when implementing an Update function.
type update func(client.Object) (client.Object, error)

// Create saves the object obj in the Kubernetes cluster and records prometheus metrics.
func (c *Client) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) status.Error {
	description := getResourceInfo(obj)
	klog.V(1).Infof("Creating %s", description)

	start := time.Now()
	err := c.Client.Create(ctx, obj, opts...)

	c.recordLatency(start, "Create", metrics.StatusLabel(err))
	m.RecordAPICallDuration(ctx, "create", m.StatusTagKey(err), start)

	if err != nil {
		switch {
		case apierrors.IsAlreadyExists(err):
			return ConflictCreateAlreadyExists(err, obj)
		case meta.IsNoMatchError(err), apierrors.IsNotFound(err):
			return ConflictCreateResourceDoesNotExist(err, obj)
		default:
			return status.APIServerError(err, "failed to create object", obj)
		}
	}

	klog.Infof("Created %s", description)
	return nil
}

// Delete deletes the given obj from Kubernetes cluster and records prometheus metrics.
// This automatically sets the propagation policy to always be "Background".
func (c *Client) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) status.Error {
	description := getResourceInfo(obj)
	namespacedName := getNamespacedName(obj)

	if err := c.Client.Get(ctx, namespacedName, obj); err != nil {
		switch {
		case apierrors.IsNotFound(err):
			// Object is already deleted
			klog.V(2).Infof("Delete skipped, object %s does not exist", description)
			return nil
		case meta.IsNoMatchError(err):
			// Resource is already deleted, implying the object was deleted
			klog.V(2).Infof("Delete skipped, resource for %s does not exist", description)
			return nil
		default:
			return status.ResourceWrap(err, "failed to get object for delete", obj)
		}
	}

	if isFinalizing(obj) {
		klog.V(2).Infof("Delete skipped, resource is finalizing %s", description)
		return nil
	}

	start := time.Now()
	opts = append(opts, client.PropagationPolicy(metav1.DeletePropagationBackground))
	err := c.Client.Delete(ctx, obj, opts...)

	switch {
	case err == nil:
		klog.Infof("Deleted %s", description)
	case apierrors.IsNotFound(err):
		klog.V(2).Infof("Object not found during attempted delete %s", description)
		err = nil
	case meta.IsNoMatchError(err):
		// Resource is already deleted, implying the object was deleted
		klog.V(2).Infof("Resource not found during attempted delete %s", description)
		err = nil
	}

	c.recordLatency(start, "delete", metrics.StatusLabel(err))
	m.RecordAPICallDuration(ctx, "delete", m.StatusTagKey(err), start)

	if err != nil {
		return status.ResourceWrap(err, "failed to delete object", obj)
	}
	return nil
}

// Apply gets the current object state, modifies it with the provided updateFn,
// and posts the update to the server.
// Return the current object state and a NoUpdateNeeded error from the updateFn
// to skip posting the update if no changes are required.
func (c *Client) Apply(ctx context.Context, obj client.Object, updateFn update, opts ...client.UpdateOption) (client.Object, status.Error) {
	return c.apply(ctx, obj, updateFn, func(ctx context.Context, obj client.Object) error {
		return c.Client.Update(ctx, obj, opts...)
	})
}

// ApplyStatus gets the current object status, modifies it with the provided
// updateFn, and posts the update to the server.
// Return the current object state and a NoUpdateNeeded error from the updateFn
// to skip posting the update if no changes are required.
func (c *Client) ApplyStatus(ctx context.Context, obj client.Object, updateFn update, opts ...client.SubResourceUpdateOption) (client.Object, status.Error) {
	return c.apply(ctx, obj, updateFn, func(ctx context.Context, obj client.Object) error {
		return c.Client.Status().Update(ctx, obj, opts...)
	})
}

// apply updates the given obj in the Kubernetes cluster using clientUpdateFn and records prometheus
// metrics. In the event of a conflicting apply, it will retry.
// This operation always involves retrieving the resource from API Server before actually updating it.
func (c *Client) apply(ctx context.Context, obj client.Object, updateFn update,
	clientUpdateFn clientUpdateFn) (client.Object, status.Error) {
	// We only want to modify the argument after successfully making an update to API Server.
	workingObj := obj.DeepCopyObject().(client.Object)
	description := getResourceInfo(workingObj)
	namespacedName := getNamespacedName(workingObj)

	var lastErr error

	for tryNum := 0; tryNum < c.MaxTries; tryNum++ {
		err := c.Client.Get(ctx, namespacedName, workingObj)
		if err != nil {
			switch {
			case apierrors.IsNotFound(err):
				return nil, ConflictUpdateObjectDoesNotExist(err, obj)
			case meta.IsNoMatchError(err):
				return nil, ConflictUpdateResourceDoesNotExist(err, obj)
			default:
				return nil, status.ResourceWrap(err, "failed to get object to update", obj)
			}
		}

		oldV := resourceVersion(workingObj)
		newObj, err := updateFn(workingObj.DeepCopyObject().(client.Object))
		if err != nil {
			if isNoUpdateNeeded(err) {
				klog.V(2).Infof("Update function for %s returned no update needed", description)
				return newObj, nil
			}
			return nil, status.ResourceWrap(err, "failed to update object", obj)
		}

		// cmp.Diff may take a while on the resource, only compute if V(1)
		if klog.V(1).Enabled() {
			klog.Infof("Updating object %q attempt=%d diff old..new:\n%v",
				description, tryNum+1, cmp.Diff(workingObj, newObj))
		}

		start := time.Now()
		err = clientUpdateFn(ctx, newObj)

		c.recordLatency(start, "update", metrics.StatusLabel(err))
		m.RecordAPICallDuration(ctx, "update", m.StatusTagKey(err), start)

		if err != nil {
			switch {
			case apierrors.IsConflict(err):
				// Backoff and retry on resource conflict.
				// The loop re-gets and modifies the current state, so callers
				// don't need to explicitly update their cached version.
				klog.V(2).Infof("Conflict during update for %q: %v", description, err)
				time.Sleep(100 * time.Millisecond)
				lastErr = err
				continue
			case apierrors.IsNotFound(err):
				return nil, ConflictUpdateObjectDoesNotExist(err, obj)
			case meta.IsNoMatchError(err):
				return nil, ConflictUpdateResourceDoesNotExist(err, obj)
			default:
				return nil, status.ResourceWrap(err, "failed to update object", obj)
			}
		}

		newV := resourceVersion(newObj)
		if oldV == newV {
			klog.Warningf("ResourceVersion for %s did not change during update (noop), updateFn should have indicated no update needed", description)
		} else {
			klog.Infof("Updated %s from ResourceVersion %s to %s", description, oldV, newV)
		}
		return newObj, nil
	}
	return nil, status.ResourceWrap(lastErr, "exceeded max tries to update", obj)
}

// Update posts the update to the server and records latency metrics.
// Specify a ResourceVersion to avoid overwriting asynchronous changes.
func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) status.Error {
	description := getResourceInfo(obj)
	klog.V(1).Infof("Will update %s to %s", description, spew.Sdump(obj))
	oldV := resourceVersion(obj)
	start := time.Now()
	err := c.Client.Update(ctx, obj, opts...)

	c.recordLatency(start, "update", metrics.StatusLabel(err))
	m.RecordAPICallDuration(ctx, "update", m.StatusTagKey(err), start)

	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			return ConflictUpdateObjectDoesNotExist(err, obj)
		case meta.IsNoMatchError(err):
			return ConflictUpdateResourceDoesNotExist(err, obj)
		default:
			return status.ResourceWrap(err, "failed to update object", obj)
		}
	}
	newV := resourceVersion(obj)
	if oldV == newV {
		klog.Warningf("ResourceVersion for %s did not change during update (noop)", description)
	} else {
		klog.Infof("Updated %s from ResourceVersion %s to %s", description, oldV, newV)
	}
	return nil
}

func (c *Client) recordLatency(start time.Time, lvs ...string) {
	if c.latencyMetric == nil {
		return
	}
	c.latencyMetric.WithLabelValues(lvs...).Observe(time.Since(start).Seconds())
}

// getResourceInfo returns a description of the object (its GroupVersionKind and NamespacedName), as well as its Kind.
func getResourceInfo(obj client.Object) string {
	gvk := obj.GetObjectKind().GroupVersionKind()
	namespacedName := getNamespacedName(obj)
	return fmt.Sprintf("%q, %q", gvk, namespacedName)
}

func getNamespacedName(obj client.Object) types.NamespacedName {
	return types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}

func resourceVersion(obj client.Object) string {
	return obj.GetResourceVersion()
}

// isFinalizing returns true if the object is finalizing.
func isFinalizing(o client.Object) bool {
	return o.GetDeletionTimestamp() != nil
}
