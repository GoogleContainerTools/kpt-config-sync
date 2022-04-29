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

package bugreport

import (
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/policycontroller"
)

// Product describes an ACM Product
type Product string

const (
	// PolicyController policy controller
	PolicyController = Product("Policy Controller")
	// ConfigSync config sync, AKA Nomos, AKA original ACM
	ConfigSync = Product("Config Sync")
	// ConfigSyncMonitoring controller
	ConfigSyncMonitoring = Product("Config Sync Monitoring")
	// ResourceGroup controller
	ResourceGroup = Product("Resource Group")
)

// Resource Group constants
const (
	// RGControllerNamespace is the namespace used for the resource-group controller
	RGControllerNamespace = "resource-group-system"
)

var (
	productNamespaces = map[Product]string{
		PolicyController:     policycontroller.NamespaceSystem,
		ConfigSync:           configmanagement.ControllerNamespace,
		ResourceGroup:        RGControllerNamespace,
		ConfigSyncMonitoring: metrics.MonitoringNamespace,
	}
)
