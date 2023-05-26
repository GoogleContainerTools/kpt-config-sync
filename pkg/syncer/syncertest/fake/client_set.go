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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

// ClientSet is a wrapper that contains a client.Client & dynamic.Interface with
// the same backing MemoryStorage.
// This enables usage of both client types in the same tests.
type ClientSet struct {
	// Client is a fake implementation of client.Client.
	Client *Client
	// DynamicClient is a fake implementation of dynamic.Interface.
	DynamicClient *DynamicClient
	// Storage is the in-memory storage backing both the Client & DynamicClient.
	Storage *MemoryStorage
}

// NewClientSet builds a new fake.Client & fake.DynamicClient that share a
// MemoryStorage.
//
// Calls t.Fatal if unable to properly instantiate Client.
func NewClientSet(t *testing.T, scheme *runtime.Scheme) *ClientSet {
	t.Helper()

	// Build mapper using known GVKs from the scheme
	gvks := prioritizedGVKsAllGroups(scheme)
	mapper := testutil.NewFakeRESTMapper(gvks...)

	watchSupervisor := NewWatchSupervisor(scheme)
	storage := NewInMemoryStorage(scheme, watchSupervisor)

	set := &ClientSet{
		Client: &Client{
			test:    t,
			scheme:  scheme,
			codecs:  serializer.NewCodecFactory(scheme),
			mapper:  mapper,
			storage: storage,
		},
		DynamicClient: &DynamicClient{
			test:              t,
			FakeDynamicClient: dynamicfake.NewSimpleDynamicClient(scheme),
			scheme:            scheme,
			mapper:            mapper,
			storage:           storage,
		},
		Storage: storage,
	}
	set.DynamicClient.registerHandlers(gvks)

	StartWatchSupervisor(t, watchSupervisor)

	return set
}
