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

package watch

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kpt.dev/resourcegroup/controllers/resourcemap"
)

type startWatchFunc func(metav1.ListOptions) (watch.Interface, error)

// watcherConfig contains the options needed
// to create a watcher.
type watcherConfig struct {
	gvk        schema.GroupVersionKind
	mapper     meta.RESTMapper
	config     *rest.Config
	resources  *resourcemap.ResourceMap
	startWatch startWatchFunc
	channel    chan event.GenericEvent
}

// createWatcherFunc is the type of functions to create watchers
type createWatcherFunc func(ctx context.Context, cfg watcherConfig) (Runnable, error)

// createWatcher creates a watcher for a given GVK
func createWatcher(ctx context.Context, cfg watcherConfig) (Runnable, error) {
	if cfg.startWatch == nil {
		mapping, err := cfg.mapper.RESTMapping(cfg.gvk.GroupKind(), cfg.gvk.Version)
		if err != nil {
			return nil, fmt.Errorf("watcher failed to get REST mapping for %s: %v", cfg.gvk.String(), err)
		}

		dynamicClient, err := dynamic.NewForConfig(cfg.config)
		if err != nil {
			return nil, fmt.Errorf("watcher failed to get dynamic client for %s: %v", cfg.gvk.String(), err)
		}

		cfg.startWatch = func(options metav1.ListOptions) (watch.Interface, error) {
			return dynamicClient.Resource(mapping.Resource).Watch(ctx, options)
		}
	}

	return NewFiltered(ctx, cfg), nil
}
