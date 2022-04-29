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

package id

// DisallowedField is the json path of a field that is not allowed in imported objects.
type DisallowedField string

const (
	// OwnerReference represents the ownerReference field in a metav1.Object.
	OwnerReference DisallowedField = "metadata.ownerReference"
	// SelfLink represents the selfLink field in a metav1.Object.
	SelfLink DisallowedField = "metadata.selfLink"
	// UID represents the uid field in a metav1.Object.
	UID DisallowedField = "metadata.uid"
	// ResourceVersion represents the resourceVersion field in a metav1.Object.
	ResourceVersion DisallowedField = "metadata.resourceVersion"
	// Generation represents the generation field in a metav1.Object.
	Generation DisallowedField = "metadata.generation"
	// CreationTimestamp represents the creationTimestamp field in a metav1.Object.
	CreationTimestamp DisallowedField = "metadata.creationTimestamp"
	// DeletionTimestamp represents the deletionTimestamp field in a metav1.Object.
	DeletionTimestamp DisallowedField = "metadata.deletionTimestamp"
	// DeletionGracePeriodSeconds represents the deletionGracePeriodSeconds field in a metav1.Object.
	DeletionGracePeriodSeconds DisallowedField = "metadata.deletionGracePeriodSeconds"
	// Status represents the status field in a client.Object.
	Status DisallowedField = "status"
)
