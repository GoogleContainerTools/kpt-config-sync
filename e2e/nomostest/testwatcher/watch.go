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

package testwatcher

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/multierr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	watchutil "kpt.dev/configsync/pkg/util/watch"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WatchOption is an optional parameter for Watch
type WatchOption func(watch *watchSpec)

type watchSpec struct {
	timeout      time.Duration
	ctx          context.Context
	unstructured bool
}

// WatchTimeout provides the timeout option to Watch.
func WatchTimeout(timeout time.Duration) WatchOption {
	return func(watch *watchSpec) {
		watch.timeout = timeout
	}
}

// WatchContext provides the context option to Watch.
func WatchContext(ctx context.Context) WatchOption {
	return func(watch *watchSpec) {
		watch.ctx = ctx
	}
}

// WatchUnstructured tells the watcher to used unstructured objects, instead of
// the default typed objects.
func WatchUnstructured() WatchOption {
	return func(watch *watchSpec) {
		watch.unstructured = true
	}
}

// Watcher is used for performing various Watch operations on k8s objects in the cluster
// Uses the provided kubeClient/restConfig to set up the Watches
type Watcher struct {
	ctx                context.Context
	defaultWaitTimeout *time.Duration
	logger             *testlogger.TestLogger
	restConfig         *rest.Config
	kubeClient         *testkubeclient.KubeClient
	scheme             *runtime.Scheme
}

// NewWatcher constructs a new Watcher
func NewWatcher(
	ctx context.Context,
	logger *testlogger.TestLogger,
	kubeClient *testkubeclient.KubeClient,
	restConfig *rest.Config,
	scheme *runtime.Scheme,
	defaultWaitTimeout *time.Duration,
) *Watcher {
	w := &Watcher{
		ctx:                ctx,
		logger:             logger,
		kubeClient:         kubeClient,
		restConfig:         restConfig,
		scheme:             scheme,
		defaultWaitTimeout: defaultWaitTimeout,
	}
	return w
}

