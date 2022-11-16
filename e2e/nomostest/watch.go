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
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchForObject watches the specified object util all predicates return nil,
// or the timeout is reached.
func WatchForObject(nt *NT, gvk schema.GroupVersionKind, name, namespace string, predicates []Predicate, waitOpts ...WaitOption) {
	nt.T.Helper()

	startTime := time.Now()

	wait := waitSpec{
		timeout:         nt.DefaultWaitTimeout,
		failureStrategy: WaitFailureStrategyFatal,
	}
	for _, opt := range waitOpts {
		opt(&wait)
	}

	err := watchForObjectInner(nt, gvk, name, namespace, predicates, wait)
	if err != nil {
		took := time.Since(startTime)
		nt.T.Logf("failed after %v watching for predicates to be satisfied", took)
		switch wait.failureStrategy {
		case WaitFailureStrategyFatal:
			nt.T.Fatal(err)
		case WaitFailureStrategyError:
			nt.T.Error(err)
		}
		return
	}
	took := time.Since(startTime)
	nt.T.Logf("took %v watching for predicates to be satisfied", took)
}

func watchForObjectInner(nt *NT, gvk schema.GroupVersionKind, name, namespace string, predicates []Predicate, wait waitSpec) error {
	nt.T.Helper()

	listGVK := gvk.GroupVersion().WithKind(gvk.Kind + "List")
	rObj, err := nt.WatchClient.Scheme().New(listGVK)
	if err != nil {
		return err
	}
	cObjList, ok := rObj.(client.ObjectList)
	if !ok {
		return fmt.Errorf("%w: got %T, want client.Object", ErrWrongType, rObj)
	}

	var listOpts []client.ListOption
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	ctx, cancel := context.WithTimeout(nt.Context, wait.timeout)
	defer cancel()
	result, err := nt.WatchClient.Watch(ctx, cObjList, listOpts...)
	defer result.Stop()
	if err != nil {
		return err
	}

	var prevObj client.Object
	var prevErrs []error

	for event := range result.ResultChan() {
		rObj := event.Object
		cObj, ok := rObj.(client.Object)
		if !ok {
			return errors.Errorf("expected event object of type client.Object but got %T", rObj)
		}
		if cObj.GetName() != name {
			// Ignore events for other objects, if any.
			continue
		}

		switch event.Type {
		case watch.Bookmark:
			// do nothing
		case watch.Deleted:
			nt.DebugLogf("WatchObject(%s) Diff (- Old, + New)\n%s",
				event.Type, cmp.Diff(prevObj, nil))
			prevObj = nil
		case watch.Added, watch.Modified:
			nt.DebugLogf("WatchObject(%s) Diff (- Old, + New)\n%s",
				event.Type, cmp.Diff(prevObj, cObj))
			var errs []error
			for _, predicate := range predicates {
				if err := predicate(cObj); err != nil {
					errs = append(errs, err)
				}
			}
			if len(errs) == 0 {
				// Success! passed all predicates!
				return nil
			}
			prevErrs = errs
			prevObj = cObj
		case watch.Error:
			return errors.Errorf("watch error: %+v", event)
		default:
			return errors.Errorf("unexpected watch event type %q", event.Type)
		}
	}

	if len(prevErrs) > 0 {
		// watch exited with predicate errors
		return multierr.Combine(prevErrs...)
	}
	return errors.Errorf("watch exited before any add/update events were recieved")
}
