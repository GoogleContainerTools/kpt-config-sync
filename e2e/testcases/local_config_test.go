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
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/syncertest"
)

var LocalConfigValue = "true"

func TestLocalConfig(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Lifecycle)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	ns := "local-config"
	nt.Must(rootSyncGitRepo.Add(
		"acme/namespaces/local-config/ns.yaml",
		k8sobjects.NamespaceObject(ns)))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/local-config/configmap.yaml"
	cm := k8sobjects.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, LocalConfigValue))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding ConfigMap as local config"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap doesn't exist in the cluster
	err := nt.ValidateNotFound(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the local-config annotation
	cm = k8sobjects.ConfigMapObject(core.Name(cmName))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding ConfigMap without local-config annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the local-config annotation again.
	// This will make the object pruned.
	cm = k8sobjects.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, LocalConfigValue))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Changing ConfigMap to local config"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap is pruned.
	err = nt.ValidateNotFound(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestLocalConfigWithManagementDisabled(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Lifecycle)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	ns := "local-config"
	nt.Must(rootSyncGitRepo.Add(
		"acme/namespaces/local-config/ns.yaml",
		k8sobjects.NamespaceObject(ns)))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/local-config/configmap.yaml"
	cm := k8sobjects.ConfigMapObject(core.Name(cmName))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding ConfigMap"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap exist
	err := nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the management disabled annotation.
	cm = k8sobjects.ConfigMapObject(core.Name(cmName), syncertest.ManagementDisabled)
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Disable the management of ConfigMap"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add the local-config annotation to the unmanaged configmap
	cm = k8sobjects.ConfigMapObject(core.Name(cmName), syncertest.ManagementDisabled,
		core.Annotation(metadata.LocalConfigAnnotationKey, LocalConfigValue))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Change the ConfigMap to local config"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the management disabled annotation
	cm = k8sobjects.ConfigMapObject(core.Name(cmName), core.Annotation(metadata.LocalConfigAnnotationKey, LocalConfigValue))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove the managed disabled annotation and keep the local-config annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Checking that the configmap exist
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Fatal(err)
	}
}
