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

package e2e

import (
	"fmt"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMultipleVersions_CustomResourceV1(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation1)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Add the Anvil CRD.
	crdObj := anvilV1CRD()
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crdObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding Anvil CRD"))
	nt.Must(nt.WatchForAllSyncs())
	nt.RenewClient()

	// Add the v1 Anvils and verify they are created.
	nsObj := k8sobjects.NamespaceObject("foo")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	anvilv1Obj := anvilCR("v1", "first", 10)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/anvilv1.yaml", anvilv1Obj))
	anvilv2Obj := anvilCR("v2", "second", 100)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/anvilv2.yaml", anvilv2Obj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding v1 and v2 Anvil CRs"))
	nt.Must(nt.WatchForAllSyncs())

	err := nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Modify the v1 Anvils and verify they are updated.
	anvilv1Obj = anvilCR("v1", "first", 20)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/anvilv1.yaml", anvilv1Obj))
	anvilv2Obj = anvilCR("v2", "second", 200)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/anvilv2.yaml", anvilv2Obj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Modifying v1 and v2 Anvil CRs"))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, crdObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, anvilv1Obj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, anvilv2Obj)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func anvilV1CRD() *apiextensionsv1.CustomResourceDefinition {
	crd := k8sobjects.CustomResourceDefinitionV1Object(core.Name("anvils.acme.com"))
	crd.Spec.Group = "acme.com"
	crd.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Plural:   "anvils",
		Singular: "anvil",
		Kind:     "Anvil",
	}
	crd.Spec.Scope = apiextensionsv1.NamespaceScoped
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
		{
			Name:    "v1",
			Served:  true,
			Storage: false,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type:     "object",
							Required: []string{"lbs"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"lbs": {
									Type:    "integer",
									Minimum: ptr.To(1.0),
									Maximum: ptr.To(9000.0),
								},
							},
						},
					},
				},
			},
		},
		{
			Name:    "v2",
			Served:  true,
			Storage: true,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type:     "object",
							Required: []string{"lbs"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"lbs": {
									Type:    "integer",
									Minimum: ptr.To(1.0),
									Maximum: ptr.To(9000.0),
								},
							},
						},
					},
				},
			},
		},
	}
	return crd
}

func anvilCR(version, name string, weight int64) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(anvilGVK(version))
	if name != "" {
		u.SetName(name)
	}
	if weight != 0 {
		u.Object["spec"] = map[string]interface{}{
			"lbs": weight,
		}
	}
	return u
}

func anvilGVK(version string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "acme.com",
		Version: version,
		Kind:    "Anvil",
	}
}

func TestMultipleVersions_RoleBinding(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation1)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rbV1 := k8sobjects.RoleBindingObject(core.Name("v1user"))
	rbV1.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     "acme-admin",
	}
	rbV1.Subjects = append(rbV1.Subjects, rbacv1.Subject{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     "v1user@acme.com",
	})

	// Add the v1 RoleBinding and verify it is created.
	nsObj := k8sobjects.NamespaceObject("foo")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/rbv1.yaml", rbV1))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding v1 RoleBinding"))
	nt.Must(nt.WatchForAllSyncs())

	err := nt.Validate("v1user", "foo", &rbacv1.RoleBinding{},
		hasV1Subjects("v1user@acme.com"))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rbV1)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Modify the v1 RoleBinding and verify it is updated.
	rbV1.Subjects = append(rbV1.Subjects, rbacv1.Subject{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     "v1admin@acme.com",
	})

	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/rbv1.yaml", rbV1))
	nt.Must(rootSyncGitRepo.CommitAndPush("Modifying v1 RoleBinding"))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Validate("v1user", "foo", &rbacv1.RoleBinding{},
		hasV1Subjects("v1user@acme.com", "v1admin@acme.com"))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rbV1)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the v1 RoleBinding and verify that it is also deleted.
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/foo/rbv1.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Removing v1 RoleBinding"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.ValidateNotFound("v1user", "foo", &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rbV1)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func hasV1Subjects(subjects ...string) func(o client.Object) error {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		r, ok := o.(*rbacv1.RoleBinding)
		if !ok {
			return testpredicates.WrongTypeErr(o, r)
		}
		if len(r.Subjects) != len(subjects) {
			return fmt.Errorf("want %v subjects; got %v: %w", subjects, r.Subjects, testpredicates.ErrFailedPredicate)
		}

		found := make(map[string]bool)
		for _, subj := range r.Subjects {
			found[subj.Name] = true
		}
		for _, name := range subjects {
			if !found[name] {
				return fmt.Errorf("missing subject %q: %w", name, testpredicates.ErrFailedPredicate)
			}
		}

		return nil
	}
}
