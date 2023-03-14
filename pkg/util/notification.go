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

package util

import (
	"kpt.dev/configsync/pkg/api/configsync"
)

const (
	// NotificationAPIGroup is the env var name for api group
	NotificationAPIGroup = "NOTIFICATION_API_GROUP"
	// NotificationAPIVersion is the env var name for api version
	NotificationAPIVersion = "NOTIFICATION_API_VERSION"
	// NotificationAPIKind is the env var name for api kind
	NotificationAPIKind = "NOTIFICATION_API_KIND"
	// NotificationResourceName is the env var name for resource name
	NotificationResourceName = "NOTIFICATION_RESOURCE_NAME"
	// NotificationResourceNamespace is the env var name for resource namespace
	NotificationResourceNamespace = "NOTIFICATION_RESOURCE_NAMESPACE"
	// NotificationResyncPeriod is the env var name for resync period
	NotificationResyncPeriod = "NOTIFICATION_RESYNC_PERIOD"
	// NotificationConfigMapName is the env var name for configmap name
	NotificationConfigMapName = "NOTIFICATION_CONFIGMAP_NAME"
	// NotificationSecretName is the env var name for secret name
	NotificationSecretName = "NOTIFICATION_SECRET_NAME"

	// AnnotationsPrefix is the notification annotations prefix
	AnnotationsPrefix = "notifications." + configsync.GroupName
)
