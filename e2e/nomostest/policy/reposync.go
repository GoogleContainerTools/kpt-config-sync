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

package policy

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/pkg/api/configsync"
)

// RBACAdmin returns a PolicyRule that grants admin for rbacv1 APIGroup
func RBACAdmin() rbacv1.PolicyRule {
	policy := rbacv1.PolicyRule{
		APIGroups: []string{rbacv1.GroupName},
		Resources: []string{rbacv1.ResourceAll},
		Verbs:     []string{rbacv1.VerbAll},
	}
	return policy
}

// CoreAdmin returns a PolicyRule that grants admin for corev1 APIGroup
func CoreAdmin() rbacv1.PolicyRule {
	policy := rbacv1.PolicyRule{
		APIGroups: []string{corev1.GroupName},
		Resources: []string{rbacv1.ResourceAll},
		Verbs:     []string{rbacv1.VerbAll},
	}
	return policy
}

// RepoSyncAdmin returns a PolicyRule that admin for RepoSync objects
func RepoSyncAdmin() rbacv1.PolicyRule {
	policy := rbacv1.PolicyRule{
		APIGroups: []string{configsync.GroupName},
		Resources: []string{"reposyncs"},
		Verbs:     []string{rbacv1.VerbAll},
	}
	return policy
}

// AllAdmin returns a PolicyRule that grants Admin for all APIGroups
func AllAdmin() rbacv1.PolicyRule {
	policy := rbacv1.PolicyRule{
		APIGroups: []string{rbacv1.APIGroupAll},
		Resources: []string{rbacv1.ResourceAll},
		Verbs:     []string{rbacv1.VerbAll},
	}
	return policy
}
