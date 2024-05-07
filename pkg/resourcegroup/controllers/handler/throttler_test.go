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

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
)

func TestThrottler(t *testing.T) {
	u := &v1alpha1.ResourceGroup{}
	u.SetName("group")
	u.SetNamespace("ns")

	// Push an event to channel
	genericE := event.GenericEvent{
		Object: u,
	}

	t.Log("Add one event")
	throttler := NewThrottler(time.Second)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	throttler.Generic(context.Background(), genericE, queue)

	_, found := throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.True(t, found)
	time.Sleep(2 * time.Second)
	_, found = throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.False(t, found)

	// The queue should contain only one event
	assert.Equal(t, queue.Len(), 1)
}

func TestThrottlerMultipleEvents(t *testing.T) {
	u := &v1alpha1.ResourceGroup{}
	u.SetName("group")
	u.SetNamespace("ns")

	// Push an event to channel
	genericE := event.GenericEvent{
		Object: u,
	}

	// Set the duration to 5 seconds
	throttler := NewThrottler(5 * time.Second)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	ctx := context.Background()

	// Call the event handler three times for the same event
	throttler.Generic(ctx, genericE, queue)
	throttler.Generic(ctx, genericE, queue)
	throttler.Generic(ctx, genericE, queue)

	// After 3 seconds, still within the duration, the event can
	// be found in the mapping
	time.Sleep(3 * time.Second)
	_, found := throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.True(t, found)

	// After 3 + 3 seconds, the duration ends, the event
	// is removed from the mapping
	time.Sleep(3 * time.Second)
	_, found = throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.False(t, found)

	// The queue should contain only one event
	assert.Equal(t, queue.Len(), 1)
}

func TestThrottlerMultipleObjects(t *testing.T) {
	u := &v1alpha1.ResourceGroup{}
	u.SetName("group")
	u.SetNamespace("ns")

	// Push an event to channel
	genericE := event.GenericEvent{
		Object: u,
	}

	u2 := &v1alpha1.ResourceGroup{}
	u2.SetName("group2")
	u2.SetNamespace("ns")

	genericE2 := event.GenericEvent{
		Object: u2,
	}

	// Set the duration to 5 seconds
	throttler := NewThrottler(5 * time.Second)
	queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	ctx := context.Background()
	// Call the event handler to push two events
	throttler.Generic(ctx, genericE, queue)
	throttler.Generic(ctx, genericE2, queue)
	throttler.Generic(ctx, genericE, queue)
	throttler.Generic(ctx, genericE2, queue)

	// After 3 seconds, still within the duration, the events can
	// be found in the mapping
	time.Sleep(3 * time.Second)
	_, found := throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.True(t, found)
	_, found = throttler.mapping[types.NamespacedName{
		Name:      "group2",
		Namespace: "ns",
	}]
	assert.True(t, found)

	// After 3 + 3 seconds, the duration ends, the events
	// is removed from the mapping
	time.Sleep(3 * time.Second)
	_, found = throttler.mapping[types.NamespacedName{
		Name:      "group",
		Namespace: "ns",
	}]
	assert.False(t, found)
	_, found = throttler.mapping[types.NamespacedName{
		Name:      "group2",
		Namespace: "ns",
	}]
	assert.False(t, found)

	// The queue should contain two events
	assert.Equal(t, queue.Len(), 2)
}
