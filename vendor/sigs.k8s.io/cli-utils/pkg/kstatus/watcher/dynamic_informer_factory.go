// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package watcher

import (
	"context"
	"regexp"
	"strings"
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

// resourceNotFoundMessage is the condition message for metav1.StatusReasonNotFound.
// This is necessary because the Informer doesn't properly wrap list errors.
// https://github.com/kubernetes/client-go/blob/v0.24.0/tools/cache/reflector.go#L325
// https://github.com/kubernetes/apimachinery/blob/v0.24.0/pkg/api/errors/errors.go#L448
// TODO: Remove once fix is released (1.25+): https://github.com/kubernetes/kubernetes/pull/110076
const resourceNotFoundMessage = "the server could not find the requested resource"

// containsNotFoundMessage checks if the error string contains the message for
// StatusReasonNotFound.
func containsNotFoundMessage(err error) bool {
	return strings.Contains(err.Error(), resourceNotFoundMessage)
}

// resourceForbiddenMessagePattern is a regex pattern to match the condition
// message for metav1.StatusForbidden.
// This is necessary because the Informer doesn't properly wrap list errors.
// https://github.com/kubernetes/client-go/blob/v0.24.0/tools/cache/reflector.go#L325
// https://github.com/kubernetes/apimachinery/blob/v0.24.0/pkg/api/errors/errors.go#L458
// https://github.com/kubernetes/apimachinery/blob/v0.24.0/pkg/api/errors/errors.go#L208
// https://github.com/kubernetes/apiserver/blob/master/pkg/endpoints/handlers/responsewriters/errors.go#L51
// TODO: Remove once fix is released (1.25+): https://github.com/kubernetes/kubernetes/pull/110076
const resourceForbiddenMessagePattern = `(.+) is forbidden: User "(.*)" cannot (.+) resource "(.*)" in API group "(.*)"`

// resourceForbiddenMessageRegexp is the pre-compiled Regexp of
// resourceForbiddenMessagePattern.
var resourceForbiddenMessageRegexp = regexp.MustCompile(resourceForbiddenMessagePattern)

// containsForbiddenMessage checks if the error string contains the message for
// StatusForbidden.
func containsForbiddenMessage(err error) bool {
	return resourceForbiddenMessageRegexp.Match([]byte(err.Error()))
}
