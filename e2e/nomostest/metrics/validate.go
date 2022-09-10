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
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
)

// ConfigSyncMetrics is a map from metric names to its measurements.
type ConfigSyncMetrics map[string][]Measurement

// Measurement is a recorded data point with a list of tags and a value.
type Measurement struct {
	Tags  []Tag
	Value string
}

// TagMap returns a new map of tag keys to values.
func (m Measurement) TagMap() map[string]string {
	tagMap := make(map[string]string, len(m.Tags))
	for _, tag := range m.Tags {
		tagMap[tag.Key] = tag.Value
	}
	return tagMap
}

// Tag is a replacement for OpenCensus Tag, which has a private name field,
// which makes it hard to test and print.
type Tag struct {
	Key   string
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

// FilterByReconciler returns a new ConfigSyncMetrics with only the metrics from
// the specified reconciler, filtered using tag values.
func (csm ConfigSyncMetrics) FilterByReconciler(reconcilerName string) ConfigSyncMetrics {
	filteredMetrics := ConfigSyncMetrics{}
	for mName, mList := range csm {
		validator := hasReconcilerNameTag(mName, reconcilerName)
		var filteredList []Measurement
		for _, m := range mList {
			if validator(m) == nil {
				filteredList = append(filteredList, m)
			}
		}
		if len(filteredList) > 0 {
			filteredMetrics[mName] = filteredList
		}
	}
	return filteredMetrics
}

// ValidateReconcilerManagerMetrics validates the `reconcile_duration_seconds`
// metric from the reconciler manager.
func (csm ConfigSyncMetrics) ValidateReconcilerManagerMetrics() error {
	metric := ocmetrics.ReconcileDurationView.Name
	validation := hasTags(metric, []Tag{
		{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
	})
	return csm.validateMetric(metric, validation)
}

// ValidateReconcilerMetrics validates the non-error and non-GVK metrics produced
// by the reconcilers.
func (csm ConfigSyncMetrics) ValidateReconcilerMetrics(reconcilerName string, numResources int) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	// These metrics have non-deterministic values, so we just validate that the
	// metric exists for the correct reconciler with the status=success tag.
	metrics := []string{
		ocmetrics.ApplyDurationView.Name,
		ocmetrics.LastApplyTimestampView.Name,
		ocmetrics.LastSyncTimestampView.Name,
	}
	for _, m := range metrics {
		if err := reconcilerMetrics.validateSuccessTag(m); err != nil {
			return err
		}
	}
	return errors.Wrapf(reconcilerMetrics.ValidateDeclaredResources(numResources), "for reconciler %s", reconcilerName)
}

// ValidateGVKMetrics validates all the metrics that have a GVK "type" tag key.
func (csm ConfigSyncMetrics) ValidateGVKMetrics(reconcilerName string, gvkMetric GVKMetric) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	if gvkMetric.APIOp != "" {
		if err := reconcilerMetrics.validateAPICallDuration(gvkMetric.APIOp, gvkMetric.GVK); err != nil {
			return err
		}
	}
	for _, applyOp := range gvkMetric.ApplyOps {
		if err := reconcilerMetrics.validateApplyOperations(applyOp.Name, gvkMetric.GVK, applyOp.Count); err != nil {
			return err
		}
	}
	return errors.Wrapf(reconcilerMetrics.validateRemediateDuration(gvkMetric.GVK), "for reconciler %s", reconcilerName)
}

// ValidateMetricsCommitSynced checks that the `last_sync_timestamp` metric has
// been recorded for a particular commit hash.
func (csm ConfigSyncMetrics) ValidateMetricsCommitSynced(reconcilerName, commitHash string) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	metric := ocmetrics.LastSyncTimestampView.Name
	validation := hasTags(metric, []Tag{
		{Key: ocmetrics.KeyCommit.Name(), Value: commitHash},
	})

	for _, measurement := range reconcilerMetrics[metric] {
		if validation(measurement) == nil {
			return nil
		}
	}

	return errors.Errorf("commit hash %s not found in config sync metrics for reconciler %s", commitHash, reconcilerName)
}

// ValidateMetricsCommitSyncedWithSuccess checks that the `last_sync_timestamp`
// metric has been recorded for a particular commit hash with status=success.
func (csm ConfigSyncMetrics) ValidateMetricsCommitSyncedWithSuccess(reconcilerName, commitHash string) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	metric := ocmetrics.LastSyncTimestampView.Name
	validation := hasTags(metric, []Tag{
		{Key: ocmetrics.KeyCommit.Name(), Value: commitHash},
		{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
	})

	for _, measurement := range reconcilerMetrics[metric] {
		if validation(measurement) == nil {
			return nil
		}
	}

	return errors.Errorf("commit hash %s with success status not found in config sync metrics for reconciler %s", commitHash, reconcilerName)
}

// ValidateErrorMetrics checks for the absence of all the error metrics except
// for the `reconciler_errors` metric. This metric is aggregated as a LastValue,
// so we check that the values are 0 instead.
func (csm ConfigSyncMetrics) ValidateErrorMetrics(reconcilerName string) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	metrics := []string{
		ocmetrics.ResourceFightsView.Name,
		// TODO: (b/236191762) Re-enable the validation for the resource_conflicts error
		// Disable it for now because this is a cumulative metric. It is triggered
		// when the remediator is fighting with CRD garbage collector.
		//ocmetrics.ResourceConflictsView.Name,
		ocmetrics.InternalErrorsView.Name,
	}
	for _, m := range metrics {
		if measurement, ok := reconcilerMetrics[m]; ok {
			return errors.Errorf("validating error metrics: expected no error metrics for reconciler %s but found %v: %+v", reconcilerName, m, measurement)
		}
	}
	return errors.Wrapf(reconcilerMetrics.ValidateReconcilerErrors(reconcilerName, 0, 0), "for reconciler %s", reconcilerName)
}

