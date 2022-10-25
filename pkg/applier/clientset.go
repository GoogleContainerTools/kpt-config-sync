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

package applier

import (
	"context"

	"github.com/GoogleContainerTools/kpt/pkg/live"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KptApplier is the interface exposed by cli-utils apply.Applier.
// Using an interface, instead of the concrete struct, allows for easier testing.
type KptApplier interface {
	Run(context.Context, inventory.Info, object.UnstructuredSet, apply.ApplierOptions) <-chan event.Event
}

// KptDestroyer is the interface exposed by cli-utils apply.Destroyer.
// Using an interface, instead of the concrete struct, allows for easier testing.
type KptDestroyer interface {
	Run(context.Context, inventory.Info, apply.DestroyerOptions) <-chan event.Event
}

// ClientSet wraps the various Kubernetes clients required for building a
// Config Sync applier.Applier.
type ClientSet struct {
	KptApplier    KptApplier
	KptDestroyer  KptDestroyer
	InvClient     inventory.Client
	Client        client.Client
	DynamicClient dynamic.Interface
	Mapper        meta.RESTMapper
	StatusMode    string
}

// NewClientSet constructs a new ClientSet.
func NewClientSet(c client.Client, configFlags *genericclioptions.ConfigFlags, statusMode string) (*ClientSet, error) {
	matchVersionKubeConfigFlags := util.NewMatchVersionFlags(configFlags)
	f := util.NewFactory(matchVersionKubeConfigFlags)

	var statusPolicy inventory.StatusPolicy
	if statusMode == StatusEnabled {
		klog.Infof("Enabled status reporting")
		statusPolicy = inventory.StatusPolicyAll
	} else {
		klog.Infof("Disabled status reporting")
		statusPolicy = inventory.StatusPolicyNone
	}
	invClient, err := inventory.NewClient(f, live.WrapInventoryObj,
		live.InvToUnstructuredFunc, statusPolicy, live.ResourceGroupGVK)
	if err != nil {
		return nil, err
	}

	applier, err := apply.NewApplierBuilder().
		WithInventoryClient(invClient).
		WithFactory(f).
		Build()
	if err != nil {
		return nil, err
	}

	destroyer, err := apply.NewDestroyerBuilder().
		WithInventoryClient(invClient).
		WithFactory(f).
		Build()
	if err != nil {
		return nil, err
	}

	dynamicClient, err := f.DynamicClient()
	if err != nil {
		return nil, err
	}
	mapper, err := f.ToRESTMapper()
	if err != nil {
		return nil, err
	}

	return &ClientSet{
		KptApplier:    applier,
		KptDestroyer:  destroyer,
		InvClient:     invClient,
		Client:        c,
		DynamicClient: dynamicClient,
		Mapper:        mapper,
		StatusMode:    statusMode,
	}, nil
}
