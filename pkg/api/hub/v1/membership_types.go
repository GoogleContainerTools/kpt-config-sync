/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MembershipSpec is the specification of the Membership.
type MembershipSpec struct {
	Owner                MembershipOwner `json:"owner,omitempty"`
	WorkloadIdentityPool string          `json:"workload_identity_pool,omitempty"`
	IdentityProvider     string          `json:"identity_provider,omitempty"`
}

// MembershipOwner specifies the owner ID of the membership.
type MembershipOwner struct {
	ID string `json:"id,omitempty"`
}

//+kubebuilder:object:root=true

// Membership is the Schema for the memberships API
type Membership struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec MembershipSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// MembershipList contains a list of Membership
type MembershipList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Membership `json:"items"`
}
