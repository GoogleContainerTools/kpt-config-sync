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

// Package watch includes a RestartableManager for dynamically watching resources.
package watch

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const restartControllerName = "restartable-manager-controller"

// RestartableManager is a controller manager that can be restarted based on the resources it syncs.
type RestartableManager interface {
	// Restart restarts the Manager and all the controllers it manages to watch the given GroupVersionKinds.
	// Returns if a restart actually happened and if there were any errors while doing it.
	Restart(gvks map[schema.GroupVersionKind]bool, force bool) (bool, error)
}

var _ RestartableManager = &subManager{}

// subManager is a manager.Manager that is managed by a higher-level controller.
type subManager struct {
	mgr manager.Manager
	// builder builds and initializes controllers for this Manager.
	builder ControllerBuilder
	// baseCfg is rest.Config that has no watched resources added to the scheme.
	baseCfg *rest.Config
	// cancel is a cancellation function for ctx. May be nil if ctx is unavailable
	cancel context.CancelFunc
	// errCh gets errors that come up when starting the subManager
	errCh chan event.GenericEvent
	// watching is a map of GVKs that the manager is currently watching
	watching map[schema.GroupVersionKind]bool
	// initTime is the instantiation time of the subManager
	initTime metav1.Time
}

// NewManager returns a new RestartableManager
func NewManager(mgr manager.Manager, builder ControllerBuilder) (RestartableManager, error) {
	sm, err := newManager(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	errCh := make(chan event.GenericEvent)
	r := &subManager{
		mgr:      sm,
		builder:  builder,
		baseCfg:  rest.CopyConfig(mgr.GetConfig()),
		errCh:    errCh,
		initTime: metav1.Now(),
	}

	// Configure a controller strictly for restarting the underlying manager if it fails to Start.
	c, err := controller.New(restartControllerName, mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "could not create %q", restartControllerName)
	}

	// Create a watch for errors when starting the subManager and force it to retry.
	managerRestartSource := &source.Channel{Source: errCh}
	if err := c.Watch(managerRestartSource, &handler.EnqueueRequestForObject{}); err != nil {
		return nil, errors.Wrapf(err, "could not watch manager initialization errors in the %q controller", restartControllerName)
	}

	return r, nil
}

func newManager(cfg *rest.Config) (manager.Manager, error) {
	// MetricsBindAddress 0 disables default metrics for the manager
	// If metrics are enabled, every time the subManger restarts it tries to bind to the metrics port
	// but fails because restarting does not unbind the port.
	// Instead of disabling these metrics, we could figure out a way to unbind the port on restart.
	opts := manager.Options{MetricsBindAddress: "0"}
	mgr, err := manager.New(rest.CopyConfig(cfg), opts)
	if err != nil {
		return nil, errors.Wrap(err, "could not create the manager for RestartableManager")
	}
	return mgr, nil
}

// context returns a new cancellable context.Context. If a context.Context was previously generated, it cancels it.
func (m *subManager) context() context.Context {
	if m.cancel != nil {
		m.cancel()
		klog.Info("Stopping SubManager")
	}
	var ctx context.Context
	ctx, m.cancel = context.WithCancel(context.Background())
	return ctx
}

func (m *subManager) needsRestart(toWatch map[schema.GroupVersionKind]bool) bool {
	return !equality.Semantic.DeepEqual(m.watching, toWatch)
}

// Restart implements RestartableManager.
func (m *subManager) Restart(gvks map[schema.GroupVersionKind]bool, force bool) (bool, error) {
	if !force && !m.needsRestart(gvks) {
		return false, nil
	}
	ctx := m.context()

	sm, err := newManager(m.baseCfg)
	if err != nil {
		return true, err
	}
	m.mgr = sm

	if err := m.builder.StartControllers(m.mgr, gvks, m.initTime); err != nil {
		return true, errors.Wrap(err, "could not start controllers")
	}

	klog.Info("Starting subManager")
	go func(ctx context.Context) {
		if err := m.mgr.Start(ctx); err != nil {
			// subManager could not successfully start, so we must force it to restart next reconcile.
			klog.Errorf("Error starting subManager, restarting: %v", err)

			// TODO: Not an intended use case for GenericEvent. Refactor.
			u := &unstructured.Unstructured{}
			u.SetNamespace("Restart")
			u.SetName("DueToError")
			m.errCh <- event.GenericEvent{Object: u}
		}
	}(ctx)

	m.watching = gvks
	return true, nil
}

// Reconcile will only be called due to an error starting the submanager on a previous Restart.
func (m *subManager) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	_, err := m.Restart(m.watching, true)
	return reconcile.Result{}, err
}
