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

// EventProducer triggers events with a chanel and defines how to handle them.
type EventProducer struct {
	// EventType of the producer.
	EventType string
	// EventChan takes a readable channel value.
	// Reflection is used to allow channels with any payload type.
	// This avoids needing to wrap channels to change type, which would
	// introduce buffering, which would prevent the event producer from waiting
	// until the event was received.
	// Usage: reflect.ValueOf(ctx.Done())
	EventChan reflect.Value
	// HandleEvent handles these events without input. Returns a HandleResult.
	// HandleEvent should be called when the event channel sends an event, but
	// not when the event channel is closed.
	HandleEvent EventHandler
	// HandleResult handles the result of all types of events. This allows this
	// producer to handle signals from other producers after they handle their
	// event.
	HandleResult ResultHandler
	// Stop the event channel.
	Stop context.CancelFunc
}

type EventHandler func(HandleEventFunc) HandleResult

type ResultHandler func(HandleResult)

// Execute watches a list of asynchronous event channels and synchronously
// executes the appropriate event handlers for each generated event.
// Takes a generic HandleEventFunc to allow injecting event-specific behavioral
// logic, separate from the scheduling logic
func Execute(ctx context.Context, producers []EventProducer, handleFn HandleEventFunc) {
	// Add Context handler
	klog.Info("Starting ContextEvent generator")
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	producers = append(producers, EventProducer{
		EventType: "ContextEvent",
		EventChan: reflect.ValueOf(ctx.Done()),
		// Handle will never be called.
		// Context channel doesn't send events, just closes.
		// Check ctx.Err() to get the reason.
		Stop: cancel,
	})

	// Make sure all streams are stopped when Handle returns.
	defer func() {
		for _, stream := range producers {
			stream.Stop()
		}
	}()

	// Convert the list of EventStreams into parallel lists with the same index.
	cases := make([]reflect.SelectCase, len(producers))
	caseNames := make([]string, len(producers))
	caseHandlers := make([]EventHandler, len(producers))
	for i, producer := range producers {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: producer.EventChan,
		}
		caseNames[i] = producer.EventType
		caseHandlers[i] = producer.HandleEvent
	}

	// Select from cases until done
	remaining := len(cases)
	for remaining > 0 {
		// Use reflect.Select to allow a dynamic set of cases.
		// None of the channels return anything important, so ignore the value.
		caseIndex, _, ok := reflect.Select(cases)
		if !ok {
			ctxErr := ctx.Err()
			if ctxErr != nil {
				klog.Infof("TimedEventProducer stopping: %v", ctxErr)
				return
			}
			// Closed channels are always selected, so nil the channel to
			// disable this case. We can't just remove the case from the list,
			// because we need the index to match the handlers list.
			cases[caseIndex].Chan = reflect.ValueOf(nil)
			remaining--
			continue
		}
		klog.Infof("Handling case[%d]: %s", caseIndex, caseNames[caseIndex])
		result := caseHandlers[caseIndex](handleFn)
		// Give all producers a chance to handle the result
		for _, producer := range producers {
			if producer.HandleResult != nil {
				producer.HandleResult(result)
			}
		}
	}
}
