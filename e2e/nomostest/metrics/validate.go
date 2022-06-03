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

package metrics

import (
	"strconv"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"go.opencensus.io/tag"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
)

// ConfigSyncMetrics is a map from metric names to its measurements.
type ConfigSyncMetrics map[string][]Measurement

// Measurement is a recorded data point with a list of tags and a value.
type Measurement struct {
	Tags  []tag.Tag
	Value string
}

// GVKMetric is used for validating the count aggregated metrics that have a GVK
// type tag (`api_duration_seconds`, `apply_operations`, and `watches`).
type GVKMetric struct {
	GVK      string
	APIOp    string
	ApplyOps []Operation
	Watches  string
}

// Operation encapsulates an operation in the applier (create, update, delete)
// with its count value.
type Operation struct {
	Name  string
	Count int
}

// Validation evaluates a Measurement, returning an error if it fails validation.
type Validation func(metric Measurement) error

const (
	// MetricsPort is the port where metrics are exposed
	MetricsPort = ":8675"
	// OtelDeployment is name of the otel-collector deployment
	OtelDeployment = "deployment/otel-collector"
)

// ResourceCreated encapsulates the expected metric data when a new resource is created
// in Config Sync.
func ResourceCreated(gvk string) GVKMetric {
	return GVKMetric{
		GVK:   gvk,
		APIOp: "update",
		ApplyOps: []Operation{
			{Name: "update", Count: 1},
		},
		Watches: "1",
	}
}

// ResourcePatched encapsulates the expected metric data when an existing resource is
// patched in Config Sync.
func ResourcePatched(gvk string, count int) GVKMetric {
	return GVKMetric{
		GVK:      gvk,
		APIOp:    "update",
		ApplyOps: []Operation{{Name: "update", Count: count}},
		Watches:  "1",
	}
}

// ResourceDeleted encapsulates the expected metric data when a resource is deleted in
// Config Sync.
func ResourceDeleted(gvk string) GVKMetric {
	return GVKMetric{
		GVK:      gvk,
		APIOp:    "delete",
		ApplyOps: []Operation{{Name: "delete", Count: 1}},
		Watches:  "0",
	}
}

// ValidateReconcilerManagerMetrics validates the `reconcile_duration_seconds`
// metric from the reconciler manager.
func (csm ConfigSyncMetrics) ValidateReconcilerManagerMetrics() error {
	metric := ocmetrics.ReconcileDurationView.Name
	validation := hasTags(metric, []tag.Tag{
		{Key: ocmetrics.KeyStatus, Value: "success"},
	})
	return csm.validateMetric(metric, validation)
}

// ValidateReconcilerMetrics validates the non-error and non-GVK metrics produced
// by the reconcilers.
func (csm ConfigSyncMetrics) ValidateReconcilerMetrics(reconciler string, numResources int) error {
	// These metrics have non-deterministic values, so we just validate that the
	// metric exists for the correct reconciler and has a "success" status tag.
	metrics := []string{
		ocmetrics.ApplyDurationView.Name,
	}
	for _, m := range metrics {
		if err := csm.validateSuccessTag(reconciler, m); err != nil {
			return err
		}
	}
	return csm.ValidateDeclaredResources(reconciler, numResources)
}

// ValidateGVKMetrics validates all the metrics that have a GVK "type" tag key.
func (csm ConfigSyncMetrics) ValidateGVKMetrics(reconciler string, gvkMetric GVKMetric) error {
	if gvkMetric.APIOp != "" {
		if err := csm.validateAPICallDuration(reconciler, gvkMetric.APIOp, gvkMetric.GVK); err != nil {
			return err
		}
	}
	for _, applyOp := range gvkMetric.ApplyOps {
		if err := csm.validateApplyOperations(reconciler, applyOp.Name, gvkMetric.GVK, applyOp.Count); err != nil {
			return err
		}
	}
	return csm.validateRemediateDuration(reconciler, gvkMetric.GVK)
}

// ValidateMetricsCommitApplied checks that the `last_apply_timestamp` metric has been
// recorded for a particular commit hash.
func (csm ConfigSyncMetrics) ValidateMetricsCommitApplied(commitHash string) error {
	metric := ocmetrics.LastApplyTimestampView.Name
	validation := hasTags(metric, []tag.Tag{
		{Key: ocmetrics.KeyCommit, Value: commitHash},
	})

	for _, measurement := range csm[metric] {
		if validation(measurement) == nil {
			return nil
		}
	}

	return errors.Errorf("commit hash %s not found in config sync metrics", commitHash)
}

