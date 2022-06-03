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
	"path/filepath"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCRDDeleteBeforeRemoveCustomResourceV1Beta1(t *testing.T) {
	nt := nomostest.New(t)

	support, err := nt.SupportV1Beta1CRD()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}
	// Skip this test if v1beta1 CRD is not supported in the testing cluster.
	if !support {
		return
	}

	crdFile := filepath.Join(".", "..", "testdata", "customresources", "v1beta1_crds", "anvil-crd.yaml")
	clusterFile := filepath.Join(".", "..", "testdata", "customresources", "v1beta1_crds", "clusteranvil-crd.yaml")
	_, err = nt.Kubectl("apply", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nt.Kubectl("apply", "-f", clusterFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.WaitForCRDs(nt, []string{"anvils.acme.com", "clusteranvils.acme.com"})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/ns.yaml", fake.NamespaceObject("prod"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilCR("v1", "heavy", 10))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
	nt.WaitForRepoSyncs()

	// Reset discovery client to pick up Anvil CRD
	nt.RenewClient()

	if nt.MultiRepo {
		err := nt.Validate(configuration.Name, "", &admissionv1.ValidatingWebhookConfiguration{},
			hasRule("acme.com.v1.admission-webhook.configsync.gke.io"))
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		return nt.Validate("heavy", "prod", anvilCR("v1", "", 0))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 3,
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("Anvil"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove CRD
	// This will garbage collect the CR too and block until both are deleted.
	_, err = nt.Kubectl("delete", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to invalidate the cached Anvil CRD
	nt.RenewClient()

	// Modify the Anvil yaml to trigger immediate re-sync, instead of waiting
	// for automatic retry (1hr default).
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilCR("v1", "heavy", 100))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR")

	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")
	} else {
		nt.WaitForRepoImportErrorCode(status.UnknownKindErrorCode)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ReconcilerMetrics.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, 1, 1)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove the CR.
	// This should fix the error.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/prod/anvil-v1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well")
	nt.WaitForRepoSyncs()
}

func TestCRDDeleteBeforeRemoveCustomResourceV1(t *testing.T) {
	nt := nomostest.New(t)

	crdFile := filepath.Join(".", "..", "testdata", "customresources", "v1_crds", "anvil-crd.yaml")
	clusterFile := filepath.Join(".", "..", "testdata", "customresources", "v1_crds", "clusteranvil-crd.yaml")
	_, err := nt.Kubectl("apply", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nt.Kubectl("apply", "-f", clusterFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.WaitForCRDs(nt, []string{"anvils.acme.com", "clusteranvils.acme.com"})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 10))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
	nt.WaitForRepoSyncs()

	// Reset discovery client to pick up Anvil CRD
	nt.RenewClient()

	if nt.MultiRepo {
		err := nt.Validate(configuration.Name, "", &admissionv1.ValidatingWebhookConfiguration{},
			hasRule("acme.com.v1.admission-webhook.configsync.gke.io"))
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		return nt.Validate("heavy", "foo", anvilCR("v1", "", 0))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 3,
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("Anvil"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove CRD
	// This will garbage collect the CR too and block until both are deleted.
	_, err = nt.Kubectl("delete", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to invalidate the cached Anvil CRD
	nt.RenewClient()

	// Modify the Anvil yaml to trigger immediate re-sync, instead of waiting
	// for automatic retry (1hr default).
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 100))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR")

	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")
	} else {
		nt.WaitForRepoImportErrorCode(status.UnknownKindErrorCode)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ReconcilerMetrics.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, 1, 1)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove the CR.
	// This should fix the error.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/anvil-v1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well")
	nt.WaitForRepoSyncs()
}

func TestSyncUpdateCustomResource(t *testing.T) {
	nt := nomostest.New(t)
	support, err := nt.SupportV1Beta1CRD()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}
	// Skip this test if v1beta1 CRD is not supported in the testing cluster.
	if !support {
		return
	}
	for _, dir := range []string{"v1beta1_crds"} {
		t.Run(dir, func(t *testing.T) {
			crdFile := filepath.Join(".", "..", "testdata", "customresources", dir, "anvil-crd-structural.yaml")
			_, err := nt.Kubectl("apply", "-f", crdFile)
			if err != nil {
				nt.T.Fatal(err)
			}

			_, err = nomostest.Retry(30*time.Second, func() error {
				return nt.Validate("anvils.acme.com", "", fake.CustomResourceDefinitionV1Object(), nomostest.IsEstablished)
			})
			if err != nil {
				nt.T.Fatal(err)
			}

			nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
			nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 10))
			nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
			nt.WaitForRepoSyncs()
			nt.RenewClient()

			_, err = nomostest.Retry(30*time.Second, func() error {
				return nt.Validate("heavy", "foo", anvilCR("v1", "", 0), weightEqual10)
			})
			if err != nil {
				nt.T.Fatal(err)
			}

			// Update CustomResource
			nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 100))
			nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updating Anvil CR")
			nt.WaitForRepoSyncs()
			nt.RenewClient()

			_, err = nomostest.Retry(30*time.Second, func() error {
				return nt.Validate("heavy", "foo", anvilCR("v1", "", 0), weightEqual100)
			})
			if err != nil {
				nt.T.Fatal(err)
			}
		})
	}
}

func weightEqual100(obj client.Object) error {
	u := obj.(*unstructured.Unstructured)
	val, _, err := unstructured.NestedInt64(u.Object, "spec", "lbs")
	if err != nil {
		return err
	}
	if val != 100 {
		return fmt.Errorf(".spec.lbs should be 100 but got %d", val)
	}
	return nil
}

func weightEqual10(obj client.Object) error {
	u := obj.(*unstructured.Unstructured)
	val, _, err := unstructured.NestedInt64(u.Object, "spec", "lbs")
	if err != nil {
		return err
	}
	if val != 10 {
		return fmt.Errorf(".spec.lbs should be 10 but got %d", val)
	}
	return nil
}
