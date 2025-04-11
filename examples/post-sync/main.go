//  Copyright 2025 Google LLC
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

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
	isRootSync := req.Namespace == "config-management-system"
	var status v1beta1.Status

	if isRootSync {
		var root v1beta1.RootSync
		if err := c.client.Get(ctx, req.NamespacedName, &root); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		status = root.Status.Status
		syncID := SyncID{Name: root.Name, Kind: root.Kind, Namespace: root.Namespace}

		// Process RootSync conditions
		if hasRootSyncConditionErrors(root.Status.Conditions) {
			errors, commit := extractRootSyncConditionErrors(root.Status.Conditions)
			if err := c.handleErrors(syncID, errors, commit); err != nil {
				return reconcile.Result{}, err
			}
		}
		return c.processSync(syncID, status)
	} else {
		var repo v1beta1.RepoSync
		if err := c.client.Get(ctx, req.NamespacedName, &repo); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		status = repo.Status.Status
		syncID := SyncID{Name: repo.Name, Kind: repo.Kind, Namespace: req.NamespacedName.Namespace}

		// Process RepoSync conditions
		if hasRepoSyncConditionErrors(repo.Status.Conditions) {
			errors, commit := extractRepoSyncConditionErrors(repo.Status.Conditions)
			if err := c.handleErrors(syncID, errors, commit); err != nil {
				return reconcile.Result{}, err
			}
		}
		return c.processSync(syncID, status)
	}
}

func (c *SyncStatusController) processSync(syncID SyncID, status v1beta1.Status) (reconcile.Result, error) {

	if !isSuccessfulSync(status) {
		errors, commit := extractErrorAndCommitFromStatus(status)
		if err := c.handleErrors(syncID, errors, commit); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// handleErrors aggregates error messages and logs them if they haven't been logged already.
func (c *SyncStatusController) handleErrors(rsyncID SyncID, errors []v1beta1.ConfigSyncError, commit string) error {
	errMessage := aggregateErrors(errors)
	if errMessage == "" {
		return nil
	}
	if c.statusTracker.IsLogged(rsyncID, commit, errMessage) {
		return nil
	}
	c.log.Info("Sync error detected", "sync", rsyncID.Name, "namespace", rsyncID.Namespace, "kind", rsyncID.Kind, "commit", commit, "status", "failed", "error", errMessage)

	c.statusTracker.MarkLogged(rsyncID, commit, errMessage)
	return nil
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
func extractErrorAndCommitFromStatus(status v1beta1.Status) ([]v1beta1.ConfigSyncError, string) {
	if status.Source.Errors != nil {
		return status.Source.Errors, status.Source.Commit
	}
	if status.Rendering.Errors != nil {
		return status.Rendering.Errors, status.Rendering.Commit
	}
	return status.Sync.Errors, status.Sync.Commit
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

// hasRootSyncConditionErrors checks if there are any errors in the RootSync status conditions.
func hasRootSyncConditionErrors(conditions []v1beta1.RootSyncCondition) bool {
	for _, condition := range conditions {
		if condition.Status == "True" {
			return true
		}
	}
	return false
}

// extractRootSyncConditionErrors extracts errors from the RootSync status conditions.
func extractRootSyncConditionErrors(conditions []v1beta1.RootSyncCondition) ([]v1beta1.ConfigSyncError, string) {
	var errors []v1beta1.ConfigSyncError
	var commit string

	for _, condition := range conditions {
		if condition.Status == "True" {
			errors = append(errors, condition.Errors...)
			commit = condition.Commit // Assuming commit is available in the condition
		}
	}
	return errors, commit
}

// hasRepoSyncConditionErrors checks if there are any errors in the RepoSync status conditions.
func hasRepoSyncConditionErrors(conditions []v1beta1.RepoSyncCondition) bool {
	for _, condition := range conditions {
		if condition.Status == "True" {
			return true
		}
	}
	return false
}

// extractRepoSyncConditionErrors extracts errors from the RepoSync status conditions.
func extractRepoSyncConditionErrors(conditions []v1beta1.RepoSyncCondition) ([]v1beta1.ConfigSyncError, string) {
	var errors []v1beta1.ConfigSyncError
	var commit string

	for _, condition := range conditions {
		if condition.Status == "True" {
			errors = append(errors, condition.Errors...)
			commit = condition.Commit
		}
	}
	return errors, commit
}

// Update the StatusTracker to use SyncID
type StatusTracker struct {
	mu          sync.Mutex
	seen        map[SyncID]string
	lastMessage string
}

// NewStatusTracker creates a new StatusTracker instance.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		seen: make(map[SyncID]string),
	}
}

// Update the IsLogged and MarkLogged methods
func (s *StatusTracker) IsLogged(syncID SyncID, commit, message string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[syncID] == commit && s.lastMessage == message
}

func (s *StatusTracker) MarkLogged(syncID SyncID, commit, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[syncID] = commit
	s.lastMessage = message
}

func main() {
	zapLog, _ := zap.NewProduction()
	logger := zapr.NewLogger(zapLog)
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
