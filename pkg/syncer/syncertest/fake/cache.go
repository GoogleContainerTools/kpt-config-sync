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

package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8scache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cache is a fake implementation of cache.Cache.
type Cache struct {
	*Client

	typedInformerFactory *TypedInformerFactory

	// Namespace restricts the cache's ListWatch to the desired namespace
	// Default watches all namespaces
	namespace string

	resyncPeriod time.Duration

	// mux guards access to the map
	mux sync.RWMutex

	// start is true if the informers have been started
	started bool

	// startWait is a channel that is closed after the
	// informer has been started.
	startWait chan struct{}

	// informerCtx context used to stop the informers
	informerCtx context.Context

	// informerCtxCancel is the CancelFunc to stop the informerCtx
	informerCtxCancel context.CancelFunc

	// informersByGVK is the cache of informers keyed by groupVersionKind
	informersByGVK map[schema.GroupVersionKind]*MapEntry
}

var _ cache.Cache = &Cache{}

// CacheOptions are the optional arguments for creating a new Cache object.
type CacheOptions struct {
	// Namespace restricts the cache's ListWatch to the desired namespace
	// Default watches all namespaces
	Namespace string

	// ResyncPeriod is how often the objects are retrieved to re-synchronize,
	// in case any events were missed.
	// If zero or less, re-sync is disabled.
	ResyncPeriod time.Duration
}

// NewCache constructs a new Cache
func NewCache(fakeClient *Client, opts CacheOptions) *Cache {
	return &Cache{
		Client:               fakeClient,
		namespace:            opts.Namespace,
		startWait:            make(chan struct{}),
		informersByGVK:       make(map[schema.GroupVersionKind]*MapEntry),
		resyncPeriod:         opts.ResyncPeriod,
		typedInformerFactory: NewTypedInformerFactory(fakeClient, opts.ResyncPeriod),
	}
}

// Start the cache, including any previously requested informers.
// New informers requested after start will be started immediately.
func (c *Cache) Start(ctx context.Context) error {
	func() {
		c.mux.Lock()
		defer c.mux.Unlock()

		// Init the informer context.
		// This is for the case where the cache is started before any informers are added.
		if c.informerCtx == nil {
			c.informerCtx, c.informerCtxCancel = context.WithCancel(context.Background())
		}

		// Start each informer
		for gvk, informer := range c.informersByGVK {
			klog.V(5).Infof("starting informer for %s", kinds.GVKToString(gvk))
			go informer.Informer.Run(c.informerCtx.Done())
		}

		// Set started to true so we immediately start any informers added later.
		c.started = true
		close(c.startWait)
	}()
	<-ctx.Done()

	c.mux.RLock()
	defer c.mux.RLock()
	// Stop the informers
	c.informerCtxCancel()

	return nil
}

// WaitForCacheSync returns true when the cached informers are all synced.
// Returns false if the context is done first.
func (c *Cache) WaitForCacheSync(ctx context.Context) bool {
	select {
	case <-c.startWait:
		return true
	case <-ctx.Done():
		return false
	}
}

// HasSyncedFuncs returns all the HasSynced functions for the informers in this map.
func (c *Cache) HasSyncedFuncs() []k8scache.InformerSynced {
	c.mux.RLock()
	defer c.mux.RUnlock()
	syncedFuncs := make([]k8scache.InformerSynced, 0, len(c.informersByGVK))
	for _, informer := range c.informersByGVK {
		syncedFuncs = append(syncedFuncs, informer.Informer.HasSynced)
	}
	return syncedFuncs
}

// GetInformer constructs or retrieves from cache an informer to watch the
// specified resource.
func (c *Cache) GetInformer(ctx context.Context, obj client.Object) (cache.Informer, error) {
	gvk, err := kinds.Lookup(obj, c.Scheme())
	if err != nil {
		return nil, err
	}
	return c.GetInformerForKind(ctx, gvk)
}

func (c *Cache) getInformerMapEntry(ctx context.Context, gvk schema.GroupVersionKind) (*MapEntry, bool, error) {
	// Return the informer if it is found
	entry, started, found := func() (*MapEntry, bool, bool) {
		c.mux.RLock()
		defer c.mux.RUnlock()
		entry, found := c.informersByGVK[gvk]
		return entry, c.started, found
	}()

	if !found {
		var err error
		if entry, started, err = c.addInformerToMap(gvk); err != nil {
			return nil, false, err
		}
	}

	if started && !entry.Informer.HasSynced() {
		// Wait for it to sync before returning the Informer so that folks don't read from a stale cache.
		if !k8scache.WaitForCacheSync(ctx.Done(), entry.Informer.HasSynced) {
			return nil, false, apierrors.NewTimeoutError(fmt.Sprintf("failed waiting for Informer to sync: %s", kinds.GVKToString(gvk)), 0)
		}
	}

	return entry, started, nil
}

// GetInformerForKind returns the informer for the GroupVersionKind.
func (c *Cache) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind) (cache.Informer, error) {
	entry, _, err := c.getInformerMapEntry(ctx, gvk)
	if err != nil {
		return nil, err
	}
	return entry.Informer, nil
}

// IndexField adds an indexer to the underlying cache, using extraction function to get
// value(s) from the given field. This index can then be used by passing a field selector
// to List. For one-to-one compatibility with "normal" field selectors, only return one value.
// The values may be anything. They will automatically be prefixed with the namespace of the
// given object, if present. The objects passed are guaranteed to be objects of the correct type.
func (c *Cache) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	informer, err := c.GetInformer(ctx, obj)
	if err != nil {
		return err
	}
	return indexByField(informer, field, extractValue)
}

