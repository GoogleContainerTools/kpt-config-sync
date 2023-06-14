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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TypedListerWatcher wraps a client.WithWatch and implements the
// cache.ListerWatcher interface with optional default ListOptions.
type TypedListerWatcher struct {
	// Context to pass to client list and watch methods
	Context context.Context

	// Client to list and watch with
	Client client.WithWatch

	// ExampleList is an example of the list type of resource to list & watch
	ExampleList client.ObjectList

	// DefaultListOptions are merged into the specified ListOptions on each
	// List and Watch call.
	// See MergeListOptions for details and limitations.
	DefaultListOptions *client.ListOptions
}

var _ cache.ListerWatcher = &TypedListerWatcher{}

// NewTypedListerWatcher constructs a new TypedListerWatcher for the specified
// list type with default options.
func NewTypedListerWatcher(ctx context.Context, c client.WithWatch, exampleList client.ObjectList, opts *client.ListOptions) cache.ListerWatcher {
	return &TypedListerWatcher{
		Context:            ctx,
		Client:             c,
		ExampleList:        exampleList,
		DefaultListOptions: opts,
	}
}

// List satisfies the cache.Lister interface.
// The ListOptions specified here are merged with the ClientListerWatcher
func (fw *TypedListerWatcher) List(options metav1.ListOptions) (runtime.Object, error) {
	objList, err := kinds.ObjectAsClientObjectList(fw.ExampleList.DeepCopyObject())
	if err != nil {
		return nil, err
	}
	cOpts, err := ConvertListOptions(&options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert metav1.ListOptions to client.ListOptions")
	}
	cOpts, err = MergeListOptions(cOpts, fw.DefaultListOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to merge list options")
	}
	optsList := UnrollListOptions(cOpts)
	klog.V(5).Infof("Listing %T %s: %v", objList, objList.GetObjectKind().GroupVersionKind().Kind, optsList)
	err = fw.Client.List(fw.Context, objList, optsList...)
	return objList, err
}

// Watch satisfies the cache.Watcher interface
func (fw *TypedListerWatcher) Watch(options metav1.ListOptions) (watch.Interface, error) {
	objList, err := kinds.ObjectAsClientObjectList(fw.ExampleList.DeepCopyObject())
	if err != nil {
		return nil, err
	}
	cOpts, err := ConvertListOptions(&options)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert metav1.ListOptions to client.ListOptions")
	}
	cOpts, err = MergeListOptions(cOpts, fw.DefaultListOptions)
	if err != nil {
		return nil, errors.Wrap(err, "failed to merge list options")
	}
	optsList := UnrollListOptions(cOpts)
	klog.V(5).Infof("Watching %T %s: %v", objList, objList.GetObjectKind().GroupVersionKind().Kind, optsList)
	return fw.Client.Watch(fw.Context, objList, optsList...)
}
