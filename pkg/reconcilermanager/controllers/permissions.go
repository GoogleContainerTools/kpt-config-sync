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

package controllers

import rbacv1 "k8s.io/api/rbac/v1"

// rolereference returns an intialized Role with apigroup, kind and name.
func rolereference(name, kind string) rbacv1.RoleRef {
	return rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     kind,
		Name:     name,
	}
}

// subject returns and initialized Subject with kind, name and namespace.
func subject(name, namespace, kind string) rbacv1.Subject {
	return rbacv1.Subject{
		Kind:      kind,
		Name:      name,
		Namespace: namespace,
	}
}
