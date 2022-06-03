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
	"testing"

	"github.com/pkg/errors"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMultipleVersions_CustomResourceV1Beta1(t *testing.T) {
	nt := nomostest.New(t)
	support, err := nt.SupportV1Beta1CRD()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}
	// Skip this test when v1beta1 CRD is not supported in the testing cluster.
	if !support {
		return
	}

	// Add the Anvil CRD.
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", anvilV1Beta1CRD())
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CRD")
	nt.WaitForRepoSyncs()
	nt.RenewClient()

	// Add the v1 and v1beta1 Anvils and verify they are created.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv1.yaml", anvilCR("v1", "first", 10))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv2.yaml", anvilCR("v2", "second", 100))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding v1 and v2 Anvil CRs")
	nt.WaitForRepoSyncs()

	err = nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 5,
			metrics.ResourceCreated("CustomResourceDefinition"), metrics.ResourceCreated("Namespace"),
			metrics.GVKMetric{
				GVK:   "Anvil",
				APIOp: "update",
				ApplyOps: []metrics.Operation{
					{Name: "update", Count: 2},
				},
				Watches: "2",
			})
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Modify the v1 and v1beta1 Anvils and verify they are updated.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv1.yaml", anvilCR("v1", "first", 20))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv2.yaml", anvilCR("v2", "second", 200))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modifying v1 and v2 Anvil CRs")
	nt.WaitForRepoSyncs()

	err = nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 5,
			metrics.ResourcePatched("Namespace", 2),
			metrics.GVKMetric{
				GVK:   "Anvil",
				APIOp: "update",
				ApplyOps: []metrics.Operation{
					{Name: "update", Count: 4},
				},
				Watches: "2",
			})
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func anvilV1Beta1CRD() *apiextensionsv1beta1.CustomResourceDefinition {
	crd := fake.CustomResourceDefinitionV1Beta1Object(core.Name("anvils.acme.com"))
	crd.Spec.Group = "acme.com"
	crd.Spec.Names = apiextensionsv1beta1.CustomResourceDefinitionNames{
		Plural:   "anvils",
		Singular: "anvil",
		Kind:     "Anvil",
	}
	crd.Spec.Scope = apiextensionsv1beta1.NamespaceScoped
	crd.Spec.Versions = []apiextensionsv1beta1.CustomResourceDefinitionVersion{
		{
			Name:    "v1",
			Served:  true,
			Storage: false,
		},
		{
			Name:    "v2",
			Served:  true,
			Storage: true,
		},
	}
	crd.Spec.Validation = &apiextensionsv1beta1.CustomResourceValidation{
		OpenAPIV3Schema: &apiextensionsv1beta1.JSONSchemaProps{
			Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
				"spec": {
					Type:     "object",
					Required: []string{"lbs"},
					Properties: map[string]apiextensionsv1beta1.JSONSchemaProps{
						"lbs": {
							Type:    "integer",
							Minimum: pointer.Float64Ptr(1.0),
							Maximum: pointer.Float64Ptr(9000.0),
						},
					},
				},
			},
		},
	}
	return crd
}

