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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DynamicListerWatcher wraps a dynamic.Interface and implements the
// cache.ListerWatcher interface with optional default ListOptions.
type DynamicListerWatcher struct {
	// Context to pass to client list and watch methods
	Context context.Context

	// DynamicClient to list and watch with
	DynamicClient dynamic.Interface

	// RESTMapper to map from kind to resource
	RESTMapper meta.RESTMapper

	// ItemGVK is the kind of resource to list & watch
	ItemGVK schema.GroupVersionKind

	// DefaultListOptions are merged into the specified ListOptions on each
	// List and Watch call.
	// See MergeListOptions for details and limitations.
	DefaultListOptions *client.ListOptions
}

var _ cache.ListerWatcher = &DynamicListerWatcher{}

// List satisfies the cache.Lister interface.
// The ListOptions specified here are merged with the ClientListerWatcher
func (dlw *DynamicListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	mapping, err := dlw.RESTMapper.RESTMapping(dlw.ItemGVK.GroupKind(), dlw.ItemGVK.Version)
	if err != nil {
		return nil, errors.Wrap(err, "listing")
	}
	cOpts, err := ConvertListOptions(&options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert metav1.ListOptions to client.ListOptions")
	}
	cOpts, err = MergeListOptions(cOpts, dlw.DefaultListOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to merge list options")
	}
	mOpts := cOpts.AsListOptions()
	klog.V(5).Infof("Listing %s: %+v", dlw.ItemGVK.GroupKind(), cOpts)
	if cOpts.Namespace != "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return dlw.DynamicClient.Resource(mapping.Resource).Namespace(cOpts.Namespace).List(dlw.Context, *mOpts)
	}
	return dlw.DynamicClient.Resource(mapping.Resource).List(dlw.Context, *mOpts)
}

// Watch satisfies the cache.Watcher interface
func (dlw *DynamicListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	mapping, err := dlw.RESTMapper.RESTMapping(dlw.ItemGVK.GroupKind(), dlw.ItemGVK.Version)
	if err != nil {
		return nil, errors.Wrap(err, "watching")
	}
	cOpts, err := ConvertListOptions(&options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert metav1.ListOptions to client.ListOptions")
	}
	cOpts, err = MergeListOptions(cOpts, dlw.DefaultListOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to merge list options")
	}
	mOpts := cOpts.AsListOptions()
	klog.V(5).Infof("Watching %s: %+v", dlw.ItemGVK.GroupKind(), cOpts)
	if cOpts.Namespace != "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return dlw.DynamicClient.Resource(mapping.Resource).Namespace(cOpts.Namespace).Watch(dlw.Context, *mOpts)
	}
	return dlw.DynamicClient.Resource(mapping.Resource).Watch(dlw.Context, *mOpts)
}
