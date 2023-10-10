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

package watch

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kpt.dev/resourcegroup/controllers/resourcemap"
)

// Manager records which GVK's are watched.
// When a new GVK needs to be watches, it adds the watch
// to the associated controller.
type Manager struct {
	// cfg is the rest config used to talk to apiserver.
	cfg *rest.Config

	// mapper is the RESTMapper to use for mapping GroupVersionKinds to Resources.
	mapper meta.RESTMapper

	// resources is the declared resources that are parsed from Git.
	resources *resourcemap.ResourceMap

	// createWatcherFunc is the function to create a watcher.
	createWatcherFunc createWatcherFunc

	// channel is the channel for ResourceGroup generic events.
	channel chan event.GenericEvent

	// The following fields are guarded by the mutex.
	mux sync.Mutex
	// watcherMap maps GVKs to their associated watchers
	watcherMap map[schema.GroupVersionKind]Runnable
	// needsUpdate indicates if the Manager's watches need to be updated.
	needsUpdate bool
}

// Options contains options for creating a watch manager.
type Options struct {
	// Mapper is the RESTMapper to use for mapping GroupVersionKinds to Resources.
	Mapper meta.RESTMapper

	watcherFunc createWatcherFunc
}

// DefaultOptions return the default options:
// - create discovery RESTmapper from the passed rest.Config
// - use createWatcher to create watchers
func DefaultOptions(cfg *rest.Config) (*Options, error) {
	mapper, err := apiutil.NewDynamicRESTMapper(cfg)
	if err != nil {
		return nil, err
	}

	return &Options{
		Mapper:      mapper,
		watcherFunc: createWatcher,
	}, nil
}

// NewManager starts a new watch manager
func NewManager(cfg *rest.Config, decls *resourcemap.ResourceMap, channel chan event.GenericEvent, options *Options) (*Manager, error) {
	if options == nil {
		var err error
		options, err = DefaultOptions(cfg)
		if err != nil {
			return nil, err
		}
	}

	return &Manager{
		cfg:               cfg,
		resources:         decls,
		watcherMap:        make(map[schema.GroupVersionKind]Runnable),
		createWatcherFunc: options.watcherFunc,
		mapper:            options.Mapper,
		channel:           channel,
		mux:               sync.Mutex{},
	}, nil
}

// NeedsUpdate returns true if the Manager's watches need to be updated. This function is threadsafe.
func (m *Manager) NeedsUpdate() bool {
	m.mux.Lock()
	defer m.mux.Unlock()
	return m.needsUpdate
}

// UpdateWatches accepts a map of GVKs that should be watched and takes the
// following actions:
// - stop watchers for any GroupVersionKind that is not present in the given map.
// - start watchers for any GroupVersionKind that is present in the given map and not present in the current watch map.
//
// This function is threadsafe.
func (m *Manager) UpdateWatches(ctx context.Context, gvkMap map[schema.GroupVersionKind]struct{}) error {
	m.mux.Lock()
	defer m.mux.Unlock()

	m.needsUpdate = false

	var startedWatches, stoppedWatches uint64
	// Stop obsolete watchers.
	for gvk := range m.watcherMap {
		if _, keepWatching := gvkMap[gvk]; !keepWatching {
			// We were watching the type, but no longer have declarations for it.
			// It is safe to stop the watcher.
			m.stopWatcher(gvk)
			stoppedWatches++
		}
	}

	// Start new watchers
	var errs []error
	for gvk := range gvkMap {
		if _, isWatched := m.watcherMap[gvk]; !isWatched {
			// We don't have a watcher for this type, so add a watcher for it.
			if err := m.startWatcher(ctx, gvk); err != nil {
				errs = append(errs, err)
			}
			startedWatches++
		}
	}

	if startedWatches > 0 || stoppedWatches > 0 {
		klog.Infof("The watch manager made new progress: started %d new watches, and stopped %d watches", startedWatches, stoppedWatches)
	} else {
		klog.V(4).Infof("The watch manager made no new progress")
	}
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

// watchedGVKs returns a list of all GroupVersionKinds currently being watched.
func (m *Manager) watchedGVKs() []schema.GroupVersionKind {
	var gvks []schema.GroupVersionKind
	for gvk := range m.watcherMap {
		gvks = append(gvks, gvk)
	}
	return gvks
}

// startWatcher starts a watcher for a GVK. This function is NOT threadsafe;
// caller must have a lock on m.mux.
func (m *Manager) startWatcher(ctx context.Context, gvk schema.GroupVersionKind) error {
	_, found := m.watcherMap[gvk]
	if found {
		// The watcher is already started.
		return nil
	}
	cfg := watcherConfig{
		gvk:       gvk,
		mapper:    m.mapper,
		config:    m.cfg,
		channel:   m.channel,
		resources: m.resources,
	}
	w, err := m.createWatcherFunc(ctx, cfg)
	if err != nil {
		return err
	}

	m.watcherMap[gvk] = w
	go m.runWatcher(ctx, w, gvk)
	return nil
}

// runWatcher blocks until the given watcher finishes running. This function is
// threadsafe.
func (m *Manager) runWatcher(ctx context.Context, r Runnable, gvk schema.GroupVersionKind) {
	if err := r.Run(ctx); err != nil {
		klog.Warningf("Error running watcher for %s: %v", gvk.String(), err)
		m.mux.Lock()
		delete(m.watcherMap, gvk)
		m.needsUpdate = true
		m.mux.Unlock()
	}
}

// stopWatcher stops a watcher for a GVK. This function is NOT threadsafe;
// caller must have a lock on m.mux.
func (m *Manager) stopWatcher(gvk schema.GroupVersionKind) {
	w, found := m.watcherMap[gvk]
	if !found {
		// The watcher is already stopped.
		return
	}

	// Stop the watcher.
	w.Stop()
	delete(m.watcherMap, gvk)
}

// Len returns the number of types that are currently watched.
func (m *Manager) Len() int {
	return len(m.watcherMap)
}

func (m *Manager) IsWatched(gvk schema.GroupVersionKind) bool {
	_, found := m.watcherMap[gvk]
	return found
}