// WatchObject watches the specified object util all predicates return nil,
// or the timeout is reached. Object does not need to exist yet, as long as the
// resource type exists.
// All Predicates need to handle nil objects (nil means Not Found).
// If no Predicates are specified, WatchObject watches until the object exists.
func (w *Watcher) WatchObject(gvk schema.GroupVersionKind, name, namespace string, predicates []testpredicates.Predicate, opts ...WatchOption) error {
	errPrefix := fmt.Sprintf("WatchObject(%s %s/%s)", gvk.Kind, namespace, name)

	startTime := time.Now()
	defer func() {
		took := time.Since(startTime)
		w.logger.Infof("%s watched for %v", errPrefix, took)
	}()

	spec := watchSpec{
		timeout: *w.defaultWaitTimeout,
		ctx:     w.ctx,
	}
	for _, opt := range opts {
		opt(&spec)
	}

	// Default to waiting until the object exists
	if len(predicates) == 0 {
		predicates = append(predicates, testpredicates.ObjectFoundPredicate)
	}

	// If a timeout is specified (WatchTimeout(0) means no timeout),
	// then cancel the context after the timeout to cancel the List/Watch.
	// Otherwise, just cancel on early return.
	var ctx context.Context
	var cancel context.CancelFunc
	if spec.timeout > 0 {
		ctx, cancel = context.WithTimeout(spec.ctx, spec.timeout)
	} else {
		ctx, cancel = context.WithCancel(spec.ctx)
	}
	defer cancel()

	lw, err := w.newListWatchForObject(ctx, gvk, name, namespace, spec.timeout, spec.unstructured)
	if err != nil {
		return fmt.Errorf("%s: %w", errPrefix, err)
	}

	// Cache the last known object state for diffing with the new state.
	var prevObj client.Object

	// Cache the last set of predicate errors to return if UntilWithSync errors
	var evalErrs []error

	// Build an example object with the expected type and key fields
	var exampleObj client.Object
	if spec.unstructured {
		exampleObj = &unstructured.Unstructured{}
		exampleObj.GetObjectKind().SetGroupVersionKind(gvk)
	} else {
		rObj, err := kinds.NewObjectForGVK(gvk, w.scheme)
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
		exampleObj, err = kinds.ObjectAsClientObject(rObj)
		if err != nil {
			return fmt.Errorf("%s: %w", errPrefix, err)
		}
	}
	exampleObj.SetName(name)
	exampleObj.SetNamespace(namespace)

	// UntilWithSync uses cache.MetaNamespaceKeyFunc for generating cache keys
	// from event objects. Use the same method, just in case it changes in the
	// future (e.g. from string to struct).
	objKey, err := cache.MetaNamespaceKeyFunc(exampleObj)
	if err != nil {
		return fmt.Errorf("%s: %w", errPrefix, err)
	}

	// precondition runs after the informer is synced.
	// This means the initial list has completed and the cache is primed.
	precondition := func(store cache.Store) (bool, error) {
		iObj, found, err := store.GetByKey(objKey)
		if err != nil {
			return false, err
		}

		var cObj client.Object
		if !found {
			w.logger.Infof("%s GET Not Found", errPrefix)
			// Predicates expect the object to be nil when Not Found.
			// cObj = nil
		} else {
			w.logger.Infof("%s GET Found", errPrefix)
			var ok bool
			cObj, ok = iObj.(client.Object)
			if !ok {
				return false, fmt.Errorf("invalid cache object: unsupported resource type (%T): failed to cast to client.Object",
					iObj)
			}
			// Log the initial/current state as a diff to make it easy to compare with update diffs.
			// Use diff.Diff because it doesn't truncate like cmp.Diff does.
			// Use log.AsYAMLWithScheme to get the full scheme-consistent YAML with GVK.
			w.logger.Debugf("%s GET Diff (+ Current):\n%s",
				errPrefix, log.AsYAMLDiffWithScheme(prevObj, cObj, w.scheme))
		}

		evalErrs = testpredicates.EvaluatePredicates(cObj, predicates)
		if len(evalErrs) == 0 {
			// Success! All predicates returned without error.
			return true, nil
		}
		// Update object cache for subsequent diffs
		prevObj = cObj
		// Continue watching.
		return false, nil
	}

	// watch.Until watches the object and executes this ConditionFunc on each
	// event until one of the following conditions is true:
	// - The condition function returns true (all predicates succeeded)
	// - The condition function returns an error
	// - The context is cancelled (timeout)
	condition := func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Error:
			// Error events are not handled by watch.Util. So catch them here.
			// For error events, the object is usually *metav1.Status,
			// indicating a server-side error. So stop watching.
			return false, fmt.Errorf(
				"received error event: %w", apierrors.FromObject(event.Object))
		case watch.Added, watch.Modified, watch.Deleted:
			eType := event.Type
			if eType == watch.Added && prevObj != nil {
				// Added to watch cache for the first time, but not to k8s
				eType = watch.Modified
			}

			// For Added/Modified/Deleted events, the object should be of the
			// type being watched.
			cObj, err := kinds.ObjectAsClientObject(event.Object)
			if err != nil {
				return false, err
			}
			if cObj.GetName() != name {
				// Ignore events for other objects, if any.
				// Should never happen, due to name selector.
				return false, nil
			}

			w.logger.Infof("%s %s (generation: %d, resourceVersion: %s)",
				errPrefix, eType, cObj.GetGeneration(), cObj.GetResourceVersion())

			if eType == watch.Deleted {
				// For delete events, the object is the last known state, but
				// Predicates expect the object to be nil when Not Found.
				cObj = nil
			}

			w.logger.Debugf("%s %s Diff (- Removed, + Added):\n%s",
				errPrefix, eType,
				log.AsYAMLDiffWithScheme(prevObj, cObj, w.scheme))

			evalErrs = testpredicates.EvaluatePredicates(cObj, predicates)
			if len(evalErrs) == 0 {
				// Success! All predicates returned without error.
				return true, nil
			}
			// Update object cache for subsequent diffs
			prevObj = cObj
			// Continue watching.
			return false, nil
		case watch.Bookmark:
			// Bookmark indicates the ResourceVersion was updated, but the
			// object(s) being watched did not (some other object did).
			// Continue watching.
			return false, nil
		default:
			// Stop watching.
			return false, fmt.Errorf("received unexpected event: %#v", event)
		}
	}
	_, err = watchtools.UntilWithSync(ctx, lw, exampleObj, precondition, condition)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			// Until returns ErrWaitTimeout for any ctx.Done
			// https://github.com/kubernetes/apimachinery/blob/v0.26.3/pkg/util/wait/wait.go#L594
			// https://github.com/kubernetes/client-go/blob/v0.26.3/tools/watch/until.go#L89
			// So use the context error instead, if done.
			// Use errors.Is to detect DeadlineExceeded or Cancelled.
			select {
			case <-ctx.Done():
				err = ctx.Err()
			default:
			}
		}
		if len(evalErrs) > 0 {
			// errors.Is only works with a single parent error, and we want to
			// detect DeadlineExceeded vs Cancelled.
			// So use the context error as the cause and convert the
			// predicate errors into a string.
			// If we ever need to test for a specific predicate failure, this
			// may need to change. But for now we don't.
			err = fmt.Errorf("predicates not satisfied: %v: %w", multierr.Combine(evalErrs...), err)
		} else {
			// If we got no errors, it probably means the context was cancelled
			// before the ConditionFunc was called.
			err = fmt.Errorf("no watch events were received: %w", err)
		}
		return fmt.Errorf("%s: %w", errPrefix, err)
	}
	// Success! Condition returned true.
	return nil
}

