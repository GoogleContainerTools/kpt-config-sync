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

package reconcile

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	syncclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"kpt.dev/configsync/pkg/util/repo"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// RepoStatus is a reconciler for maintaining the status field of the Repo resource based upon
// updates from the other syncer reconcilers.
type repoStatus struct {
	// client is used to list configs on the cluster when building state for a commit
	client *syncclient.Client
	// client is used to perform CRUD operations on the Repo resource
	rClient *repo.Client

	// now returns the current time.
	now func() metav1.Time

	//initTime is the reconciler's instantiation time
	initTime metav1.Time
}

// syncState represents the current status of the syncer and all commits that it is reconciling.
type syncState struct {
	// reconciledCommits is a map of commit tokens that have configs that are already reconciled
	reconciledCommits map[string]bool
	// unreconciledCommits is a map of commit token to list of configs currently being reconciled for
	// that commit
	unreconciledCommits map[string][]string
	// configs is a map of config name to the state of the config being reconciled
	configs map[string]configState

	//resourceConditions contains health status for all resources synced in namespace and cluster configs
	resourceConditions []v1.ResourceCondition
}

// configState represents the current status of a ClusterConfig or NamespaceConfig being reconciled.
type configState struct {
	// commit is the version token of the change to which the config is being reconciled
	commit string
	// errors is a list of any errors that occurred which prevented a successful reconcile
	errors []v1.ConfigManagementError
}

// NewRepoStatus returns a reconciler for maintaining the status field of the Repo resource.
func NewRepoStatus(sClient *syncclient.Client, now func() metav1.Time) reconcile.Reconciler {
	return &repoStatus{
		client:   sClient,
		rClient:  repo.New(sClient),
		now:      now,
		initTime: now(),
	}
}

// Reconcile is the Reconcile callback for RepoStatus reconciler.
func (r *repoStatus) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	start := r.now()
	metrics.ReconcileEventTimes.WithLabelValues("repo").Set(float64(start.Unix()))

	result, err := r.reconcile(ctx)
	metrics.ReconcileDuration.WithLabelValues("repo", metrics.StatusLabel(err)).Observe(time.Since(start.Time).Seconds())

	return result, err
}

func (r *repoStatus) reconcile(ctx context.Context) (reconcile.Result, error) {
	repoObj, sErr := r.rClient.GetOrCreateRepo(ctx)
	if sErr != nil {
		klog.Errorf("Failed to fetch Repo: %v", sErr)
		return reconcile.Result{Requeue: true}, sErr
	}

	state, err := r.buildState(ctx)
	if err != nil {
		klog.Errorf("Failed to build sync state: %v", err)
		return reconcile.Result{Requeue: true}, sErr
	}

	state.merge(&repoObj.Status, r.now)

	// We used to stop reconciliation here if the sync token is the same as
	// import token.  We no longer do that, to ensure that even non-monotonic
	// status updates are reconciled properly.  See b/131250908 why this is
	// relevant.  Instead, we rely on UpdateSyncStatus to skip updates if the
	// new sync status is equal to the old one.
	updatedRepo, err := r.rClient.UpdateSyncStatus(ctx, repoObj)
	if err != nil {
		klog.Errorf("Failed to update RepoSyncStatus: %v", err)
		return reconcile.Result{Requeue: true}, sErr
	}

	// If the ImportToken is different in the updated repo, it means that the importer made a change
	// in the middle of this reconcile. In that case we tell the controller to requeue the request so
	// that we can recalculate sync status with up-to-date information.
	requeue := updatedRepo.Status.Import.Token != repoObj.Status.Import.Token
	return reconcile.Result{Requeue: requeue}, sErr
}

// buildState returns a freshly initialized syncState based upon the current configs on the cluster.
func (r *repoStatus) buildState(ctx context.Context) (*syncState, error) {
	ccList := &v1.ClusterConfigList{}
	if err := r.client.List(ctx, ccList); err != nil {
		return nil, errors.Wrapf(err, "listing ClusterConfigs")
	}
	ncList := &v1.NamespaceConfigList{}
	if err := r.client.List(ctx, ncList); err != nil {
		return nil, errors.Wrapf(err, "listing NamespaceConfigs")
	}
	return r.processConfigs(ccList, ncList), nil
}

// processConfigs is broken out to make unit testing easier.
func (r *repoStatus) processConfigs(ccList *v1.ClusterConfigList, ncList *v1.NamespaceConfigList) *syncState {
	state := &syncState{
		reconciledCommits:   make(map[string]bool),
		unreconciledCommits: make(map[string][]string),
		configs:             make(map[string]configState),
	}

	for _, cc := range ccList.Items {
		state.addConfigToCommit(clusterPrefix(cc.Name), cc.Spec.Token, cc.Status.Token, cc.Status.SyncState, r.initTime, cc.Status.SyncTime, metav1.Time{}, cc.Status.SyncErrors)
		state.resourceConditions = append(state.resourceConditions, cc.Status.ResourceConditions...)
	}
	for _, nc := range ncList.Items {
		state.addConfigToCommit(namespacePrefix(nc.Name), nc.Spec.Token, nc.Status.Token, nc.Status.SyncState, r.initTime, nc.Status.SyncTime, nc.Spec.DeleteSyncedTime, nc.Status.SyncErrors)
		state.resourceConditions = append(state.resourceConditions, nc.Status.ResourceConditions...)
	}

	return state
}