// Get implements client.Reader.Get.
func (c *Cache) Get(ctx context.Context, key client.ObjectKey, out client.Object, opts ...client.GetOption) error {
	gvk, err := kinds.Lookup(out, c.Scheme())
	if err != nil {
		return err
	}

	entry, started, err := c.getInformerMapEntry(ctx, gvk)
	if err != nil {
		return err
	}

	if !started {
		return &cache.ErrCacheNotStarted{}
	}
	return entry.Reader.Get(ctx, key, out, opts...)
}

// List implements client.Reader.List.
func (c *Cache) List(ctx context.Context, out client.ObjectList, opts ...client.ListOption) error {
	itemGVK, err := c.itemGVKForListObject(out)
	if err != nil {
		return err
	}

	entry, started, err := c.getInformerMapEntry(ctx, itemGVK)
	if err != nil {
		return err
	}

	if !started {
		return &cache.ErrCacheNotStarted{}
	}

	return entry.Reader.List(ctx, out, opts...)
}

func (c *Cache) addInformerToMap(gvk schema.GroupVersionKind) (*MapEntry, bool, error) {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Check the cache to see if we already have an Informer.  If we do, return the Informer.
	// This is for the case where 2 routines tried to get the informer when it wasn't in the map
	// so neither returned early, but the first one created it.
	if entry, ok := c.informersByGVK[gvk]; ok {
		return entry, c.started, nil
	}

	mapping, err := c.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, false, err
	}

	// Init the informer context.
	// This is for the case where informers are added before the cache is started.
	if c.informerCtx == nil {
		c.informerCtx, c.informerCtxCancel = context.WithCancel(context.Background())
	}

	informer, err := c.typedInformerFactory.NewInformer(c.informerCtx, mapping, c.namespace)
	if err != nil {
		return nil, false, err
	}

	entry := &MapEntry{
		Informer: informer,
		Reader:   c.Client,
	}
	c.informersByGVK[gvk] = entry

	// Start the Informer if cache is already started
	if c.started {
		klog.V(5).Infof("starting informer for %s", kinds.GVKToString(gvk))
		go entry.Informer.Run(c.informerCtx.Done())
	}
	return entry, c.started, nil
}

// indexByField adds an indexer to the informer that indexes by the specified field.
// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.1/pkg/cache/informer_cache.go#L180
func indexByField(informer cache.Informer, field string, extractor client.IndexerFunc) error {
	indexFunc := func(objRaw interface{}) ([]string, error) {
		obj, isObj := objRaw.(client.Object)
		if !isObj {
			return nil, fmt.Errorf("object of type %T is not an Object", objRaw)
		}
		meta, err := apimeta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		ns := meta.GetNamespace()

		rawVals := extractor(obj)
		var vals []string
		if ns == "" {
			// if we're not doubling the keys for the namespaced case, just create a new slice with same length
			vals = make([]string, len(rawVals))
		} else {
			// if we need to add non-namespaced versions too, double the length
			vals = make([]string, len(rawVals)*2)
		}
		for i, rawVal := range rawVals {
			// save a namespaced variant, so that we can ask
			// "what are all the object matching a given index *in a given namespace*"
			vals[i] = keyToNamespacedKey(ns, rawVal)
			if ns != "" {
				// if we have a namespace, also inject a special index key for listing
				// regardless of the object namespace
				vals[i+len(rawVals)] = keyToNamespacedKey("", rawVal)
			}
		}

		return vals, nil
	}

	return informer.AddIndexers(k8scache.Indexers{fieldIndexName(field): indexFunc})
}

// itemGVKForListObject tries to find the GVK for a single object
// corresponding to the specified list type. Used as cache map key.
func (c *Cache) itemGVKForListObject(list client.ObjectList) (schema.GroupVersionKind, error) {
	// Lookup the List type from the scheme
	listGVK, err := kinds.Lookup(list, c.Scheme())
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	// Convert the List type to the Item type
	targetKind := strings.TrimSuffix(listGVK.Kind, kinds.ListSuffix)
	if targetKind == listGVK.Kind {
		return schema.GroupVersionKind{}, fmt.Errorf("list kind does not have required List suffix: %s", listGVK.Kind)
	}
	return listGVK.GroupVersion().WithKind(targetKind), nil
}

// MapEntry contains the cached data for an Informer.
type MapEntry struct {
	// Informer is the cached informer
	Informer k8scache.SharedIndexInformer

	// CacheReader wraps Informer and implements the CacheReader interface for a single type
	Reader client.Reader
}

// fieldIndexName constructs the name of the index over the given field,
// for use with an indexer.
// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.1/pkg/cache/internal/cache_reader.go#L204
func fieldIndexName(field string) string {
	return "field:" + field
}

// allNamespacesNamespace is used as the "namespace" when we want to list across all namespaces.
// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.1/pkg/cache/internal/cache_reader.go#L209
const allNamespacesNamespace = "__all_namespaces"

// keyToNamespacedKey prefixes the given index key with a namespace
// for use in field selector indexes.
// https://github.com/kubernetes-sigs/controller-runtime/blob/v0.13.1/pkg/cache/internal/cache_reader.go#L213
func keyToNamespacedKey(ns string, baseKey string) string {
	if ns != "" {
		return ns + "/" + baseKey
	}
	return allNamespacesNamespace + "/" + baseKey
}