// newListWatchForObject returns a ListWatch that does server-side filtering
// down to a single resource object.
// Optionally specify the minimum timeout for the REST client.
func (w *Watcher) newListWatchForObject(ctx context.Context, itemGVK schema.GroupVersionKind, name, namespace string, timeout time.Duration, unstructured bool) (cache.ListerWatcher, error) {
	restConfig := w.restConfig
	// Make sure the client-side watch timeout isn't too short.
	// If not, duplicate the config and update the timeout.
	// You can use the Context for timeout, so the rest.Config timeout just needs to be longer.
	if timeout <= 0 {
		// no timeout
		restConfig.Timeout = 0
	} else if restConfig.Timeout < timeout*2 {
		restConfig = rest.CopyConfig(restConfig)
		restConfig.Timeout = timeout * 2
	}
	// Lookup the resource name from the GVK using discovery (usually cached)
	mapper := w.kubeClient.Client.RESTMapper()
	mapping, err := mapper.RESTMapping(itemGVK.GroupKind(), itemGVK.Version)
	if err != nil {
		return nil, err
	}
	// Validate namespace is empty for cluster-scoped resources
	if mapping.Scope.Name() == meta.RESTScopeNameRoot && namespace != "" {
		return nil, fmt.Errorf("cannot watch cluster-scoped resource %q in namespace %q", mapping.Resource.Resource, namespace)
	}
	// Filter by name and namespace
	opts := &client.ListOptions{
		Namespace:     namespace,
		FieldSelector: fields.OneTermEqualSelector("metadata.name", name),
	}

	var lw cache.ListerWatcher
	if unstructured {
		dynamicClient, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			return nil, err
		}
		lw = &watchutil.DynamicListerWatcher{
			Context:            ctx,
			DynamicClient:      dynamicClient,
			RESTMapper:         mapper,
			ItemGVK:            itemGVK,
			DefaultListOptions: opts,
		}
	} else {
		// Use the custom client scheme to encode requests and decode responses
		scheme := w.kubeClient.Client.Scheme()
		typedClient, err := client.NewWithWatch(restConfig, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			return nil, err
		}
		exampleList, err := kinds.NewTypedListForItemGVK(itemGVK, scheme)
		if err != nil {
			return nil, err
		}
		lw = &watchutil.TypedListerWatcher{
			Context:            ctx,
			Client:             typedClient,
			ExampleList:        exampleList,
			DefaultListOptions: opts,
		}
	}
	return lw, nil
}

// WatchForCurrentStatus watches the object until it reconciles (Current).
func (w *Watcher) WatchForCurrentStatus(gvk schema.GroupVersionKind, name, namespace string, opts ...WatchOption) error {
	return w.WatchObject(gvk, name, namespace,
		[]testpredicates.Predicate{testpredicates.StatusEquals(w.scheme, kstatus.CurrentStatus)},
		opts...)
}

// WatchForNotFound waits for the passed object to be fully deleted or not found.
// Returns an error if the object is not deleted before the timeout.
func (w *Watcher) WatchForNotFound(gvk schema.GroupVersionKind, name, namespace string, opts ...WatchOption) error {
	return w.WatchObject(gvk, name, namespace,
		[]testpredicates.Predicate{testpredicates.ObjectNotFoundPredicate(w.scheme)},
		opts...)
}
