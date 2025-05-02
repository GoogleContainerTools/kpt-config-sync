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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

const (
	// RootSyncKind represents the Config Sync RootSync resource type
	RootSyncKind = "RootSync"
	// RepoSyncKind represents the Config Sync RepoSync resource type
	RepoSyncKind = "RepoSync"
)

// SyncID uniquely identifies a sync resource
type SyncID struct {
	Name      string
	Kind      string
	Namespace string
}

// StatusTracker tracks which errors have been logged to prevent duplicate logging.
type StatusTracker struct {
	mu   sync.Mutex
	seen map[string]struct{} // Using a single map with hashed keys
}

// NewStatusTracker creates a new StatusTracker instance
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{
		seen: make(map[string]struct{}),
	}
}

// generateKey creates a unique key for a sync resource, commit, and error message
func generateKey(syncID SyncID, commit, message string) string {
	// Include SyncID information in the key
	key := fmt.Sprintf("%s:%s:%s:%s:%s",
		syncID.Kind,
		syncID.Namespace,
		syncID.Name,
		commit,
		message)

	// Hash the key to save memory
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// IsLogged checks if an error has been logged
func (s *StatusTracker) IsLogged(syncID SyncID, commit, message string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := generateKey(syncID, commit, message)
	_, logged := s.seen[key]
	return logged
}

// MarkLogged marks an error as logged
func (s *StatusTracker) MarkLogged(syncID SyncID, commit, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := generateKey(syncID, commit, message)
	s.seen[key] = struct{}{}
}

// SyncStatusController reconciles RootSync and RepoSync resources by monitoring
// their status fields for errors and logging them in a structured format.
type SyncStatusController struct {
	client        client.Client
	log           logr.Logger
	statusTracker *StatusTracker
	syncKind      string // Explicit field to indicate what kind of resource we're watching
}

// NewSyncStatusController creates a new controller instance with the specified parameters.
// It validates the syncKind and initializes the controller with the provided dependencies.
func NewSyncStatusController(client client.Client, log logr.Logger, statusTracker *StatusTracker, syncKind string) *SyncStatusController {
	// Validate sync kind
	if syncKind != RootSyncKind && syncKind != RepoSyncKind {
		panic(fmt.Sprintf("invalid sync kind: %s, must be either %s or %s", syncKind, RootSyncKind, RepoSyncKind))
	}
	return &SyncStatusController{
		client:        client,
		log:           log,
		statusTracker: statusTracker,
		syncKind:      syncKind,
	}
}

// Reconcile implements the reconciliation loop for RootSync and RepoSync resources.
// It processes both the resource's conditions and status fields to detect and log errors.
// The function ensures that each unique error is logged only once using the StatusTracker.
func (c *SyncStatusController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	var syncID SyncID
	var status v1beta1.Status
	var generation int64
	var observedGeneration int64
	var conditionErrorMessage, commit string

	// Use the explicit syncKind to know which type to fetch
	switch c.syncKind {
	case RootSyncKind:
		var root v1beta1.RootSync
		if err := c.client.Get(ctx, req.NamespacedName, &root); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		syncID = c.getSyncID(req)
		status = root.Status.Status
		generation = root.Generation
		observedGeneration = root.Status.ObservedGeneration
		conditionErrorMessage, commit = c.processRootSyncConditions(root.Status.Conditions)

	case RepoSyncKind:
		var repo v1beta1.RepoSync
		if err := c.client.Get(ctx, req.NamespacedName, &repo); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		syncID = c.getSyncID(req)
		status = repo.Status.Status
		generation = repo.Generation
		observedGeneration = repo.Status.ObservedGeneration
		conditionErrorMessage, commit = c.processRepoSyncConditions(repo.Status.Conditions)

	default:
		c.log.Error(fmt.Errorf("unknown sync kind: %s", c.syncKind), "unrecognized syncKind")
		return reconcile.Result{}, fmt.Errorf("unknown sync kind: %s", c.syncKind)
	}

	// Process any errors from conditions
	if err := c.handleErrors(syncID, conditionErrorMessage, commit, generation, observedGeneration, false); err != nil {
		return reconcile.Result{}, err
	}

	// Process sync status
	if !isSuccessfulSync(status, generation) {
		statusErrorMessage, commit, isTruncated := extractErrorAndCommitFromStatus(status)
		if err := c.handleErrors(syncID, statusErrorMessage, commit, generation, observedGeneration, isTruncated); err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

func (c *SyncStatusController) getSyncID(req reconcile.Request) SyncID {
	return SyncID{
		Name:      req.Name,
		Kind:      c.syncKind,
		Namespace: req.Namespace,
	}
}

// handleErrors processes error messages and logs them if they haven't been logged before.
// It includes metadata such as the sync resource details, commit hash, and generation numbers.
// Parameters:
//   - rsyncID: The identifier of the sync resource
//   - errMessage: The error message to process
//   - commit: The commit hash associated with the error
//   - generation: The resource's current generation
//   - observedGeneration: The last generation that was processed
//   - isTruncated: Whether the error message was truncated
func (c *SyncStatusController) handleErrors(rsyncID SyncID, errMessage string, commit string, generation int64, observedGeneration int64, isTruncated bool) error {
	if errMessage == "" {
		return nil
	}
	if c.statusTracker.IsLogged(rsyncID, commit, errMessage) {
		return nil
	}
	c.log.Info("Sync error detected",
		"kind", rsyncID.Kind,
		"namespace", rsyncID.Namespace,
		"sync", rsyncID.Name,
		"commit", commit,
		"generation", generation,
		"observedGeneration", observedGeneration,
		"status", "error",
		"error", errMessage,
		"truncated", isTruncated)

	c.statusTracker.MarkLogged(rsyncID, commit, errMessage)
	return nil
}

// processRootSyncConditions analyzes RootSync conditions and extracts error information.
// It aggregates error messages from relevant condition types and returns the combined
// error message along with the associated commit hash.
// Returns:
//   - string: Aggregated error message (empty if no errors)
//   - string: Associated commit hash (empty if not available)
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
			// Source, Rendering, and Sync are all checked for errors separately in hasStatusError.
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

// processRepoSyncConditions analyzes RepoSync conditions and extracts error information.
// Similar to processRootSyncConditions but for RepoSync resources.
// Returns:
//   - string: Aggregated error message (empty if no errors)
//   - string: Associated commit hash (empty if not available)
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
			// Source, Rendering, and Sync are all checked for errors separately in hasStatusError.
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

// isSuccessfulSync checks if the rsync status has no errors and all commits match
func isSuccessfulSync(status v1beta1.Status, generation int64) bool {
	allCommitsMatch := status.Source.Commit != "" &&
		status.Source.Commit == status.Rendering.Commit &&
		status.Rendering.Commit == status.Sync.Commit &&
		status.LastSyncedCommit == status.Source.Commit &&
		status.ObservedGeneration == generation

	return allCommitsMatch && !hasStatusError(status)
}

// hasStatusError checks if there are any errors in the rsync status
func hasStatusError(status v1beta1.Status) bool {
	return status.Source.Errors != nil ||
		status.Rendering.Errors != nil ||
		status.Sync.Errors != nil
}

// extractErrorAndCommitFromStatus extracts errors and the associated commit from the status
func extractErrorAndCommitFromStatus(status v1beta1.Status) (string, string, bool) {
	if len(status.Source.Errors) > 0 {
		truncated := false
		if status.Source.ErrorSummary != nil {
			truncated = status.Source.ErrorSummary.Truncated
		}
		return aggregateErrors(status.Source.Errors), status.Source.Commit, truncated
	}
	if len(status.Rendering.Errors) > 0 {
		truncated := false
		if status.Rendering.ErrorSummary != nil {
			truncated = status.Rendering.ErrorSummary.Truncated
		}
		return aggregateErrors(status.Rendering.Errors), status.Rendering.Commit, truncated
	}
	if len(status.Sync.Errors) > 0 {
		truncated := false
		if status.Sync.ErrorSummary != nil {
			truncated = status.Sync.ErrorSummary.Truncated
		}
		return aggregateErrors(status.Sync.Errors), status.Sync.Commit, truncated
	}
	return "", "", false
}

// aggregateErrors combines multiple Config Sync errors into a single error message.
// It preserves the individual error messages while creating a consolidated view.
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
