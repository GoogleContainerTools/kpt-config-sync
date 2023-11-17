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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2/klogr"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type fakeMapping struct{}

func (m fakeMapping) Get(gknn v1alpha1.ObjMetadata) []types.NamespacedName {
	if gknn.Kind == "MyKind" {
		return []types.NamespacedName{
			{
				Name:      "my-name",
				Namespace: "my-namespace",
			},
			{
				Name:      "name2",
				Namespace: "namespace2",
			},
		}
	}
	return []types.NamespacedName{
		{
			Name:      "name1",
			Namespace: "namespace1",
		},
		{
			Name:      "name2",
			Namespace: "namespace2",
		},
	}
}

func (m fakeMapping) GetResources(_ schema.GroupKind) []v1alpha1.ObjMetadata {
	return nil
}

func (m fakeMapping) SetStatus(_ v1alpha1.ObjMetadata, _ *resourcemap.CachedStatus) {
}

func TestEventHandler(t *testing.T) {
	ch := make(chan event.GenericEvent)
	h := EnqueueEventToChannel{
		Mapping: fakeMapping{},
		Channel: ch,
		Log:     klogr.New(),
	}
	u := &unstructured.Unstructured{}

	// Push an event to channel
	go func() { h.OnAdd(u, false) }()

	// Consume an event from the channel
	e := <-ch
	assert.Equal(t, e.Object.GetName(), "name1")
	assert.Equal(t, e.Object.GetNamespace(), "namespace1")

	// Consume another event from the channel
	e = <-ch
	assert.Equal(t, e.Object.GetName(), "name2")
	assert.Equal(t, e.Object.GetNamespace(), "namespace2")
}

func TestEventHandlerMultipleHandlers(t *testing.T) {
	ch := make(chan event.GenericEvent)
	h1 := EnqueueEventToChannel{
		Mapping: fakeMapping{},
		Channel: ch,
		Log:     klogr.New(),
	}

	h2 := EnqueueEventToChannel{
		Mapping: fakeMapping{},
		Channel: ch,
		Log:     klogr.New(),
		GVK:     schema.GroupVersionKind{Kind: "MyKind"},
	}

	u1 := &unstructured.Unstructured{}
	u2 := &unstructured.Unstructured{}
	u2.SetGroupVersionKind(schema.GroupVersionKind{Kind: "MyKind"})
	// Push an event to channel
	go func() { h1.OnAdd(u1, false) }()
	go func() { h2.OnDelete(u2) }()
	time.Sleep(time.Second)

	// Consume four events from the channel
	// These events should be from h1 and h2.
	// There should be
	//   1 event for "name1"
	//   1 event for "my-name"
	//   2 events for "name2"
	names := map[string]int{
		"name1":   0,
		"name2":   0,
		"my-name": 0,
	}
	for i := 1; i <= 4; i++ {
		e := <-ch
		name := e.Object.GetName()
		names[name]++
	}

	assert.Equal(t, names["name1"], 1)
	assert.Equal(t, names["name2"], 2)
	assert.Equal(t, names["my-name"], 1)
}
