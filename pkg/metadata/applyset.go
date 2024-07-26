// Copyright 2024 Google LLC
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

package metadata

import (
	kubectlapply "k8s.io/kubectl/pkg/cmd/apply"
	"kpt.dev/configsync/pkg/api/configsync"
)

// Labels with the `applyset.kubernetes.io/` prefix.
const (
	// ApplySetPartOfLabel is the key of the label which indicates that the
	// object is a member of an ApplySet. The value of the label MUST match the
	// value of ApplySetParentIDLabel on the parent object.
	ApplySetPartOfLabel = kubectlapply.ApplysetPartOfLabel

	// ApplySetParentIDLabel is the key of the label that makes object an
	// ApplySet parent object. Its value MUST use the format specified in
	// k8s.io/kubectl/pkg/cmd/apply.V1ApplySetIdFormat.
	ApplySetParentIDLabel = kubectlapply.ApplySetParentIDLabel
)

// Annotations with the `applyset.kubernetes.io/` prefix.
const (
	// ApplySetToolingAnnotation is the key of the label that indicates which
	// tool is used to manage this ApplySet. Tooling should refuse to mutate
	// ApplySets belonging to other tools. The value must be in the format
	// <toolname>/<semver>. Example value: "kubectl/v1.27" or "helm/v3" or
	// "kpt/v1.0.0"
	ApplySetToolingAnnotation = kubectlapply.ApplySetToolingAnnotation

	// ApplySetToolingName is the name used to represent Config Sync in the
	// ApplySet tooling annotation.
	ApplySetToolingName = configsync.GroupName

	// ApplySetToolingVersion is the version used to represent Config Sync in
	// the ApplySet tooling annotation.
	//
	// The ApplySetKEP and kubectl require this to be a semantic version,
	// implying that it should be the version of the tool. But we're using a
	// static version instead, to allow listing all objects managed Config Sync,
	// regardless of version.
	ApplySetToolingVersion = "v1"
)
