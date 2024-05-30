// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package apply

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/kstatus/watcher"
)

type commonBuilder struct {
	// factory is only used to retrieve things that have not been provided explicitly.
	factory                      util.Factory
	invClient                    inventory.Client
	client                       dynamic.Interface
	discoClient                  discovery.CachedDiscoveryInterface
	mapper                       meta.RESTMapper
	restConfig                   *rest.Config
	unstructuredClientForMapping func(*meta.RESTMapping) (resource.RESTClient, error)
	statusWatcher                watcher.StatusWatcher
	statusWatcherFilters         *watcher.Filters
}

func (cb *commonBuilder) finalize() (*commonBuilder, error) {
	cx := *cb // make a copy before mutating any fields. Shallow copy is good enough.
	var err error
	if cx.invClient == nil {
		return nil, errors.New("inventory client must be provided")
	}
	if cx.client == nil {
		if cx.factory == nil {
			return nil, fmt.Errorf("a factory must be provided or all other options: %v", err)
		}
		cx.client, err = cx.factory.DynamicClient()
		if err != nil {
			return nil, fmt.Errorf("error getting dynamic client: %v", err)
		}
	}
	if cx.discoClient == nil {
		if cx.factory == nil {
			return nil, fmt.Errorf("a factory must be provided or all other options: %v", err)
		}
		cx.discoClient, err = cx.factory.ToDiscoveryClient()
		if err != nil {
			return nil, fmt.Errorf("error getting discovery client: %v", err)
		}
	}
	if cx.mapper == nil {
		if cx.factory == nil {
			return nil, fmt.Errorf("a factory must be provided or all other options: %v", err)
		}
		cx.mapper, err = cx.factory.ToRESTMapper()
		if err != nil {
			return nil, fmt.Errorf("error getting rest mapper: %v", err)
		}
	}
	if cx.restConfig == nil {
		if cx.factory == nil {
			return nil, fmt.Errorf("a factory must be provided or all other options: %v", err)
		}
		cx.restConfig, err = cx.factory.ToRESTConfig()
		if err != nil {
			return nil, fmt.Errorf("error getting rest config: %v", err)
		}
	}
	if cx.unstructuredClientForMapping == nil {
		if cx.factory == nil {
			return nil, fmt.Errorf("a factory must be provided or all other options: %v", err)
		}
		cx.unstructuredClientForMapping = cx.factory.UnstructuredClientForMapping
	}
	if cx.statusWatcher == nil {
		statusWatcher := watcher.NewDefaultStatusWatcher(cx.client, cx.mapper)
		if cx.statusWatcherFilters != nil {
			statusWatcher.Filters = cx.statusWatcherFilters
		}
		cx.statusWatcher = statusWatcher
	} else if cx.statusWatcherFilters != nil {
		// If you want to use a custom status watcher with a label selector,
		// configure it before injecting the status watcher.
		return nil, errors.New("status watcher and status watcher filters must not both be provided")
	}
	return &cx, nil
}
