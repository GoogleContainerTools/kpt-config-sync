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

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestLocalConfig(t *testing.T) {
	nt := nomostest.New(t)

	ns := "local-config"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/local-config/ns.yaml",
		fake.NamespaceObject(ns))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/local-config/configmap.yaml"
	cm := fake.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, metadata.LocalConfigValue))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding ConfigMap as local config")
	nt.WaitForRepoSyncs()

	// Checking that the configmap doesn't exist in the cluster
	err := nt.ValidateNotFound(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the local-config annotation
	cm = fake.ConfigMapObject(core.Name(cmName))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding ConfigMap without local-config annotation")
	nt.WaitForRepoSyncs()

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the local-config annotation again.
	// This will make the object pruned.
	cm = fake.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, metadata.LocalConfigValue))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Changing ConfigMap to local config")
	nt.WaitForRepoSyncs()

	// Checking that the configmap is pruned.
	err = nt.ValidateNotFound(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestLocalConfigWithManagementDisabled(t *testing.T) {
	nt := nomostest.New(t)

	ns := "local-config"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/local-config/ns.yaml",
		fake.NamespaceObject(ns))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/local-config/configmap.yaml"
	cm := fake.ConfigMapObject(core.Name(cmName))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding ConfigMap")
	nt.WaitForRepoSyncs()

	// Checking that the configmap exist
	err := nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the management disabled annotation.
	cm = fake.ConfigMapObject(core.Name(cmName), syncertest.ManagementDisabled)
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Disable the management of ConfigMap")
	nt.WaitForRepoSyncs()

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the local-config annotation to the unmanaged configmap
	cm = fake.ConfigMapObject(core.Name(cmName), syncertest.ManagementDisabled,
		core.Annotation(metadata.LocalConfigAnnotationKey, metadata.LocalConfigValue))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Change the ConfigMap to local config")
	nt.WaitForRepoSyncs()

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the management disabled annotation
	cm = fake.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, metadata.LocalConfigValue))
	nt.RootRepos[configsync.RootSyncName].Add(cmPath, cm)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove the managed disabled annotation and keep the local-config annotation")
	nt.WaitForRepoSyncs()

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}
}
