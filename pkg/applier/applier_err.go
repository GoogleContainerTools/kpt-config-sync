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
	"fmt"
	"strings"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
)

// ApplierErrorCode is the error code for apply failures.
const ApplierErrorCode = "2009"

var applierErrorBuilder = status.NewErrorBuilder(ApplierErrorCode)

// Error indicates that the applier failed to apply some resources.
func Error(err error) status.Error {
	return applierErrorBuilder.Wrap(err).Build()
}

// ErrorForResource indicates that the applier failed to apply
// the given resource.
func ErrorForResource(err error, id core.ID) status.Error {
	return applierErrorBuilder.Wrap(fmt.Errorf("failed to apply %v: %w", id, err)).Build()
}

// PruneErrorForResource indicates that the applier failed to prune
// the given resource.
func PruneErrorForResource(err error, id core.ID) status.Error {
	return applierErrorBuilder.Wrap(fmt.Errorf("failed to prune %v: %w", id, err)).Build()
}

// DeleteErrorForResource indicates that the applier failed to delete
// the given resource.
func DeleteErrorForResource(err error, id core.ID) status.Error {
	return applierErrorBuilder.Wrap(fmt.Errorf("failed to delete %v: %w", id, err)).Build()
}

// WaitErrorForResource indicates that the applier failed to wait for
// the given resource.
func WaitErrorForResource(err error, id core.ID) status.Error {
	return applierErrorBuilder.Wrap(fmt.Errorf("failed to wait for %v: %w", id, err)).Build()
}

// SkipErrorForResource indicates that the applier skipped apply or delete of
// the given resource.
func SkipErrorForResource(err error, id core.ID, strategy actuation.ActuationStrategy) status.Error {
	return applierErrorBuilder.Wrap(fmt.Errorf("skipped %s of %v: %w",
		strings.ToLower(strategy.String()), id, err)).Build()
}

// largeResourceGroupError indicates that the source repo has too many objects
// to manage with a single resource group.
func largeResourceGroupError(err error, id core.ID) status.Error {
	e := fmt.Errorf("source repository contains too many resource objects to inventory with a single ResourceGroup: "+
		"inventory apply failed: %v: %s. "+
		"To mitigate, spread the source objects across multiple repositories. "+
		"For how, see https://cloud.google.com/anthos-config-management/docs/how-to/breaking-up-repo",
		id, err)
	return applierErrorBuilder.Wrap(e).Build()
}
