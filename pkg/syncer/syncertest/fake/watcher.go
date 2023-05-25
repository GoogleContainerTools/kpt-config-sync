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
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type eventWithKind struct {
	watch.Event
	GroupKind schema.GroupKind
}

// StartWatchSupervisor starts a watchSupervisor and stops it in test cleanup.
func StartWatchSupervisor(t *testing.T, supervisor *WatchSupervisor) {
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		// Stop supervisor
		cancel()
		// Wait for supervisor to exit
		<-doneCh
	})

	// Start event fan-out
	go func() {
		// Run supervisor until cancelled
		supervisor.Run(ctx)
		// Signal supervisor exit
		close(doneCh)
	}()
}

// WatchSupervisor supervises watches for all resources in a scheme.
//
// Once started, the supervisor handles fan-out of events to all current
// watchers. Watches are version-agnostic. Any events for a specific GroupKind
// will be propagated to all watchers of that GroupKind.
type WatchSupervisor struct {
	scheme  *runtime.Scheme
	eventCh chan eventWithKind
	doneCh  chan struct{}

	lock     sync.RWMutex
	watchers map[schema.GroupKind]map[chan watch.Event]struct{}
}

// NewWatchSupervisor constructs a new WatchSupervisor
func NewWatchSupervisor(scheme *runtime.Scheme) *WatchSupervisor {
	return &WatchSupervisor{
		scheme:   scheme,
		eventCh:  make(chan eventWithKind),
		doneCh:   make(chan struct{}),
		watchers: make(map[schema.GroupKind]map[chan watch.Event]struct{}),
	}
}

// Run propagates events to watchers until the context is done.
func (ws *WatchSupervisor) Run(ctx context.Context) {
	doneCh := ctx.Done()
	for {
		select {
		case <-doneCh:
			// Context is cancelled or timed out.
			close(ws.eventCh)
			return
		case event, ok := <-ws.eventCh:
			if !ok {
				// Input channel is closed.
				close(ws.doneCh)
				return
			}
			// Input event received.
			ws.sendEventToWatchers(ctx, event.GroupKind, event.Event)
		}
	}
}

// Send an event to all watchers of the specified GroupKind.
func (ws *WatchSupervisor) Send(gk schema.GroupKind, event watch.Event) {
	select {
	case <-ws.doneCh: // skip sending event if no longer running
	case ws.eventCh <- eventWithKind{Event: event, GroupKind: gk}:
	}
}

func (ws *WatchSupervisor) addWatcher(gk schema.GroupKind, eventCh chan watch.Event) {
	ws.lock.Lock()
	defer ws.lock.Unlock()

	if _, ok := ws.watchers[gk]; !ok {
		ws.watchers[gk] = make(map[chan watch.Event]struct{})
	}

	ws.watchers[gk][eventCh] = struct{}{}
}

func (ws *WatchSupervisor) removeWatcher(gk schema.GroupKind, eventCh chan watch.Event) {
	ws.lock.Lock()
	defer ws.lock.Unlock()

	if _, ok := ws.watchers[gk]; !ok {
		return
	}

	delete(ws.watchers[gk], eventCh)
}

func (ws *WatchSupervisor) sendEventToWatchers(ctx context.Context, gk schema.GroupKind, event watch.Event) {
	ws.lock.RLock()
	defer ws.lock.RUnlock()

	klog.V(5).Infof("Broadcasting %s event for %s", event.Type,
		kinds.ObjectSummary(event.Object))

	doneCh := ctx.Done()
	for watcher := range ws.watchers[gk] {
		klog.V(5).Infof("Narrowcasting %s event for %s", event.Type,
			kinds.ObjectSummary(event.Object))
		watcher := watcher
		go func() {
			select {
			case <-doneCh:
				// Context is cancelled or timed out.
			case watcher <- event:
				// Event recieved or channel closed
			}
		}()
	}
}