// ValidateReconcilerErrors checks that the `reconciler_errors` metric is recorded
// for the correct reconciler with the expected values for each of its component tags.
func (csm ConfigSyncMetrics) ValidateReconcilerErrors(reconcilerName string, sourceValue, syncValue int) error {
	reconcilerMetrics := csm.FilterByReconciler(reconcilerName)
	metric := ocmetrics.ReconcilerErrorsView.Name
	if _, ok := reconcilerMetrics[metric]; !ok {
		return nil
	}
	for _, measurement := range reconcilerMetrics[metric] {
		// If the measurement has a "source" tag, validate the values match.
		if hasTags(metric, []Tag{
			{Key: ocmetrics.KeyComponent.Name(), Value: "source"},
		})(measurement) == nil {
			if err := valueEquals(metric, sourceValue)(measurement); err != nil {
				return errors.Wrapf(err, "for reconciler %s", reconcilerName)
			}
		}
		// If the measurement has a "sync" tag, validate the values match.
		if hasTags(metric, []Tag{
			{Key: ocmetrics.KeyComponent.Name(), Value: "sync"},
		})(measurement) == nil {
			if err := valueEquals(metric, syncValue)(measurement); err != nil {
				return errors.Wrapf(err, "for reconciler %s", reconcilerName)
			}
		}
	}
	return nil
}

// ValidateResourceOverrideCount checks that the `resource_override_count` metric is recorded
// for the correct reconciler, container name, and resource type, and checks the metric value is correct.
func (csm ConfigSyncMetrics) ValidateResourceOverrideCount(reconcilerType, containerName, resourceType string, count int) error {
	metric := ocmetrics.ResourceOverrideCountView.Name
	if _, ok := csm[metric]; !ok {
		return nil
	}
	validations := []Validation{
		hasTags(metric, []Tag{
			{Key: ocmetrics.KeyReconcilerType.Name(), Value: reconcilerType},
			{Key: ocmetrics.KeyContainer.Name(), Value: containerName},
			{Key: ocmetrics.KeyResourceType.Name(), Value: resourceType},
		}),
		valueEquals(metric, count),
	}
	return csm.validateMetric(metric, validations...)
}

// ValidateResourceOverrideCountMissingTags checks that the `resource_override_count` metric misses the specific the tags.
func (csm ConfigSyncMetrics) ValidateResourceOverrideCountMissingTags(tags []Tag) error {
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

// validateSuccessTag checks that the metric has the status=success tag.
func (csm ConfigSyncMetrics) validateSuccessTag(metric string) error {
	validation := hasTags(metric, []Tag{
		{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
	})
	return csm.validateMetric(metric, validation)
}

// validateAPICallDuration checks that the `api_duration_seconds` metric is recorded
// and has the correct operation, status, and type tags.
func (csm ConfigSyncMetrics) validateAPICallDuration(operation, gvk string) error {
	metric := ocmetrics.APICallDurationView.Name
	validation := hasTags(metric, []Tag{
		{Key: ocmetrics.KeyOperation.Name(), Value: operation},
		{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
	})
	return errors.Wrapf(csm.validateMetric(metric, validation), "%s %s operation", gvk, operation)
}

// ValidateDeclaredResources checks that the declared_resources metric is recorded
// and has the expected value.
func (csm ConfigSyncMetrics) ValidateDeclaredResources(value int) error {
	metric := ocmetrics.DeclaredResourcesView.Name
	validations := []Validation{
		valueEquals(metric, value),
	}
	return csm.validateMetric(metric, validations...)
}

// validateApplyOperations checks that the `apply_operations` metric is recorded
// and has the correct operation, status, and type tag values. Because
// controllers may fail and retry successfully, the recorded value of this metric may
// fluctuate, so we check that it is greater than or equal to the expected value.
func (csm ConfigSyncMetrics) validateApplyOperations(operation, gvk string, value int) error {
	metric := ocmetrics.ApplyOperationsView.Name
	validations := []Validation{
		hasTags(metric, []Tag{
			{Key: ocmetrics.KeyOperation.Name(), Value: operation},
			{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
		}),
		valueGTE(metric, value),
	}
	return errors.Wrapf(csm.validateMetric(metric, validations...), "%s %s operation", gvk, operation)
}

// validateRemediateDuration checks that the `remediate_duration_seconds` metric
// is recorded and has the correct status and type tags.
func (csm ConfigSyncMetrics) validateRemediateDuration(gvk string) error {
	metric := ocmetrics.RemediateDurationView.Name
	validations := []Validation{
		hasTags(metric, []Tag{
			{Key: ocmetrics.KeyStatus.Name(), Value: ocmetrics.StatusSuccess},
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

// hasReconcilerNameTag checks that the measurement contains all the tags
// expected from the reconciler deployment by name.
func hasReconcilerNameTag(metricName, reconcilerName string) Validation {
	tags := []Tag{
		{Key: ocmetrics.ResourceKeyDeploymentName.Name(), Value: reconcilerName},
	}
	return hasTags(metricName, tags)
}

// hasTags checks that the measurement contains all the expected tags.
func hasTags(name string, tags []Tag) Validation {
	return func(metric Measurement) error {
		contains := func(tts []Tag, t Tag) bool {
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
func missingTags(name string, tags []Tag) Validation {
	return func(metric Measurement) error {
		contains := func(tts []Tag, t Tag) bool {
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
