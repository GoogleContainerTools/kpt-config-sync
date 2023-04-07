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

	"github.com/pkg/errors"
	"go.uber.org/multierr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testlogger"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/util/log"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// WatchOption is an optional parameter for Watch
type WatchOption func(watch *watchSpec)

type watchSpec struct {
	timeout time.Duration
	ctx     context.Context
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

	lw, err := w.newListWatchForObject(ctx, gvk, name, namespace, spec.timeout)
	if err != nil {
		return errors.Wrap(err, errPrefix)
	}

	// Don't need to specify the FieldSelector again.
	// NewListWatchFromClient does it for us.
	listOpts := metav1.ListOptions{}

	// LIST with a name selector is functionally equivalent to a GET, except it
	// can be performed with the same ListerWatcher, so we can re-use the same
	// client.
	// Using LIST also allows us to get the latest ResourceVersion for the whole
	// resource, not just the RV when the object was last updated. This makes
	// the subsequent WATCH a little more efficient, because it can skip the
	// intermediate ResourceVersions (when only other objects were changed).
	rObjList, err := lw.List(listOpts)
	if err != nil {
		return errors.Wrap(err, errPrefix)
	}
	// This cast should almost always work, except on some rarely used internal types.
	cObjList, ok := rObjList.(client.ObjectList)
	if !ok {
		return errors.Wrapf(
			testpredicates.WrongTypeErr(rObjList, client.ObjectList(nil)),
			"%s unexpected list type", errPrefix)
	}
	// Since items are typed, we have to use some reflection to extract them.
	// But since we filtered by name and namespace, there should only be one item.
	cObj, err := getObjectFromList(cObjList, name, namespace)
	if err != nil {
		return errors.Wrap(err, errPrefix)
	}

	// Cache the last known object state for diffing with the new state.
	var prevObj client.Object

	if cObj == nil {
		w.logger.Infof("%s GET Not Found", errPrefix)
		// Predicates expect the object to be nil when Not Found.
	} else {
		w.logger.Infof("%s GET Found", errPrefix)
		// Log the initial/current state as a diff to make it easy to compare with update diffs.
		// Use diff.Diff because it doesn't truncate like cmp.Diff does.
		// Use log.AsYAMLWithScheme to get the full scheme-consistent YAML with GVK.
		w.logger.Debugf("%s GET Diff (+ Current):\n%s",
			errPrefix, log.AsYAMLDiffWithScheme(prevObj, cObj, w.scheme))
	}

	// Cache the predicate errors from the last evaluation and return them
	// if the watch closes before the predicates all pass.
	evalErrs := testpredicates.EvaluatePredicates(cObj, predicates)
	if len(evalErrs) == 0 {
		// Success! All predicates passed!
		return nil
	}
	// Update object cache for subsequent diffs
	prevObj = cObj

	// Specify ResourceVersion to ensure the Watch starts where the List stopped.
	// watch.Until will update the ListOptions for us.
	initialResourceVersion := cObjList.GetResourceVersion()

	// Enable bookmarks to ensure retries start from the latest ResourceVersion,
	// even if there haven't been any updates to the object being watched.
	listOpts.AllowWatchBookmarks = true

	// watch.Until watches the object and executes this ConditionFunc on each
	// event until one of the following conditions is true:
	// - The conditon function returns true (all predicates succeeded)
	// - The condition function returns an error
	// - The context is cancelled (timeout)
	condition := func(event watch.Event) (bool, error) {
		switch event.Type {
		case watch.Error:
			// Error events are not handled by watch.Util. So catch them here.
			// For error events, the object is usually *metav1.Status,
			// indicating a server-side error. So stop watching.
			return false, errors.Wrap(
				apierrors.FromObject(event.Object),
				"received error event")
		case watch.Added, watch.Modified, watch.Deleted:
			eType := event.Type
			if eType == watch.Added && prevObj != nil {
				// Added to watch cache for the first time, but not to k8s
				eType = watch.Modified
			}
			w.logger.Infof("%s %s", errPrefix, eType)

			// For Added/Modified/Deleted events, the object should be of the
			// type being watched.
			// This cast should almost always work, except on some rarely used
			// internal types.
			cObj, ok := event.Object.(client.Object)
			if !ok {
				return false, errors.Errorf(
					"expected event object of type client.Object but got %T",
					event.Object)
			}
			if cObj.GetName() != name {
				// Ignore events for other objects, if any.
				// Should never happen, due to name selector.
				return false, nil
			}

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
			return false, errors.Errorf("received unexpected event: %#v", event)
		}
	}
	_, err = watchtools.Until(ctx, initialResourceVersion, lw, condition)
	if err != nil {
		if err == wait.ErrWaitTimeout {
			if len(evalErrs) > 0 {
				return &ErrWatchTimeout{
					cause:  multierr.Combine(evalErrs...),
					prefix: errPrefix,
				}
			}
			return errors.Errorf("%s timed out before any watch events were received", errPrefix)
		}
		return errors.Wrap(err, errPrefix)
	}
	// Success! Condition returned true.
	return nil
}

