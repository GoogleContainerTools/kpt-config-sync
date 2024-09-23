// Copyright 2024 Google LLC
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

package events

import (
	"context"
	"reflect"

	"k8s.io/klog/v2"
)

// Funnel events from multiple publishers to a single subscriber.
type Funnel struct {
	Publishers []Publisher
	Subscriber Subscriber
}

// Start all the event publishers and sends events one at a time to the
// subscriber. Blocks until the publishers are started. Returns a channel that
// will be closed when the Funnel is done handling events. Stops when the
// context is done.
func (f *Funnel) Start(ctx context.Context) <-chan struct{} {
	// Stop all sources when done, even if the parent context isn't done.
	// This could happen if all the source channels are closed.
	ctx, cancel := context.WithCancel(ctx)

	klog.Info("Starting event loop")

	// Create a list of cases to select from
	cases := make([]reflect.SelectCase, len(f.Publishers)+1)
	for i, source := range f.Publishers {
		klog.Infof("Starting event source: %s", source.Type())
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: source.Start(ctx),
		}
	}

	// Add a select case that closes the event channel when the context is done
	cases[len(cases)-1] = reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	}

	doneCh := make(chan struct{})
	go func() {
		defer cancel()
		defer close(doneCh)
		f.funnel(ctx, cases)
	}()
	return doneCh
}

func (f *Funnel) funnel(ctx context.Context, cases []reflect.SelectCase) {
	// Select from cases until context is done or all source channels have been closed.
	remaining := len(cases)
	for remaining > 0 {
		// Use reflect.Select to allow a dynamic set of cases.
		// None of the channels return anything important, so ignore the value.
		caseIndex, _, ok := reflect.Select(cases)
		if !ok {
			ctxErr := ctx.Err()
			if ctxErr != nil {
				klog.Infof("Stopping event loop: %v", ctxErr)
				return
			}
			// Closed channels are always selected, so nil the channel to
			// disable this case. We can't just remove the case from the list,
			// because we need the index to match the handlers list.
			cases[caseIndex].Chan = reflect.ValueOf(nil)
			remaining--
			continue
		}

		klog.V(1).Infof("Handling event: %s", f.Publishers[caseIndex].Type())
		result := f.Publishers[caseIndex].Publish(f.Subscriber)

		// Give all publishers a chance to handle the result
		for _, source := range f.Publishers {
			source.HandleResult(result)
		}
	}
}