func TestMultipleVersions_CustomResourceV1(t *testing.T) {
	nt := nomostest.New(t)

	// Add the Anvil CRD.
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", anvilV1CRD())
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CRD")
	nt.WaitForRepoSyncs()
	nt.RenewClient()

	// Add the v1 and v1beta1 Anvils and verify they are created.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv1.yaml", anvilCR("v1", "first", 10))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv2.yaml", anvilCR("v2", "second", 100))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding v1 and v2 Anvil CRs")
	nt.WaitForRepoSyncs()

	err := nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Modify the v1 and v1beta1 Anvils and verify they are updated.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv1.yaml", anvilCR("v1", "first", 20))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvilv2.yaml", anvilCR("v2", "second", 200))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modifying v1 and v2 Anvil CRs")
	nt.WaitForRepoSyncs()

	err = nt.Validate("first", "foo", anvilCR("v1", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate("second", "foo", anvilCR("v2", "", 0))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func anvilV1CRD() *apiextensionsv1.CustomResourceDefinition {
	crd := fake.CustomResourceDefinitionV1Object(core.Name("anvils.acme.com"))
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
									Minimum: pointer.Float64Ptr(1.0),
									Maximum: pointer.Float64Ptr(9000.0),
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
									Minimum: pointer.Float64Ptr(1.0),
									Maximum: pointer.Float64Ptr(9000.0),
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
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "acme.com",
		Version: version,
		Kind:    "Anvil",
	})
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

func TestMultipleVersions_RoleBinding(t *testing.T) {
	nt := nomostest.New(t)
	supportV1beta1, err := nt.SupportV1Beta1CRD()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}

	rbV1 := fake.RoleBindingObject(core.Name("v1user"))
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

	rbV1Beta1 := fake.RoleBindingV1Beta1Object(core.Name("v1beta1user"))
	rbV1Beta1.RoleRef = rbacv1beta1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     "acme-admin",
	}
	rbV1Beta1.Subjects = append(rbV1Beta1.Subjects, rbacv1beta1.Subject{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     "v1beta1user@acme.com",
	})

	// Add the v1 and v1beta1 RoleBindings and verify they are created.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/rbv1.yaml", rbV1)
	if supportV1beta1 {
		nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/rbv1beta1.yaml", rbV1Beta1)
	}
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding v1 and v1beta1 RoleBindings")
	nt.WaitForRepoSyncs()

	err = nt.Validate("v1user", "foo", &rbacv1.RoleBinding{},
		hasV1Subjects("v1user@acme.com"))
	if err != nil {
		nt.T.Fatal(err)
	}

	if supportV1beta1 {
		err = nt.Validate("v1beta1user", "foo", &rbacv1beta1.RoleBinding{},
			hasV1Beta1Subjects("v1beta1user@acme.com"))
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	// Modify the v1 and v1beta1 RoleBindings and verify they are updated.
	rbV1.Subjects = append(rbV1.Subjects, rbacv1.Subject{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     "v1admin@acme.com",
	})
	rbV1Beta1.Subjects = append(rbV1Beta1.Subjects, rbacv1beta1.Subject{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "User",
		Name:     "v1beta1admin@acme.com",
	})

	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/rbv1.yaml", rbV1)
	if supportV1beta1 {
		nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/rbv1beta1.yaml", rbV1Beta1)
	}
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modifying v1 and v1beta1 RoleBindings")
	nt.WaitForRepoSyncs()

	err = nt.Validate("v1user", "foo", &rbacv1.RoleBinding{},
		hasV1Subjects("v1user@acme.com", "v1admin@acme.com"))
	if err != nil {
		nt.T.Fatal(err)
	}
	if supportV1beta1 {
		err = nt.Validate("v1beta1user", "foo", &rbacv1beta1.RoleBinding{},
			hasV1Beta1Subjects("v1beta1user@acme.com", "v1beta1admin@acme.com"))
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	if supportV1beta1 {
		// Remove the v1beta1 RoleBinding and verify that only it is deleted.
		nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/rbv1beta1.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing v1beta1 RoleBinding")
		nt.WaitForRepoSyncs()

		if err := nt.Validate("v1user", "foo", &rbacv1.RoleBinding{}); err != nil {
			nt.T.Fatal(err)
		}
		if err := nt.ValidateNotFound("v1beta1user", "foo", &rbacv1beta1.RoleBinding{}); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Remove the v1 RoleBinding and verify that it is also deleted.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/rbv1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing v1 RoleBinding")
	nt.WaitForRepoSyncs()

	if err := nt.ValidateNotFound("v1user", "foo", &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func hasV1Subjects(subjects ...string) func(o client.Object) error {
	return func(o client.Object) error {
		r, ok := o.(*rbacv1.RoleBinding)
		if !ok {
			return nomostest.WrongTypeErr(o, r)
		}
		if len(r.Subjects) != len(subjects) {
			return errors.Wrapf(nomostest.ErrFailedPredicate, "want %v subjects; got %v", subjects, r.Subjects)
		}

		found := make(map[string]bool)
		for _, subj := range r.Subjects {
			found[subj.Name] = true
		}
		for _, name := range subjects {
			if !found[name] {
				return errors.Wrapf(nomostest.ErrFailedPredicate, "missing subject %q", name)
			}
		}

		return nil
	}
}

func hasV1Beta1Subjects(subjects ...string) func(o client.Object) error {
	return func(o client.Object) error {
		r, ok := o.(*rbacv1beta1.RoleBinding)
		if !ok {
			return nomostest.WrongTypeErr(o, r)
		}
		if len(r.Subjects) != len(subjects) {
			return errors.Wrapf(nomostest.ErrFailedPredicate, "want %v subjects; got %v", subjects, r.Subjects)
		}

		found := make(map[string]bool)
		for _, subj := range r.Subjects {
			found[subj.Name] = true
		}
		for _, name := range subjects {
			if !found[name] {
				return errors.Wrapf(nomostest.ErrFailedPredicate, "missing subject %q", name)
			}
		}

		return nil
	}
}
