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

package kustomizecomponents

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidateAllTenants validates all DRY resources under
// '../testdata/hydration/kustomize-components' are reconciled.
func ValidateAllTenants(nt *nomostest.NT, reconcilerScope, baseRelPath string, tenants ...string) {
	nt.T.Logf("Validate resources are synced for all tenants %s", tenants)
	ValidateNamespaces(nt, tenants, fmt.Sprintf("path: %s/namespace.yaml\n", baseRelPath))
	for _, tenant := range tenants {
		ValidateTenant(nt, reconcilerScope, tenant, baseRelPath)
	}
}

// ValidateTenant validates the provided tenant resources under
// '../testdata/hydration/kustomize-components' are reconciled.
func ValidateTenant(nt *nomostest.NT, reconcilerScope, tenant, baseRelPath string) {
	nt.T.Logf("Validate %s resources are created and managed by %s", tenant, reconcilerScope)
	if declared.Scope(reconcilerScope) == declared.RootReconciler {
		// Only validate Namespace for root reconciler because namespace reconciler can't manage Namespaces.
		if err := nt.Validate(tenant, "", &corev1.Namespace{},
			testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
			nt.T.Error(err)
		}
	}
	if err := nt.Validate("deny-all", tenant, &networkingv1.NetworkPolicy{},
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, fmt.Sprintf("path: %s/networkpolicy.yaml\n", baseRelPath)),
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("tenant-admin", tenant, &rbacv1.Role{},
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, fmt.Sprintf("path: %s/role.yaml\n", baseRelPath)),
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("tenant-admin-rolebinding", tenant, &rbacv1.RoleBinding{},
		testpredicates.HasAnnotation(metadata.KustomizeOrigin, fmt.Sprintf("path: %s/rolebinding.yaml\n", baseRelPath)),
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, reconcilerScope)); err != nil {
		nt.T.Error(err)
	}
}

// ValidateNamespaces validates all namespaces under
// '../testdata/hydration/kustomize-components' are reconciled and have the
// kustomize annotations.
func ValidateNamespaces(nt *nomostest.NT, expectedNamespaces []string, expectedOrigin string) {
	namespaces := &corev1.NamespaceList{}
	var testLabels = client.MatchingLabels{"test-case": "hydration"}
	if err := nt.KubeClient.List(namespaces, testLabels); err != nil {
		nt.T.Error(err)
	}
	var actualNamespaces []string
	for _, ns := range namespaces.Items {
		if ns.Status.Phase == corev1.NamespaceActive {
			actualNamespaces = append(actualNamespaces, ns.Name)
		}
		origin, ok := ns.Annotations[metadata.KustomizeOrigin]
		if !ok {
			nt.T.Errorf("expected annotation[%q], but not found", metadata.KustomizeOrigin)
		}
		if origin != expectedOrigin {
			nt.T.Errorf("expected annotation[%q] to be %q, but got '%s'", metadata.KustomizeOrigin, expectedOrigin, origin)
		}
	}
	if !reflect.DeepEqual(actualNamespaces, expectedNamespaces) {
		nt.T.Errorf("expected namespaces: %v, but got: %v", expectedNamespaces, actualNamespaces)
	}
}
