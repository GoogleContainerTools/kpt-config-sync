// Copyright 2025 Google LLC
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

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"kpt.dev/configsync/pkg/api/configsync/v1beta1"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
}

// Define a new struct for the unique identifier
type SyncID struct {
	Name      string
	Kind      string
	Namespace string
}

type SyncStatusController struct {
	client        client.Client
	log           logr.Logger
	statusTracker *StatusTracker
}

func (c *SyncStatusController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	var syncID SyncID
	var status v1beta1.Status
	var generation int64
	var observedGeneration int64

	// Try to get as RootSync first
	var root v1beta1.RootSync
	if err := c.client.Get(ctx, req.NamespacedName, &root); err == nil {
		syncID = SyncID{
			Name:      root.Name,
			Kind:      root.Kind,
			Namespace: root.Namespace,
		}
		status = root.Status.Status
		generation = root.Generation
		observedGeneration = root.Status.ObservedGeneration
		errs, commit := c.processRootSyncConditions(root.Status.Conditions)
		if err := c.handleErrors(syncID, errs, commit, generation, observedGeneration); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		// Fall back to RepoSync if RootSync not found
		var repo v1beta1.RepoSync
		if err := c.client.Get(ctx, req.NamespacedName, &repo); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		syncID = SyncID{
			Name:      repo.Name,
			Kind:      repo.Kind,
			Namespace: req.Namespace,
		}
		status = repo.Status.Status
		generation = repo.Generation
		observedGeneration = repo.Status.ObservedGeneration
		errs, commit := c.processRepoSyncConditions(repo.Status.Conditions)
		if err := c.handleErrors(syncID, errs, commit, generation, observedGeneration); err != nil {
			return reconcile.Result{}, err
		}
	}

	// Process sync status
	if !isSuccessfulSync(status) {
		errors, commit := extractErrorAndCommitFromStatus(status)
		if err := c.handleErrors(syncID, errors, commit, generation, observedGeneration); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// handleErrors aggregates error messages and logs them if they haven't been logged already.
func (c *SyncStatusController) handleErrors(rsyncID SyncID, errMessage string, commit string, generation int64, observedGeneration int64) error {
	if errMessage == "" {
		return nil
	}
	if c.statusTracker.IsLogged(rsyncID, commit, errMessage) {
		return nil
	}
	c.log.Info("Sync error detected", "sync", rsyncID.Name, "namespace", rsyncID.Namespace, "kind", rsyncID.Kind, "commit", commit, "status", "failed", "generation", generation, "observedGeneration", observedGeneration, "error", errMessage)

	c.statusTracker.MarkLogged(rsyncID, commit, errMessage)
	return nil
}

func (c *SyncStatusController) processRootSyncConditions(conditions []v1beta1.RootSyncCondition) (string, string) {
	var errorMessages []string
	var commit string

	for _, condition := range conditions {
		isErrorCondition := false
		var conditionType v1beta1.RootSyncConditionType

		switch condition.Type {
		case v1beta1.RootSyncStalled:
			if condition.Status == metav1.ConditionTrue {
				isErrorCondition = true
				conditionType = condition.Type
			}
		case v1beta1.RootSyncReconcilerFinalizerFailure:
			if condition.Status == metav1.ConditionTrue {
				isErrorCondition = true
				conditionType = condition.Type
			}
			// Other condition types (Reconciling, Syncing, ReconcilerFinalizing)
			// are not treated as primary error indicators here for aggregation.
			// Stalled usually captures persistent source/rendering issues,
			// and Sync errors are handled via status.errors.
		}

		if isErrorCondition {
			errorMessages = append(errorMessages,
				fmt.Sprintf("%s: %s", conditionType, condition.Message))
			if commit == "" && condition.Commit != "" {
				commit = condition.Commit
			}
		}
	}

	return strings.Join(errorMessages, "\n"), commit
}

func (c *SyncStatusController) processRepoSyncConditions(conditions []v1beta1.RepoSyncCondition) (string, string) {
	var errorMessages []string
	var commit string

	for _, condition := range conditions {
		isErrorCondition := false
		var conditionType v1beta1.RepoSyncConditionType

		switch condition.Type {
		case v1beta1.RepoSyncStalled:
			if condition.Status == metav1.ConditionTrue {
				isErrorCondition = true
				conditionType = condition.Type
			}
		case v1beta1.RepoSyncReconcilerFinalizerFailure:
			if condition.Status == metav1.ConditionTrue {
				isErrorCondition = true
				conditionType = condition.Type
			}
			// Other condition types (Reconciling, Syncing, ReconcilerFinalizing)
			// are not treated as primary error indicators here for aggregation.
		}

		if isErrorCondition {
			errorMessages = append(errorMessages,
				fmt.Sprintf("%s: %s", conditionType, condition.Message))
			if commit == "" && condition.Commit != "" {
				commit = condition.Commit
			}
		}
	}

	return strings.Join(errorMessages, "\n"), commit
}

// isSuccessfulSync checks if the rsync status no errors in rsync status and all commits match.
func isSuccessfulSync(status v1beta1.Status) bool {
	allCommitsMatch := status.Source.Commit != "" &&
		status.Source.Commit == status.Rendering.Commit &&
		status.Rendering.Commit == status.Sync.Commit

	return allCommitsMatch && !hasStatusError(status)
}

// hasStatusError checks if there are any errors in the rsync status.
func hasStatusError(status v1beta1.Status) bool {
	return status.Source.Errors != nil ||
		status.Rendering.Errors != nil ||
		status.Sync.Errors != nil
}

// extractErrorAndCommitFromStatus extracts errors and the associated commit from the status.
func extractErrorAndCommitFromStatus(status v1beta1.Status) (string, string) {
	if status.Source.Errors != nil {
		return aggregateErrors(status.Source.Errors), status.Source.Commit
	}
	if status.Rendering.Errors != nil {
		return aggregateErrors(status.Rendering.Errors), status.Rendering.Commit
	}
	return aggregateErrors(status.Sync.Errors), status.Sync.Commit
}

// aggregateErrors aggregates error messages from a slice of ConfigSyncError
// and returns a single error message.
func aggregateErrors(errors []v1beta1.ConfigSyncError) string {
	if len(errors) == 0 {
		return ""
	}

	var errorMessages []string
	for _, err := range errors {
		errorMessages = append(errorMessages, fmt.Sprintf("Code: %s, Message: %s", err.Code, err.ErrorMessage))
	}

	return "aggregated errors: " + strings.Join(errorMessages, "\n")
}

// Update the StatusTracker to use SyncID
type StatusTracker struct {
	mu   sync.Mutex
	seen map[SyncID]map[string]struct{}
}

// NewStatusTracker creates a new StatusTracker instance.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		seen: make(map[SyncID]map[string]struct{}),
	}
}

