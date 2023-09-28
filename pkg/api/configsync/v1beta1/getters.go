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

package v1beta1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/client/restconfig"
)

// GetPeriod returns the sync period defaulting to the provided defaultPeriod if empty.
func GetPeriod(period metav1.Duration, defaultPeriod time.Duration) time.Duration {
	if period.Duration == 0 {
		return defaultPeriod
	}
	return period.Duration
}

// GetSecretName will return an empty string if the secretRef.name is
// empty or the secretRef doesn't exist
func GetSecretName(secretRef *SecretReference) string {
	if secretRef != nil {
		return secretRef.Name
	}
	return ""
}

// SafeOverride creates an override or returns an existing one
// use it if you need to ensure that you are assigning
// to an object, but not to test for nil (current existence)
func (rs *RepoSyncSpec) SafeOverride() *RepoSyncOverrideSpec {
	if rs.Override == nil {
		rs.Override = &RepoSyncOverrideSpec{}
	}
	return rs.Override
}

// SafeOverride creates an override or returns an existing one
// use it if you need to ensure that you are assigning
// to an object, but not to test for nil (current existence)
func (rs *RootSyncSpec) SafeOverride() *RootSyncOverrideSpec {
	if rs.Override == nil {
		rs.Override = &RootSyncOverrideSpec{}
	}
	return rs.Override
}

// GetReconcileTimeout returns reconcile timeout in string, defaulting to 5m if empty
func GetReconcileTimeout(d *metav1.Duration) string {
	if d == nil || d.Duration == 0 {
		return configsync.DefaultReconcileTimeout.String()
	}
	return d.Duration.String()
}

// GetAPIServerTimeout returns the API server timeout in string, defaulting to 15s if empty
func GetAPIServerTimeout(d *metav1.Duration) string {
	if d == nil || d.Duration == 0 {
		return restconfig.DefaultTimeout.String()
	}
	return d.Duration.String()
}
