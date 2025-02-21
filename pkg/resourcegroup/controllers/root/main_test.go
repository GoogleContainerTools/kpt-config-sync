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

package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/resourcemap"
	"kpt.dev/configsync/pkg/resourcegroup/controllers/watch"
	"kpt.dev/configsync/pkg/testing/testcontroller"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	// +kubebuilder:scaffold:imports
)

var cfg *rest.Config
var testEnv *envtest.Environment

// TestMain executes the tests for this package, with a controller-runtime test
// environment.
func TestMain(m *testing.M) {
	setup := func() error {
		klog.InitFlags(nil)
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "manifests")},
		}
		var err error
		cfg, err = testEnv.Start()
		if err != nil {
			return err
		}
		s := scheme.Scheme
		if err := v1alpha1.AddToScheme(s); err != nil {
			return err
		}
		if err := v1.AddToScheme(s); err != nil {
			return err
		}
		return nil
	}
	cleanup := func() error {
		if testEnv != nil {
			return testEnv.Stop()
		}
		return nil
	}

	os.Exit(testcontroller.RunTestSuite(m, setup, cleanup))
}

func NewReconciler(mgr manager.Manager, logger logr.Logger) (*Reconciler, error) {
	resmap := resourcemap.NewResourceMap()
	watches, err := watch.NewManager(mgr.GetConfig(), mgr.GetHTTPClient(), resmap, nil, nil)
	if err != nil {
		return nil, err
	}
	r := &Reconciler{
		LoggingController: controllers.NewLoggingController(logger),
		client:            mgr.GetClient(),
		cfg:               mgr.GetConfig(),
		resMap:            resmap,
		watches:           watches,
	}
	obj := &v1alpha1.ResourceGroup{}
	_, err = ctrl.NewControllerManagedBy(mgr).
		For(obj).
		Build(r)
	if err != nil {
		return nil, err
	}
	return r, nil
}