// StartWatcher starts a watchSupervisor and stops it in test cleanup.
func StartWatcher(t *testing.T, watcher *Watcher) {
	doneCh := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		// Stop watcher
		cancel()
		// Wait for watcher to exit
		<-doneCh
	})

	// Start event handler
	go func() {
		// Run watcher until cancelled
		watcher.Run(ctx)
		// Signal watcher exit
		close(doneCh)
	}()
}

// Watcher is a fake implementation of watch.Interface.
type Watcher struct {
	supervisor  *WatchSupervisor
	inCh        chan watch.Event
	outCh       chan watch.Event
	gk          schema.GroupKind
	exampleList client.ObjectList
	options     *client.ListOptions
	scheme      *runtime.Scheme

	context context.Context
	cancel  context.CancelFunc
}

// NewWatcher constructs a new Watcher
func NewWatcher(supervisor *WatchSupervisor, gk schema.GroupKind, exampleList client.ObjectList, options *client.ListOptions) *Watcher {
	return &Watcher{
		supervisor:  supervisor,
		inCh:        make(chan watch.Event),
		outCh:       make(chan watch.Event),
		gk:          gk,
		exampleList: exampleList,
		options:     options,
		scheme:      supervisor.scheme,
	}
}

// Run adds the watcher to the supervisor and handles events until the
// context is done or the Watcher is stopped, then the watcher is removed from
// the supervisor.
func (fw *Watcher) Run(ctx context.Context) {
	fw.supervisor.addWatcher(fw.gk, fw.inCh)
	// Wrap with a new context so that both the input context and
	// fakeWatcher.Stop() can stop the event handler.
	fw.context, fw.cancel = context.WithCancel(ctx)
	defer func() {
		// Clean up context if input channel closed before context was cancelled
		fw.cancel()
		fw.supervisor.removeWatcher(fw.gk, fw.inCh)
	}()
	fw.handleEvents(fw.context)
}

func (fw *Watcher) handleEvents(ctx context.Context) {
	doneCh := ctx.Done()
	defer close(fw.outCh)
	for {
		select {
		case <-doneCh:
			// Context is cancelled or timed out.
			return
		case event, ok := <-fw.inCh:
			if !ok {
				// Input channel is closed.
				return
			}
			// Input event received.
			fw.sendEvent(ctx, event)
		}
	}
}

func (fw *Watcher) sendEvent(ctx context.Context, event watch.Event) {
	if event.Type != watch.Error {
		klog.V(5).Infof("Filtering %s event for %s", event.Type,
			kinds.ObjectSummary(event.Object))

		// Convert input object type to desired object type and version, if possible
		obj, matches, err := convertToListItemType(event.Object, fw.exampleList, fw.scheme)
		if err != nil {
			fw.sendEvent(ctx, watch.Event{
				Type:   watch.Error,
				Object: &apierrors.NewInternalError(err).ErrStatus,
			})
			return
		}
		if !matches {
			// No match
			return
		}
		event.Object = obj

		// Check if input object matches list option filters
		matches, err = matchesListFilters(event.Object, fw.options, fw.scheme)
		if err != nil {
			fw.sendEvent(ctx, watch.Event{
				Type:   watch.Error,
				Object: &apierrors.NewInternalError(err).ErrStatus,
			})
			return
		}
		if !matches {
			// No match
			return
		}
	}

	klog.V(5).Infof("Sending %s event for %s", event.Type,
		kinds.ObjectSummary(event.Object))

	doneCh := ctx.Done()
	select {
	case <-doneCh:
		// Context is cancelled or timed out.
		return
	case fw.outCh <- event:
		// Event received or channel closed
	}
}

// Stop watching the event channel.
func (fw *Watcher) Stop() {
	fw.cancel()
}

// ResultChan returns the event channel.
// The event channel will be closed when the watcher is stopped.
func (fw *Watcher) ResultChan() <-chan watch.Event {
	return fw.outCh
}
