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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestSwitchFromMultiRepoToMonoRepo(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo)

	// Declare the Namespace
	ns := "switch-to-mono"
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns),
		fake.NamespaceObject(ns))

	// Declare the Service.
	serviceName := "e2e-test-service"
	service := fake.ServiceObject(core.Name(serviceName))
	// The port numbers are arbitrary - just any unused port.
	// Don't reuse these port in other tests just in case.
	targetPort1 := 9378
	targetPort2 := 9379
	service.Spec = corev1.ServiceSpec{
		SessionAffinity: corev1.ServiceAffinityClientIP,
		Selector:        map[string]string{"app": serviceName},
		Type:            corev1.ServiceTypeNodePort,
		Ports: []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(targetPort1),
		}},
	}
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), service)

	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Namespace and Service")
	nt.WaitForRepoSyncs()

	// Ensure the Service has the target port we set.
	err := nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort1))
	if err != nil {
		nt.T.Fatal(err)
	}

	var rs v1beta1.RootSync
	err = nt.Validate(configsync.RootSyncName, v1.NSConfigManagementSystem, &rs)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete RootSync custom resource from the cluster.
	err = nt.Delete(&rs)
	if err != nil {
		nt.T.Fatalf("deleting RootSync: %v", err)
	}

	// Verify Root Reconciler deployment no longer present.
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, fake.DeploymentObject())
	})
	if err != nil {
		nt.T.Errorf("Reconciler deployment present after deletion: %v", err)
	}

	// Switch to mono-repo mode.
	nomostest.SwitchMode(nt, nt.RootRepos[configsync.RootSyncName].Format)
	t.Cleanup(func() {
		// Switch back to the multi-repo mode so that
		// 1. the test resources can be cleaned up gracefully.
		// 2. the following testcases can run with the same mode.
		nomostest.SwitchMode(nt, nt.RootRepos[configsync.RootSyncName].Format)
	})

	nt.WaitForRepoSyncs()
	// Ensure the Service exists and has the target port we set.
	err = nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort1))
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedService := service.DeepCopy()
	updatedService.Spec.Ports[0].TargetPort = intstr.FromInt(targetPort2)
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), updatedService)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("update declared Service")
	nt.WaitForRepoSyncs()

	// Ensure the Service exists and has the target port we set.
	err = nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort2))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestSwitchFromMonoRepoToMultiRepo(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMultiRepo)

	// Declare the Namespace
	ns := "switch-to-csmr"
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns),
		fake.NamespaceObject(ns))

	// Declare the Service.
	serviceName := "e2e-test-service"
	service := fake.ServiceObject(core.Name(serviceName))
	// The port numbers are arbitrary - just any unused port.
	// Don't reuse these port in other tests just in case.
	targetPort1 := 9380
	targetPort2 := 9381
	service.Spec = corev1.ServiceSpec{
		SessionAffinity: corev1.ServiceAffinityClientIP,
		Selector:        map[string]string{"app": serviceName},
		Type:            corev1.ServiceTypeNodePort,
		Ports: []corev1.ServicePort{{
			Name:       "http",
			Protocol:   corev1.ProtocolTCP,
			Port:       80,
			TargetPort: intstr.FromInt(targetPort1),
		}},
	}
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), service)

	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Namespace and Service")
	nt.WaitForRepoSyncs()

	// Ensure the Service has the target port we set.
	err := nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort1))
	if err != nil {
		nt.T.Fatal(err)
	}

	d := fake.DeploymentObject()
	err = nt.Validate(filesystem.GitImporterName, v1.NSConfigManagementSystem, d)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete git-importer from the cluster.
	err = nt.Delete(d)
	if err != nil {
		nt.T.Fatalf("deleting Repo: %v", err)
	}

	// Verify git-importer no longer present.
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(filesystem.GitImporterName, v1.NSConfigManagementSystem, fake.DeploymentObject())
	})
	if err != nil {
		nt.T.Errorf("Git importer deployment present after deletion: %v", err)
	}

	// Switch to multi-repo mode.
	nomostest.SwitchMode(nt, nt.RootRepos[configsync.RootSyncName].Format)
	t.Cleanup(func() {
		// Switch back to the mono-repo mode so that
		// 1. the test resources can be cleaned up gracefully.
		// 2. the following testcases can run with the same mode.
		nomostest.SwitchMode(nt, nt.RootRepos[configsync.RootSyncName].Format)
	})

	nt.WaitForRepoSyncs()
	err = nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort1))
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedService := service.DeepCopy()
	updatedService.Spec.Ports[0].TargetPort = intstr.FromInt(targetPort2)
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), updatedService)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("update declared Service")
	nt.WaitForRepoSyncs()

	// Ensure the Service exists and has the target port we set.
	err = nt.Validate(serviceName, ns, &corev1.Service{}, hasTargetPort(targetPort2))
	if err != nil {
		nt.T.Fatal(err)
	}
}