// isConfigReconciled checks if the config is reconciled.
// initTime is the repostatus-reconciler's instantiation time. It is used to force-update
// everything on startup so we don't assume everything is already correctly synced.
// A config is reconciled when the import token is the same as the sync token, and
// the sync state is `synced`, and
// the sync time is after the reconciler's instantiation time, and
// the sync time is after the deleteSyncedTime if set.
func isConfigReconciled(importToken, syncToken string,
	syncState v1.ConfigSyncState, initTime, syncTime, deleteSyncedTime metav1.Time) bool {
	return importToken == syncToken && syncState == v1.StateSynced &&
		syncTime.After(initTime.Time) && syncTime.After(deleteSyncedTime.Time)
}

// addConfigToCommit adds the specified config data to the commit for the specified syncToken.
// initTime is the repostatus-reconciler's instantiation time. It is used to force-update
// everything on startup so we don't assume everything is already correctly synced.
func (s *syncState) addConfigToCommit(name, importToken, syncToken string, syncState v1.ConfigSyncState, initTime, syncTime, deleteSyncedTime metav1.Time, errs []v1.ConfigManagementError) {
	var commitHash string
	if len(errs) > 0 {
		// If there are errors, then the syncToken indicates the unreconciled commit.
		commitHash = syncToken
	} else if isConfigReconciled(importToken, syncToken, syncState, initTime, syncTime, deleteSyncedTime) {
		// If the tokens match and there are no errors, then the config is already done being processed.
		if _, ok := s.unreconciledCommits[syncToken]; !ok {
			s.reconciledCommits[syncToken] = true
		}
		return
	} else {
		// If there are no errors and the tokens do not match, then the importToken indicates the unreconciled commit
		commitHash = importToken
	}
	s.unreconciledCommits[commitHash] = append(s.unreconciledCommits[commitHash], name)
	s.configs[name] = configState{commit: commitHash, errors: errs}
	// If we previously marked the commit as reconciled for a different config, remove the entry.
	delete(s.reconciledCommits, commitHash)
}

// merge updates the given RepoStatus with current configs and commits in the syncState.
func (s syncState) merge(repoStatus *v1.RepoStatus, now func() metav1.Time) {
	var updated bool
	if len(s.unreconciledCommits) == 0 {
		if len(repoStatus.Source.Errors) > 0 || len(repoStatus.Import.Errors) > 0 {
			klog.Infof("No unreconciled commits but there are source/import errors. RepoStatus sync token will remain at %q.", repoStatus.Sync.LatestToken)
		} else if repoStatus.Sync.LatestToken != repoStatus.Import.Token {
			klog.Infof("All commits are reconciled, updating RepoStatus sync token to %q.", repoStatus.Import.Token)
			repoStatus.Sync.LatestToken = repoStatus.Import.Token
			updated = true
		}
	} else {
		klog.Infof("RepoStatus import token at %q, but %d commits are unreconciled. RepoStatus sync token will remain at %q.",
			repoStatus.Import.Token, len(s.unreconciledCommits), repoStatus.Sync.LatestToken)
		if klog.V(2).Enabled() {
			for token, cfgs := range s.unreconciledCommits {
				klog.Infof("Unreconciled configs for commit %q: %v", token, cfgs)
			}
		}
	}

	var inProgress []v1.RepoSyncChangeStatus
	for token, configNames := range s.unreconciledCommits {
		changeStatus := v1.RepoSyncChangeStatus{Token: token}
		for _, name := range configNames {
			config := s.configs[name]
			changeStatus.Errors = append(changeStatus.Errors, config.errors...)
		}
		inProgress = append(inProgress, changeStatus)
	}

	sort.Slice(inProgress, func(i, j int) bool {
		return strings.Compare(inProgress[i].Token, inProgress[j].Token) < 0
	})

	nonEmpty := len(repoStatus.Sync.InProgress) > 0 || len(inProgress) > 0
	if nonEmpty && !reflect.DeepEqual(repoStatus.Sync.InProgress, inProgress) {
		repoStatus.Sync.InProgress = inProgress
		updated = true
	}

	nonEmpty = len(repoStatus.Sync.ResourceConditions) > 0 || len(s.resourceConditions) > 0
	if nonEmpty && !reflect.DeepEqual(repoStatus.Sync.ResourceConditions, s.resourceConditions) {
		repoStatus.Sync.ResourceConditions = s.resourceConditions
		updated = true
	}

	if updated {
		repoStatus.Sync.LastUpdate = now()
	}
}

// clusterPrefix returns the given name prefixed to indicate it is for a ClusterConfig.
func clusterPrefix(name string) string {
	return "cc:" + name
}

// namespacePrefix returns the given name prefixed to indicate it is for a NamespaceConfig.
func namespacePrefix(name string) string {
	return "nc:" + name
}