// ErrWatchTimeout is returned by Watch functions to indicate timeout.
type ErrWatchTimeout struct {
	cause  error
	prefix string
}

// Error returns the error message.
func (ewt *ErrWatchTimeout) Error() string {
	return fmt.Sprintf("%s timed out waiting for predicates: %v", ewt.prefix, ewt.cause)
}

// Unwrap provides compatibility for Go 1.13 error chains.
func (ewt *ErrWatchTimeout) Unwrap() error { return ewt.cause }

// newListWatchForObject returns a ListWatch that does server-side filtering
// down to a single resource object.
// Optionally specify the minimum timeout for the REST client.
func (w *Watcher) newListWatchForObject(ctx context.Context, gvk schema.GroupVersionKind, name, namespace string, timeout time.Duration) (*cache.ListWatch, error) {
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
	// Use the custom client scheme to encode requests and decode responses
	codecs := serializer.NewCodecFactory(w.kubeClient.Client.Scheme())
	restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, codecs)
	if err != nil {
		return nil, err
	}
	// Lookup the resource name from the GVK using discovery (usually cached)
	mapping, err := w.kubeClient.Client.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	resource := mapping.Resource.Resource
	// Validate namespace is empty for cluster-scoped resources
	if mapping.Scope.Name() == meta.RESTScopeNameRoot && namespace != "" {
		return nil, errors.Errorf("cannot watch cluster-scoped resource %q in namespace %q", resource, namespace)
	}
	// Use a selector to filter the events down to just the object we care about.
	nameSelector := fields.OneTermEqualSelector("metadata.name", name)
	// Create a ListerWatcher for this resource and namespace
	lw := NewListWatchFromClient(ctx, restClient, resource, namespace, nameSelector)
	return lw, nil
}

// getObjectFromList loops through the items in an ObjectList and returns the
// one with the specified name and namespace.
// This compexity is required because the items are typed without generics.
// So we have to use reflection to read the type so we can access the fields.
func getObjectFromList(objList client.ObjectList, name, namespace string) (client.Object, error) {
	items, err := meta.ExtractList(objList)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to understand list result %#v",
			objList)
	}
	// Iterate through the list to find the desired object, if it exists
	for _, rObj := range items {
		cObj, ok := rObj.(client.Object)
		if !ok {
			return nil, errors.Wrap(
				testpredicates.WrongTypeErr(rObj, client.Object(nil)),
				"unexpected list item type")
		}
		if cObj.GetName() == name && cObj.GetNamespace() == namespace {
			return cObj, nil
		}
		// Ignore other objects, if any.
	}
	// Object Not Found
	return nil, nil
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

// NewListWatchFromClient replaces cache.NewListWatchFromClient, but adds a
// context, to allow the List and Watch to be cancelled or time out.
// https://github.com/kubernetes/client-go/blob/v0.26.3/tools/cache/listwatch.go#L70
func NewListWatchFromClient(ctx context.Context, c cache.Getter, resource string, namespace string, fieldSelector fields.Selector) *cache.ListWatch {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector.String()
	}
	return NewFilteredListWatchFromClient(ctx, c, resource, namespace, optionsModifier)
}

// NewFilteredListWatchFromClient replaces cache.NewFilteredListWatchFromClient,
// but adds a context, to allow the List and Watch to be cancelled or time out.
// https://github.com/kubernetes/client-go/blob/v0.26.3/tools/cache/listwatch.go#L80
func NewFilteredListWatchFromClient(ctx context.Context, c cache.Getter, resource string, namespace string, optionsModifier func(options *metav1.ListOptions)) *cache.ListWatch {
	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		optionsModifier(&options)
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, metav1.ParameterCodec).
			Do(ctx).
			Get()
	}
	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		options.Watch = true
		optionsModifier(&options)
		return c.Get().
			Namespace(namespace).
			Resource(resource).
			VersionedParams(&options, metav1.ParameterCodec).
			Watch(ctx)
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}
