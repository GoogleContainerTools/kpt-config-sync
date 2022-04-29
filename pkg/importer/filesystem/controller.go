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

package filesystem

import (
	"os"
	"path"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kpt.dev/configsync/pkg/syncer/decode"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	utildiscovery "kpt.dev/configsync/pkg/util/discovery"
)

const (
	// GitImporterName is the name of the git-importer Deployment.
	GitImporterName = "git-importer"

	// pollFilesystem is an invalid resource name used to signal that the event
	// triggering the reconcile is a periodic poll of the filesystem. The reason
	// it is an invalid name is we want to prevent treating a namespaceconfig
	// change as a poll filesystem event, if it happens to be named poll-filesystem.
	pollFilesystem = "@poll-filesystem"
)

// AddController adds the git-importer controller to the manager.
func AddController(clusterName string, mgr manager.Manager, gitDir, policyDirRelative string,
	pollPeriod time.Duration) error {
	syncerClient := syncerclient.New(mgr.GetClient(), metrics.APICallDuration)

	klog.Infof("Policy dir: %s", path.Join(gitDir, policyDirRelative))

	var err error
	dc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return errors.Wrapf(err, "failed to create discoveryclient")
	}

	if err = ValidateInstallation(dc); err != nil {
		return err
	}

	// If SOURCE_FORMAT is invalid, assume hierarchy.
	format := SourceFormat(os.Getenv(SourceFormatKey))
	cfgParser := NewParser(&reader.File{})
	if format == "" {
		format = SourceFormatHierarchy
	}

	decoder := decode.NewGenericResourceDecoder(runtime.NewScheme())
	r, err := newReconciler(clusterName, gitDir, policyDirRelative, cfgParser,
		syncerClient, dc, mgr.GetCache(), decoder, format)
	if err != nil {
		return errors.Wrap(err, "failure creating reconciler")
	}
	c, err := controller.New(GitImporterName, mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return errors.Wrap(err, "failure creating controller")
	}

	// We map all requests generated from from watching Nomos CRs to the same request.
	// The reason we do this is because the logic is the same in the reconcile loop,
	// regardless of which resource changed. Having a constant used for the reconcile.Request
	// avoids doing redundant reconciles.
	mapToConstant := handler.EnqueueRequestsFromMapFunc(nomosResourceRequest)

	// Watch all Nomos CRs that are managed by the importer.
	if err = c.Watch(&source.Kind{Type: &v1.ClusterConfig{}}, mapToConstant); err != nil {
		return errors.Wrapf(err, "could not watch ClusterConfigs in the %q controller", GitImporterName)
	}
	if err = c.Watch(&source.Kind{Type: &v1.NamespaceConfig{}}, mapToConstant); err != nil {
		return errors.Wrapf(err, "could not watch NamespaceConfigs in the %q controller", GitImporterName)
	}
	if err = c.Watch(&source.Kind{Type: &v1.Sync{}}, mapToConstant); err != nil {
		return errors.Wrapf(err, "could not watch Syncs in the %q controller", GitImporterName)
	}

	return watchFileSystem(c, pollPeriod)
}

// ValidateInstallation checks to see if Nomos is installed on a server,
// given a client that returns a CachedDiscoveryInterface.
// TODO: Server-side validation for this check.
func ValidateInstallation(dc utildiscovery.ServerResourcer) status.MultiError {
	lists, err := utildiscovery.GetResources(dc)
	if err != nil {
		return status.APIServerError(err, "could not get discovery client")
	}

	scoper := utildiscovery.Scoper{}
	return scoper.AddAPIResourceLists(lists)
}

// watchFileSystem issues a reconcile.Request after every pollPeriod.
func watchFileSystem(c controller.Controller, pollPeriod time.Duration) error {
	pollCh := make(chan event.GenericEvent)
	go func() {
		ticker := time.NewTicker(pollPeriod)
		for range ticker.C {
			// TODO: Not an intended use case for GenericEvent. Refactor.
			u := &unstructured.Unstructured{}
			u.SetName(pollFilesystem)
			pollCh <- event.GenericEvent{Object: u}
		}
	}()

	pollSource := &source.Channel{Source: pollCh}
	if err := c.Watch(pollSource, &handler.EnqueueRequestForObject{}); err != nil {
		return errors.Wrapf(err, "could not watch manager initialization errors in the %q controller", GitImporterName)
	}

	return nil
}

// nomosResourceRequest maps resources being watched,
// to reconciliation requests for a cluster-scoped resource with name "nomos-resource".
func nomosResourceRequest(_ client.Object) []reconcile.Request {
	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name: "nomos-resource",
		},
	}}
}
