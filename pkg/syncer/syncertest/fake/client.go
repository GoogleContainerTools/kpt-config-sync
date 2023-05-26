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
	"testing"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Client is a fake implementation of client.Client.
//
// Known Unimplemented features (update as discovered/added):
// - DeletePropagationBackground (performs foreground instead)
// - DeletePropagationOrphan (returns error if used)
// - ListOptions.Limit & Continue (ignored - all objects are returned)
// - Status().Patch
type Client struct {
	test    *testing.T
	scheme  *runtime.Scheme
	codecs  serializer.CodecFactory
	mapper  meta.RESTMapper
	storage *MemoryStorage
}

// Prove Client satisfies the client.Client interface
var _ client.Client = &Client{}

// NewClient instantiates a new fake.Client pre-populated with the specified
// objects.
//
// Calls t.Fatal if unable to properly instantiate Client.
func NewClient(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) *Client {
	t.Helper()

	// Build mapper using known GVKs from the scheme
	gvks := prioritizedGVKsAllGroups(scheme)
	mapper := testutil.NewFakeRESTMapper(gvks...)

	watchSupervisor := NewWatchSupervisor(scheme)
	storage := NewInMemoryStorage(scheme, watchSupervisor)
	result := Client{
		test:    t,
		scheme:  scheme,
		codecs:  serializer.NewCodecFactory(scheme),
		mapper:  mapper,
		storage: storage,
	}

	StartWatchSupervisor(t, watchSupervisor)

	for _, o := range objs {
		err := result.Create(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
	}

	return &result
}

// Get implements client.Client.
func (c *Client) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	options := &client.GetOptions{}
	options.ApplyOptions(opts)
	gvk, err := kinds.Lookup(obj, c.scheme)
	if err != nil {
		return err
	}
	return c.storage.Get(ctx, gvk, key, obj, options)
}

// List implements client.Client.
//
// Does not paginate results.
func (c *Client) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	return c.storage.List(ctx, list, options)
}

// Create implements client.Client.
func (c *Client) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	options := &client.CreateOptions{}
	options.ApplyOptions(opts)
	return c.storage.Create(ctx, obj, options)
}

// Delete implements client.Client.
func (c *Client) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	options := &client.DeleteOptions{}
	options.ApplyOptions(opts)
	return c.storage.Delete(ctx, obj, options)
}

// Update implements client.Client.
func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	options := &client.UpdateOptions{}
	options.ApplyOptions(opts)
	return c.storage.Update(ctx, obj, options)
}

// Patch implements client.Client.
func (c *Client) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	options := &client.PatchOptions{}
	options.ApplyOptions(opts)
	return c.storage.Patch(ctx, obj, patch, options)
}

// DeleteAllOf implements client.Client.
func (c *Client) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	options := &client.DeleteAllOfOptions{}
	options.ApplyOptions(opts)
	listObj, ok := obj.(client.ObjectList)
	if !ok {
		return errors.Errorf("failed to convert %T to client.ObjectList", obj)
	}
	return c.storage.DeleteAllOf(ctx, listObj, options)
}

// StatusWriter is a fake implementation of client.StatusWriter.
type statusWriter struct {
	Client  *Client
	storage *SubresourceStorage
}

var _ client.StatusWriter = &statusWriter{}

// Status implements client.Client.
func (c *Client) Status() client.StatusWriter {
	return &statusWriter{
		Client:  c,
		storage: c.storage.Subresource("status"),
	}
}

// Update implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	options := &client.UpdateOptions{}
	options.ApplyOptions(opts)
	return s.storage.Update(ctx, obj, options)
}

// Patch implements client.StatusWriter. It only updates the status field.
func (s *statusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	options := &client.PatchOptions{}
	options.ApplyOptions(opts)
	return s.storage.Patch(ctx, obj, patch, options)
}

// Check reports an error to `t` if the passed objects in wants do not match the
// expected set of objects in the fake.Client, and only the passed updates to
// Status fields were recorded.
func (c *Client) Check(t *testing.T, wants ...client.Object) {
	t.Helper()
	c.storage.Check(t, wants...)
}

// Watch implements client.WithWatch.
func (c *Client) Watch(ctx context.Context, exampleList client.ObjectList, opts ...client.ListOption) (watch.Interface, error) {
	options := &client.ListOptions{}
	options.ApplyOptions(opts)
	watcher, err := c.storage.Watch(ctx, exampleList, options)
	if err != nil {
		return nil, err
	}
	c.test.Cleanup(watcher.Stop)
	return watcher, nil
}

// Applier returns a fake.Applier wrapping this fake.Client. Callers using the
// resulting Applier will read from/write to the original fake.Client.
func (c *Client) Applier() reconcile.Applier {
	return &Applier{Client: c}
}

// Codecs returns the CodecFactory.
func (c *Client) Codecs() serializer.CodecFactory {
	return c.codecs
}

// Scheme implements client.Client.
func (c *Client) Scheme() *runtime.Scheme {
	return c.scheme
}

// RESTMapper returns the RESTMapper.
func (c *Client) RESTMapper() meta.RESTMapper {
	return c.mapper
}

// Storage returns the backing Storage layer
func (c *Client) Storage() *MemoryStorage {
	return c.storage
}
