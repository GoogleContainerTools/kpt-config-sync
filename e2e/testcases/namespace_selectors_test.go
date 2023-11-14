// Copyright 2023 Google LLC
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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	fake "kpt.dev/configsync/pkg/testing/fake"
)

func TestNamespaceSelectorHierarchicalFormat(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)

	nt.T.Log("Add Namespaces, NamespaceSelectors and Namespace-scoped resources")
	bookstoreNS := "bookstore"
	bookstoreNSS := fake.NamespaceSelectorObject(core.Name(bookstoreNS))
	bookstoreNSS.Spec.Selector.MatchLabels = map[string]string{"app": bookstoreNS}
	bookstoreCM := fake.ConfigMapObject(core.Name("cm-bookstore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, bookstoreNSS.Name))
	bookstoreRQ := fake.ResourceQuotaObject(core.Name("rq-bookstore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, bookstoreNSS.Name))
	bookstoreRQ.Spec.Hard = map[corev1.ResourceName]resource.Quantity{corev1.ResourcePods: resource.MustParse("1")}

	shoestoreNS := "shoestore"
	shoestoreNSS := fake.NamespaceSelectorObject(core.Name(shoestoreNS))
	shoestoreNSS.Spec.Selector.MatchLabels = map[string]string{"app": shoestoreNS}
	shoestoreCM := fake.ConfigMapObject(core.Name("cm-shoestore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, shoestoreNSS.Name))

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/namespace-selector-bookstore.yaml", bookstoreNSS))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/namespace-selector-shoestore.yaml", shoestoreNSS))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/bookstore/ns.yaml", fake.NamespaceObject(bookstoreNS, core.Label("app", bookstoreNS))))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shoestore/ns.yaml", fake.NamespaceObject(shoestoreNS, core.Label("app", shoestoreNS))))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/cm-bookstore.yaml", bookstoreCM))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/rq-bookstore.yaml", bookstoreRQ))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/cm-shoestore.yaml", shoestoreCM))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Namespaces, NamespaceSelectors and Namespace-scoped resources"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookstoreCM.Name, bookstoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookstoreRQ.Name, bookstoreNS, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(nt, "1")); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(shoestoreCM.Name, shoestoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookstoreCM.Name, shoestoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookstoreRQ.Name, shoestoreNS, &corev1.ResourceQuota{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(shoestoreCM.Name, bookstoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceSelectorUnstructuredFormat(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector, ntopts.Unstructured)

	nt.T.Log("Add Namespaces, NamespaceSelectors and Namespace-scoped resources")
	bookstoreNS := "bookstore"
	bookstoreNSS := fake.NamespaceSelectorObject(core.Name(bookstoreNS))
	bookstoreNSS.Spec.Selector.MatchLabels = map[string]string{"app": bookstoreNS}
	bookstoreCM := fake.ConfigMapObject(core.Name("cm-bookstore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, bookstoreNSS.Name))
	bookstoreRQ := fake.ResourceQuotaObject(core.Name("rq-bookstore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, bookstoreNSS.Name))
	bookstoreRQ.Spec.Hard = map[corev1.ResourceName]resource.Quantity{corev1.ResourcePods: resource.MustParse("1")}

	shoestoreNS := "shoestore"
	shoestoreNSS := fake.NamespaceSelectorObject(core.Name(shoestoreNS))
	shoestoreNSS.Spec.Selector.MatchLabels = map[string]string{"app": shoestoreNS}
	shoestoreCM := fake.ConfigMapObject(core.Name("cm-shoestore"),
		core.Annotation(metadata.NamespaceSelectorAnnotationKey, shoestoreNSS.Name))

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespace-selector-bookstore.yaml", bookstoreNSS))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespace-selector-shoestore.yaml", shoestoreNSS))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/bookstore-ns.yaml", fake.NamespaceObject(bookstoreNS, core.Label("app", bookstoreNS))))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/shoestore-ns.yaml", fake.NamespaceObject(shoestoreNS, core.Label("app", shoestoreNS))))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cm-bookstore.yaml", bookstoreCM))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/rq-bookstore.yaml", bookstoreRQ))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cm-shoestore.yaml", shoestoreCM))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Namespaces, NamespaceSelectors and Namespace-scoped resources"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookstoreCM.Name, bookstoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookstoreRQ.Name, bookstoreNS, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(nt, "1")); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(shoestoreCM.Name, shoestoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookstoreCM.Name, shoestoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookstoreRQ.Name, shoestoreNS, &corev1.ResourceQuota{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(shoestoreCM.Name, bookstoreNS, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
}
