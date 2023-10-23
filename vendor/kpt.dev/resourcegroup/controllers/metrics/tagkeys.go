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
	"go.opencensus.io/tag"
)

var (
	// KeyStallReason groups metrics by the stall condition reason field
	KeyStallReason, _ = tag.NewKey("stallreason")

	// KeyOperation groups metrics by their operation. Possible values: create, patch, update, delete.
	KeyOperation, _ = tag.NewKey("operation")

	// KeyErrorCode groups metrics by their error code.
	KeyErrorCode, _ = tag.NewKey("errorcode")

	// KeyType groups metrics by their resource reconciler type. Possible values: root-sync, repo-sync
	KeyType, _ = tag.NewKey("reconciler")

	// KeyResourceGroup groups metrics by their resource group
	KeyResourceGroup, _ = tag.NewKey("resourcegroup")

	// KeyName groups metrics by their name of reconciler.
	KeyName, _ = tag.NewKey("name")

	// KeyComponent groups metrics by their component. Possible value: readiness
	KeyComponent, _ = tag.NewKey("component")

	// ResourceKeyDeploymentName groups metrics by k8s deployment name.
	// This metric tag is populated from the k8s.deployment.name resource
	// attribute for Prometheus using the resource_to_telemetry_conversion feature.
	ResourceKeyDeploymentName, _ = tag.NewKey("k8s_deployment_name")
)
