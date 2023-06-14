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

package watch

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TypedInformerFactory is a factory that constructs new SharedIndexInformers.
type TypedInformerFactory struct {
	Client       client.WithWatch
	ResyncPeriod time.Duration
	Indexers     cache.Indexers
}

// NewTypedInformerFactory constructs a new TypedInformerFactory
func NewTypedInformerFactory(client client.WithWatch, resyncPeriod time.Duration) *TypedInformerFactory {
	return &TypedInformerFactory{
		Client:       client,
		ResyncPeriod: resyncPeriod,
		Indexers: cache.Indexers{
			// by default, index by namespace
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	}
}

// NewInformer constructs a new SharedIndexInformer for the specified resource.
func (f *TypedInformerFactory) NewInformer(ctx context.Context, mapping *meta.RESTMapping, namespace string) (cache.SharedIndexInformer, error) {
	itemGVK := mapping.GroupVersionKind
	scheme := f.Client.Scheme()

	exampleObj, err := kinds.NewClientObjectForGVK(itemGVK, scheme)
	if err != nil {
		return nil, err
	}

	exampleList, err := kinds.NewTypedListForItemGVK(itemGVK, scheme)
	if err != nil {
		return nil, err
	}

	opts := &client.ListOptions{
		Namespace: namespace,
	}

	return cache.NewSharedIndexInformer(
		NewTypedListerWatcher(ctx, f.Client, exampleList, opts),
		exampleObj,
		f.ResyncPeriod,
		f.Indexers,
	), nil
}
