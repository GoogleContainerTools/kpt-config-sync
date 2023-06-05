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
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
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
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		},
	}
}

// NewInformer constructs a new SharedIndexInformer for the specified resource.
func (f *TypedInformerFactory) NewInformer(ctx context.Context, mapping *meta.RESTMapping, namespace string) (cache.SharedIndexInformer, error) {
	gvk := mapping.GroupVersionKind
	scope := mapping.Scope

	exampleObj, err := kinds.NewClientObjectForGVK(gvk, f.Client.Scheme())
	if err != nil {
		return nil, err
	}

	// Validate List type exists in scheme
	if _, err = kinds.NewTypedListForItemGVK(gvk, f.Client.Scheme()); err != nil {
		return nil, err
	}

	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				opts, err := MetaListOptionsToClientListOptions(&options)
				if err != nil {
					return nil, err
				}
				isNamespaceScoped := namespace != "" && scope.Name() != meta.RESTScopeNameRoot
				if isNamespaceScoped {
					opts.Namespace = namespace
				}
				optsList := UnrollClientListOptions(opts)
				listObj, err := kinds.NewTypedListForItemGVK(gvk, f.Client.Scheme())
				if err != nil {
					return nil, err
				}
				err = f.Client.List(ctx, listObj, optsList...)
				return listObj, err
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				opts, err := MetaListOptionsToClientListOptions(&options)
				if err != nil {
					return nil, err
				}
				isNamespaceScoped := namespace != "" && scope.Name() != meta.RESTScopeNameRoot
				if isNamespaceScoped {
					opts.Namespace = namespace
				}
				optsList := UnrollClientListOptions(opts)
				listObj, err := kinds.NewTypedListForItemGVK(gvk, f.Client.Scheme())
				if err != nil {
					return nil, err
				}
				return f.Client.Watch(ctx, listObj, optsList...)
			},
		},
		exampleObj,
		f.ResyncPeriod,
		f.Indexers,
	), nil
}

// MetaListOptionsToClientListOptions converts metav1.ListOptions to
// client.ListOptions.
func MetaListOptionsToClientListOptions(in *metav1.ListOptions) (*client.ListOptions, error) {
	out := &client.ListOptions{}
	if in == nil {
		return out, nil
	}
	if in.LabelSelector != "" {
		selector, err := labels.Parse(in.LabelSelector)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse label selector")
		}
		out.LabelSelector = selector
	}
	if in.FieldSelector != "" {
		selector, err := fields.ParseSelector(in.FieldSelector)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse field selector")
		}
		out.FieldSelector = selector
	}
	if !in.Watch {
		out.Limit = in.Limit
		out.Continue = in.Continue
	}
	out.Raw = in
	return out, nil
}

// UnrollClientListOptions converts client.ListOptions to []client.ListOption
func UnrollClientListOptions(in *client.ListOptions) []client.ListOption {
	var out []client.ListOption
	if in == nil {
		return out
	}
	if in.LabelSelector != nil {
		out = append(out, client.MatchingLabelsSelector{Selector: in.LabelSelector})
	}
	if in.FieldSelector != nil {
		out = append(out, client.MatchingFieldsSelector{Selector: in.FieldSelector})
	}
	if in.Limit > 0 {
		out = append(out, client.Limit(in.Limit))
	}
	if in.Continue != "" {
		out = append(out, client.Continue(in.Continue))
	}
	if in.Raw != nil {
		out = append(out, RawListOptions{Raw: in.Raw})
	}
	return out
}

// RawListOptions filters the list operation using raw metav1.ListOptions.
type RawListOptions struct {
	Raw *metav1.ListOptions
}

// ApplyToList applies this configuration to the given an List options.
func (rlo RawListOptions) ApplyToList(opts *client.ListOptions) {
	opts.Raw = rlo.Raw
}