// compositeKey generates a unique key for a commit and message.
// Using a simple concatenation with a separator. Hashing could be used for long messages.
func compositeKey(commit, message string) string {
	return commit + "::" + message
}

// Update the IsLogged and MarkLogged methods
func (s *StatusTracker) IsLogged(syncID SyncID, commit, message string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := compositeKey(commit, message)
	if commitMap, ok := s.seen[syncID]; ok {
		_, logged := commitMap[key]
		return logged
	}
	return false
}

func (s *StatusTracker) MarkLogged(syncID SyncID, commit, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := compositeKey(commit, message)
	if _, ok := s.seen[syncID]; !ok {
		s.seen[syncID] = make(map[string]struct{})
	}
	s.seen[syncID][key] = struct{}{}
}

func main() {
	zapLog, _ := zap.NewProduction()
	logger := zapr.NewLogger(zapLog)

	ctrl.SetLogger(logger)

	logger.Info("Starting standalone SyncStatusController")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
	})
	if err != nil {
		logger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctrlr := &SyncStatusController{
		client:        mgr.GetClient(),
		log:           logger,
		statusTracker: NewStatusTracker(),
	}

	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.RootSync{}).
		Complete(ctrlr); err != nil {
		logger.Error(err, "unable to watch RootSync")
		os.Exit(1)
	}
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.RepoSync{}).
		Complete(ctrlr); err != nil {
		logger.Error(err, "unable to watch RepoSync")
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "problem running manager")
		os.Exit(1)
	}
}
