// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package watcher

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

type DynamicInformerFactory struct {
	Client       dynamic.Interface
	ResyncPeriod time.Duration
	Indexers     cache.Indexers
}

func NewDynamicInformerFactory(client dynamic.Interface, resyncPeriod time.Duration) *DynamicInformerFactory {
	return &DynamicInformerFactory{
		Client:       client,
		ResyncPeriod: resyncPeriod,
		Indexers: cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	}
}

func (f *DynamicInformerFactory) NewInformer(ctx context.Context, mapping *meta.RESTMapping, namespace string) cache.SharedIndexInformer {
	// Unstructured example output need `"apiVersion"` and `"kind"` set.
	example := &unstructured.Unstructured{}
	example.SetGroupVersionKind(mapping.GroupVersionKind)

	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return f.Client.Resource(mapping.Resource).
					Namespace(namespace).
					List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return f.Client.Resource(mapping.Resource).
					Namespace(namespace).
					Watch(ctx, options)
			},
		},
		example,
		f.ResyncPeriod,
		f.Indexers,
	)
}