// ValidateErrorMetrics checks for the absence of all the error metrics except
// for the `reconciler_errors` metric. This metric is aggregated as a LastValue,
// so we check that the values are 0 instead.
func (csm ConfigSyncMetrics) ValidateErrorMetrics(reconciler string) error {
	metrics := []string{
		ocmetrics.ResourceFightsView.Name,
		ocmetrics.ResourceConflictsView.Name,
		ocmetrics.InternalErrorsView.Name,
	}
	for _, m := range metrics {
		if measurement, ok := csm[m]; ok {
			return errors.Errorf("validating error metrics: expected no error metrics but found %v: %+v", m, measurement)
		}
	}
	return csm.ValidateReconcilerErrors(reconciler, 0, 0)
}

// ValidateReconcilerErrors checks that the `reconciler_errors` metric is recorded
// for the correct reconciler with the expected values for each of its component tags.
func (csm ConfigSyncMetrics) ValidateReconcilerErrors(reconciler string, sourceValue, syncValue int) error {
	metric := ocmetrics.ReconcilerErrorsView.Name
	if _, ok := csm[metric]; ok {
		for _, measurement := range csm[metric] {
			// If the measurement has a "source" tag, validate the values match.
			if hasTags(metric, []tag.Tag{
				{Key: ocmetrics.KeyComponent, Value: "source"},
			})(measurement) == nil {
				if err := valueEquals(metric, sourceValue)(measurement); err != nil {
					return err
				}
			}
			// If the measurement has a "sync" tag, validate the values match.
			if hasTags(metric, []tag.Tag{
				{Key: ocmetrics.KeyComponent, Value: "sync"},
			})(measurement) == nil {
				if err := valueEquals(metric, syncValue)(measurement); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ValidateResourceOverrideCount checks that the `resource_override_count` metric is recorded
// for the correct reconciler, container name, and resource type, and checks the metric value is correct.
func (csm ConfigSyncMetrics) ValidateResourceOverrideCount(reconciler, containerName, resourceType string, count int) error {
	metric := ocmetrics.ResourceOverrideCountView.Name
	if _, ok := csm[metric]; ok {
		validations := []Validation{
			hasTags(metric, []tag.Tag{
				{Key: ocmetrics.KeyReconcilerType, Value: reconciler},
				{Key: ocmetrics.KeyContainer, Value: containerName},
				{Key: ocmetrics.KeyResourceType, Value: resourceType},
			}),
			valueEquals(metric, count),
		}
		return csm.validateMetric(metric, validations...)
	}
	return nil
}

// ValidateResourceOverrideCountMissingTags checks that the `resource_override_count` metric misses the specific the tags.
func (csm ConfigSyncMetrics) ValidateResourceOverrideCountMissingTags(tags []tag.Tag) error {
	metric := ocmetrics.ResourceOverrideCountView.Name
	if _, ok := csm[metric]; ok {
		validations := []Validation{
			missingTags(metric, tags),
		}
		return csm.validateMetric(metric, validations...)
	}
	return nil
}

// ValidateGitSyncDepthOverrideCount checks that the `git_sync_depth_override_count` metric has the correct value.
func (csm ConfigSyncMetrics) ValidateGitSyncDepthOverrideCount(count int) error {
	metric := ocmetrics.GitSyncDepthOverrideCountView.Name
	if _, ok := csm[metric]; ok {
		validations := []Validation{
			valueEquals(metric, count),
		}
		return csm.validateMetric(metric, validations...)
	}
	return nil
}

// ValidateNoSSLVerifyCount checks that the `no_ssl_verify_count` metric has the correct value.
func (csm ConfigSyncMetrics) ValidateNoSSLVerifyCount(count int) error {
	metric := ocmetrics.NoSSLVerifyCountView.Name
	if _, ok := csm[metric]; ok {
		validations := []Validation{
			valueEquals(metric, count),
		}
		return csm.validateMetric(metric, validations...)
	}
	return nil
}

// validateSuccessTag checks that the metric is recorded for the correct reconciler
// and has a "success" tag value.
func (csm ConfigSyncMetrics) validateSuccessTag(reconciler, metric string) error {
	validation := hasTags(metric, []tag.Tag{
		{Key: ocmetrics.KeyStatus, Value: "success"},
	})
	return csm.validateMetric(metric, validation)
}

// validateAPICallDuration checks that the `api_duration_seconds` metric is recorded
// and has the correct reconciler, operation, status, and type tags.
func (csm ConfigSyncMetrics) validateAPICallDuration(reconciler, operation, gvk string) error {
	metric := ocmetrics.APICallDurationView.Name
	validation := hasTags(metric, []tag.Tag{
		{Key: ocmetrics.KeyOperation, Value: operation},
		{Key: ocmetrics.KeyStatus, Value: "success"},
	})
	return errors.Wrapf(csm.validateMetric(metric, validation), "%s %s operation", gvk, operation)
}

// ValidateDeclaredResources checks that the declared_resources metric is recorded
// and has the expected value.
func (csm ConfigSyncMetrics) ValidateDeclaredResources(reconciler string, value int) error {
	metric := ocmetrics.DeclaredResourcesView.Name
	validations := []Validation{
		valueEquals(metric, value),
	}
	return csm.validateMetric(metric, validations...)
}

// validateApplyOperations checks that the `apply_operations` metric is recorded
// and has the correct reconciler, operation, status, and type tag values. Because
// controllers may fail and retry successfully, the recorded value of this metric may
// fluctuate, so we check that it is greater than or equal to the expected value.
func (csm ConfigSyncMetrics) validateApplyOperations(reconciler, operation, gvk string, value int) error {
	metric := ocmetrics.ApplyOperationsView.Name
	validations := []Validation{
		hasTags(metric, []tag.Tag{
			{Key: ocmetrics.KeyOperation, Value: operation},
			{Key: ocmetrics.KeyStatus, Value: "success"},
		}),
		valueGTE(metric, value),
	}
	return errors.Wrapf(csm.validateMetric(metric, validations...), "%s %s operation", gvk, operation)
}

// validateRemediateDuration checks that the `remediate_duration_seconds` metric
// is recorded and has the correct status and type tags.
func (csm ConfigSyncMetrics) validateRemediateDuration(reconciler, gvk string) error {
	metric := ocmetrics.RemediateDurationView.Name
	validations := []Validation{
		hasTags(metric, []tag.Tag{
			{Key: ocmetrics.KeyStatus, Value: "success"},
		}),
	}
	return errors.Wrap(csm.validateMetric(metric, validations...), gvk)
}

// validateMetric checks that at least one measurement from the metric passes all the validations.
func (csm ConfigSyncMetrics) validateMetric(name string, validations ...Validation) error {
	var errs status.MultiError
	allValidated := func(entry Measurement, vs []Validation) bool {
		for _, v := range vs {
			err := v(entry)
			if err != nil {
				errs = status.Append(errs, err)
				return false
			}
		}
		return true
	}

	if entries, ok := csm[name]; ok {
		for _, e := range entries {
			if allValidated(e, validations) {
				return nil
			}
		}
		return errors.Wrapf(errs, "validating metric %q", name)
	}
	return errors.Errorf("validating metric %q: metric not found", name)
}

// hasTags checks that the measurement contains all the expected tags.
func hasTags(name string, tags []tag.Tag) Validation {
	return func(metric Measurement) error {
		contains := func(tts []tag.Tag, t tag.Tag) bool {
			for _, tt := range tts {
				if tt == t {
					return true
				}
			}
			return false
		}

		for _, t := range tags {
			if !contains(metric.Tags, t) {
				return errors.Errorf("expected metric %q (tags: %v) to contain tag %v",
					name, metric.Tags, t)
			}
		}
		return nil
	}
}

// missingTags checks that the measurement misses all the specific tags.
func missingTags(name string, tags []tag.Tag) Validation {
	return func(metric Measurement) error {
		contains := func(tts []tag.Tag, t tag.Tag) bool {
			for _, tt := range tts {
				if tt == t {
					return true
				}
			}
			return false
		}

		for _, t := range tags {
			if contains(metric.Tags, t) {
				return errors.Errorf("expected metric %q (tags: %v) to not contain tag %v",
					name, metric.Tags, t)
			}
		}
		return nil
	}
}

// valueEquals checks that the measurement is recorded with the expected value.
func valueEquals(name string, value int) Validation {
	return func(metric Measurement) error {
		mv, err := strconv.Atoi(metric.Value)
		if err != nil {
			return err
		}
		if !cmp.Equal(mv, value) {
			return errors.Errorf("expected metric %q (tags: %v) to equal %v but got %v",
				name, metric.Tags, value, metric.Value)
		}
		return nil
	}
}

// valueGTE checks that the measurement value is greater than or equal to the expected value.
func valueGTE(name string, value int) Validation {
	return func(metric Measurement) error {
		mv, err := strconv.Atoi(metric.Value)
		if err != nil {
			return err
		}
		if mv < value {
			return errors.Errorf("expected metric %q (tags: %v) to be greater than or equal to %v but got %v",
				name, metric.Tags, value, mv)
		}
		return nil
	}
}
