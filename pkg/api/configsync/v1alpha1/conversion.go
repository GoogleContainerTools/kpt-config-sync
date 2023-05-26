// Copyright 2023 Google LLC
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

package v1alpha1

import (
	conversion "k8s.io/apimachinery/pkg/conversion"
	v1beta1 "kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// Convert_v1beta1_HelmRootSync_To_v1alpha1_HelmRootSync converts HelmRootSync
// from v1beta1 to v1alpha1.
//
// This conversion is manual, because it is lossy.
// The `DeployNamespace` field is in v1beta1, but not v1alpha1.
// This is fine. New versions are allowed to add new fields.
// The newer version is used for storage.
//
//nolint:revive // name underscores required by conversion-gen
func Convert_v1beta1_HelmRootSync_To_v1alpha1_HelmRootSync(in *v1beta1.HelmRootSync, out *HelmRootSync, s conversion.Scope) error {
	return autoConvert_v1beta1_HelmRootSync_To_v1alpha1_HelmRootSync(in, out, s)
}
