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

package testwatch

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchObject returns a new watcher for a typed resource.
// The watcher will automatically be stopped when the Context is done.
//
// This is designed for simplified use in tests and is not likely to be suitable
// for non-test usage.
func WatchObject(ctx context.Context, c client.WithWatch, exampleObjList client.ObjectList) (watch.Interface, error) {
	watcher, err := c.Watch(ctx, exampleObjList)
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		watcher.Stop()
	}()
	return watcher, nil
}

// WatchUnstructured returns a new watcher for a dynamic (Unstructured) resource.
// The watcher will automatically be stopped when the Context is done.
//
// This is designed for simplified use in tests and is not likely to be suitable
// for non-test usage.
func WatchUnstructured(ctx context.Context, c dynamic.Interface, gvr schema.GroupVersionResource) (watch.Interface, error) {
	watcher, err := c.Resource(gvr).Watch(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		watcher.Stop()
	}()
	return watcher, nil
}

// WatchObjectUntil consumes a watcher, filters events to just those for the
// specified object (by key), and tests each event using the specified condition
// function. If the condition function returns an error, the watcher will cache
// the error and continue. If the condition returns nil, the watcher will be
// stopped and WatchObjectUntil will return nil. If the watcher is stopped,
// before the condition is met, WatchObjectUntil will return the last cached
// error from the condition function. WatchObjectUntil also logs object diffs
// for each event, to facilitate debugging.
//
// This is designed for simplified use in tests and is not likely to be suitable
// for non-test usage.
func WatchObjectUntil(ctx context.Context, scheme *runtime.Scheme, watcher watch.Interface, key client.ObjectKey, condition func(watch.Event) error) error {
	// Wait until added or modified
	var conditionErr error
	doneCh := ctx.Done()
	resultCh := watcher.ResultChan()
	defer watcher.Stop()
	var lastKnown client.Object
	for {
		select {
		case <-doneCh:
			return fmt.Errorf("context done before condition was met: %w: %w", ctx.Err(), conditionErr)
		case event, open := <-resultCh:
			if !open {
				if conditionErr != nil {
					return fmt.Errorf("watch stopped before condition was met: %w", conditionErr)
				}
				return errors.New("watch stopped before any events were received")
			}
			if event.Type == watch.Error {
				statusErr := apierrors.FromObject(event.Object)
				return fmt.Errorf("watch event error: %w", statusErr)
			}
			obj := event.Object.(client.Object)
			if key != client.ObjectKeyFromObject(obj) {
				// not the right object
				continue
			}
			klog.V(5).Infof("Watch %s Event for %s: Diff (- Removed, + Added):\n%s",
				event.Type,
				kinds.ObjectSummary(obj),
				log.AsYAMLDiffWithScheme(lastKnown, obj, scheme))
			lastKnown = obj
			conditionErr = condition(event)
			if conditionErr == nil {
				// success - condition met
				return nil
			}
			if errors.Is(conditionErr, &TerminalError{}) {
				// failure - exit early
				return conditionErr
			}
			// wait for next event - condition not met
		}
	}
}

// TerminalError will stop WatchObjectUntil if returned by the condition function.
type TerminalError struct {
	Cause error
}

// NewTerminalError constructs a new TerminalError
func NewTerminalError(cause error) *TerminalError {
	return &TerminalError{
		Cause: cause,
	}
}

// Error returns the error message
func (te *TerminalError) Error() string {
	return te.Cause.Error()
}

// Unwrap returns the cause of this TerminalError
func (te *TerminalError) Unwrap() error {
	return te.Cause
}
