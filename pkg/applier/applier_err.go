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
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// ErrorForResourceWithResource returns an Error that indicates that
// the applier failed to apply the given resource and includes the resource itself
func ErrorForResourceWithResource(err error, id core.ID, resource client.Object) status.Error {
	return applierErrorBuilder.Sprintf("failed to apply %v", id).Wrap(err).BuildWithResources(resource)
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
func largeResourceGroupError(_ error, id core.ID) status.Error {
	// the error is not used because it's confusing to the user.  The current core error
	// says: Request entity too large: limit is 3145728 (eg)
	// the actual size is 1.5mb, so it is a bit mislreading.
	// Possibly in the future if the error changes we will start passing it back to the user.
	e := fmt.Errorf("the size of the ResourceGroup object for this repository is exceeding the row size. "+
		"To mitigate follow instructions here https://cloud.google.com/anthos-config-management/docs/how-to/breaking-up-repo. "+
		": %v", id)
	return applierErrorBuilder.Wrap(e).Build()
}
