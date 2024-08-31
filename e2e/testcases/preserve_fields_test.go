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
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestPreserveGeneratedServiceFields(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Declare the Service's Namespace
	ns := "autogen-fields"
	nsObj := k8sobjects.NamespaceObject(ns)
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns), nsObj))

	// Declare the Service.
	serviceName := "e2e-test-service"
	serviceObj := k8sobjects.ServiceObject(core.Name(serviceName))
	// The port numbers are arbitrary - just any unused port.
	// Don't reuse these port in other tests just in case.
	targetPort1 := 9376
	targetPort2 := 9377
	serviceObj.Spec = corev1.ServiceSpec{
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
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), serviceObj))

	nt.Must(rootSyncGitRepo.CommitAndPush("declare Namespace and Service"))
	nt.Must(nt.WatchForAllSyncs())

	// Ensure the Service has the target port we set.
	err := nt.Watcher.WatchObject(kinds.Service(), serviceName, ns,
		[]testpredicates.Predicate{hasTargetPort(targetPort1)})
	if err != nil {
		nt.T.Fatal(err)
	}

	// We want to wait until the Service specifies ClusterIP and NodePort.
	// We're going to ensure these fields don't change during the test; ACM should
	// not modify these fields since they're never specified and StrategicMergePatch
	// won't overwrite them otherwise.
	var gotService *corev1.Service
	duration, err := retry.Retry(60*time.Second, func() error {
		service := &corev1.Service{}
		err := nt.Validate(serviceName, ns, service,
			specifiesClusterIP, specifiesNodePort)
		if err != nil {
			return err
		}
		// The Service specifies the fields we're looking for, so record it.
		gotService = service
		return nil
	})
	nt.T.Logf("waited %v for nodePort and clusterIP to be set", duration)
	if err != nil {
		nt.T.Fatal(err)
	}

	// If strategic merge is NOT being used, Nomos and Kubernetes fight over
	// nodePort.  Nomos constantly deletes the value, and Kubernetes assigns a
	// random value each time. ClusterIP has similar behavior.
	generatedNodePort := gotService.Spec.Ports[0].NodePort
	generatedClusterIP := gotService.Spec.ClusterIP

	// This can only return nil if the NodePort/ClusterIP was updated.
	// Potentially flaky check since other things can cause NodePort/ClusterIP
	// to change; copied from bats.
	err = nt.Watcher.WatchObject(kinds.Service(), serviceName, ns,
		[]testpredicates.Predicate{hasDifferentNodePortOrClusterIP(generatedNodePort, generatedClusterIP)},
		testwatcher.WatchTimeout(30*time.Second))
	if err == nil {
		// We want non-nil error from the Retry above - if err is nil then at least
		// one was incorrectly changed.
		// The node port or cluster IP was updated, so we aren't using StrategicMergePatch.
		nt.T.Fatalf("not using strategic merge patch: %v", err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, serviceObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	updatedService := serviceObj.DeepCopy()
	updatedService.Spec.Ports[0].TargetPort = intstr.FromInt(targetPort2)
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/service.yaml", ns), updatedService))
	nt.Must(rootSyncGitRepo.CommitAndPush("update declared Service"))
	nt.Must(nt.WatchForAllSyncs())

	// Ensure the Service has the new target port we set.
	err = nt.Watcher.WatchObject(kinds.Service(), serviceName, ns,
		[]testpredicates.Predicate{hasTargetPort(targetPort2)})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, updatedService)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestPreserveGeneratedClusterRoleFields(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nsViewerName := "namespace-viewer"
	nsViewer := k8sobjects.ClusterRoleObject(core.Name(nsViewerName),
		core.Label("permissions", "viewer"))
	nsViewer.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"namespaces"},
		Verbs:     []string{"get", "list"},
	}}
	nt.Must(rootSyncGitRepo.Add("acme/cluster/ns-viewer-cr.yaml", nsViewer))

	rbacViewerName := "rbac-viewer"
	rbacViewer := k8sobjects.ClusterRoleObject(core.Name(rbacViewerName),
		core.Label("permissions", "viewer"))
	rbacViewer.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{rbacv1.SchemeGroupVersion.Group},
		Resources: []string{"roles", "rolebindings", "clusterroles", "clusterrolebindings"},
		Verbs:     []string{"get", "list"},
	}}
	nt.Must(rootSyncGitRepo.Add("acme/cluster/rbac-viewer-cr.yaml", rbacViewer))

	aggregateRoleName := "aggregate"
	// We have to declare the YAML explicitly because otherwise the declaration
	// explicitly declares "rules: []" due to how Go handles empty/unset fields.
	nt.Must(rootSyncGitRepo.AddFile("acme/cluster/aggregate-viewer-cr.yaml", []byte(`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: aggregate
aggregationRule:
  clusterRoleSelectors:
  - matchLabels:
      permissions: viewer`)))
	aggregateViewer := rootSyncGitRepo.MustGet(nt.T, "acme/cluster/aggregate-viewer-cr.yaml")

	nt.Must(rootSyncGitRepo.CommitAndPush("declare ClusterRoles"))
	nt.Must(nt.WatchForAllSyncs())

	// Ensure the aggregate rule is actually aggregated.
	err := nt.Watcher.WatchObject(kinds.ClusterRole(), aggregateRoleName, "",
		[]testpredicates.Predicate{
			clusterRoleHasRules([]rbacv1.PolicyRule{
				nsViewer.Rules[0], rbacViewer.Rules[0],
			}),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsViewer)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rbacViewer)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, aggregateViewer)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update aggregateRole with a new label.
	nt.Must(rootSyncGitRepo.AddFile("acme/cluster/aggregate-viewer-cr.yaml", []byte(`
kind: ClusterRole
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: aggregate
  labels:
    meaningless-label: exists
aggregationRule:
  clusterRoleSelectors:
  - matchLabels:
      permissions: viewer`)))
	aggregateViewer = rootSyncGitRepo.MustGet(nt.T, "acme/cluster/aggregate-viewer-cr.yaml")
	nt.Must(rootSyncGitRepo.CommitAndPush("add label to aggregate ClusterRole"))
	nt.Must(nt.WatchForAllSyncs())

	// Ensure we don't overwrite the aggregate rules.
	err = nt.Validate(aggregateRoleName, "", &rbacv1.ClusterRole{},
		clusterRoleHasRules([]rbacv1.PolicyRule{
			nsViewer.Rules[0], rbacViewer.Rules[0],
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, aggregateViewer)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestPreserveLastApplied ensures we don't destroy the last-applied-configuration
// annotation.
// TODO: Remove this test once all users are past 1.4.0.
func TestPreserveLastApplied(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Declare a ClusterRole and wait for it to sync.
	nsViewerName := "namespace-viewer"
	nsViewer := k8sobjects.ClusterRoleObject(core.Name(nsViewerName),
		core.Label("permissions", "viewer"))
	nsViewer.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"namespaces"},
		Verbs:     []string{"get", "list"},
	}}
	nt.Must(rootSyncGitRepo.Add("acme/cluster/ns-viewer-cr.yaml", nsViewer))
	nt.Must(rootSyncGitRepo.CommitAndPush("add namespace-viewer ClusterRole"))
	nt.Must(nt.WatchForAllSyncs())

	err := nt.Validate(nsViewerName, "", &rbacv1.ClusterRole{})
	if err != nil {
		nt.T.Fatal(err)
	}

	annotationKeys := metadata.GetNomosAnnotationKeys()

	nsViewer.Annotations[corev1.LastAppliedConfigAnnotation] = `{"apiVersion":"rbac.authorization.k8s.io/v1","kind":"ClusterRole","metadata":{"annotations":{"configmanagement.gke.io/cluster-name":"e2e-test-cluster","configmanagement.gke.io/managed":"enabled","configmanagement.gke.io/source-path":"cluster/namespace-viewer-clusterrole.yaml"},"labels":{"app.kubernetes.io/managed-by":"configmanagement.gke.io","permissions":"viewer"},"name":"namespace-viewer"},"rules":[{"apiGroups":[""],"resources":["namespaces"],"verbs":["get","list"]}]}`
	nt.Must(rootSyncGitRepo.Add("ns-viewer-cr-replace.yaml", nsViewer))
	// Admission webhook denies change. We don't get a "LastApplied" annotation
	// as we prevented the change outright.
	_, err = nt.Shell.Kubectl("replace", "-f", filepath.Join(rootSyncGitRepo.Root, "ns-viewer-cr-replace.yaml"))
	if err == nil {
		nt.T.Fatal("got kubectl replace err = nil, want admission webhook to deny")
	}

	err = nt.Watcher.WatchObject(kinds.ClusterRole(), nsViewerName, "",
		[]testpredicates.Predicate{
			testpredicates.HasExactlyAnnotationKeys(annotationKeys...),
		})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsViewer)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestAddUpdateDeleteLabels(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	ns := "crud-labels"
	nsObj := k8sobjects.NamespaceObject(ns)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/crud-labels/ns.yaml", nsObj))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/crud-labels/configmap.yaml"
	cm := k8sobjects.ConfigMapObject(core.Name(cmName))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding ConfigMap with no labels to repo"))
	nt.Must(nt.WatchForAllSyncs())

	var defaultLabels = []string{
		metadata.ManagedByKey,
		metadata.DeclaredVersionLabel,
		metadata.ApplySetPartOfLabel,
	}

	// Checking that the configmap with no labels appears on cluster, and
	// that no user labels are specified
	err := nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyLabelKeys(defaultLabels...))
	if err != nil {
		nt.T.Fatal(err)
	}

	cm.Labels["baz"] = "qux"
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update label for ConfigMap in repo"))
	nt.Must(nt.WatchForAllSyncs())

	var updatedLabels []string
	updatedLabels = append(updatedLabels, defaultLabels...)
	updatedLabels = append(updatedLabels, "baz")

	// Checking that label was added after syncing.
	nt.Must(nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyLabelKeys(updatedLabels...)))

	delete(cm.Labels, "baz")
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Delete label for configmap in repo"))
	nt.Must(nt.WatchForAllSyncs())

	// Check that the label is deleted after syncing.
	nt.Must(nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyLabelKeys(defaultLabels...)))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cm)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestAddUpdateDeleteAnnotations(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	ns := "crud-annotations"
	nsObj := k8sobjects.NamespaceObject(ns)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/crud-annotations/ns.yaml", nsObj))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/crud-annotations/configmap.yaml"
	cmObj := k8sobjects.ConfigMapObject(core.Name(cmName))
	nt.Must(rootSyncGitRepo.Add(cmPath, cmObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding ConfigMap with no annotations to repo"))
	nt.Must(nt.WatchForAllSyncs())

	annotationKeys := metadata.GetNomosAnnotationKeys()

	// Checking that the configmap with no annotations appears on cluster, and
	// that no user annotations are specified
	err := nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(annotationKeys...))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cmObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	cmObj.Annotations["baz"] = "qux"
	nt.Must(rootSyncGitRepo.Add(cmPath, cmObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update annotation for ConfigMap in repo"))
	nt.Must(nt.WatchForAllSyncs())

	updatedKeys := append([]string{"baz"}, annotationKeys...)

	// Checking that annotation is updated after syncing an update.
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(updatedKeys...),
		testpredicates.HasAnnotation("baz", "qux"))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cmObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	delete(cmObj.Annotations, "baz")
	nt.Must(rootSyncGitRepo.Add(cmPath, cmObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Delete annotation for configmap in repo"))
	nt.Must(nt.WatchForAllSyncs())

	// Check that the annotation is deleted after syncing.
	err = nt.Validate(cmName, ns, &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(annotationKeys...))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cmObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func hasDifferentNodePortOrClusterIP(nodePort int32, clusterIP string) testpredicates.Predicate {
	// We have to check both in the same Predicate as predicates are AND-ed together.
	// We want to return nil if EITHER nodePort or clusterIP changes.
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		service, ok := o.(*corev1.Service)
		if !ok {
			return testpredicates.WrongTypeErr(o, &corev1.Service{})
		}
		gotNodePort := service.Spec.Ports[0].NodePort
		gotClusterIP := service.Spec.ClusterIP
		if nodePort == gotNodePort && clusterIP == gotClusterIP {
			return errors.New("spec.ports[0].nodePort and spec.clusterIP unchanged")
		}
		return nil
	}
}

func specifiesClusterIP(o client.Object) error {
	if o == nil {
		return testpredicates.ErrObjectNotFound
	}
	service, ok := o.(*corev1.Service)
	if !ok {
		return testpredicates.WrongTypeErr(o, &corev1.Service{})
	}
	if service.Spec.ClusterIP == "" {
		return errors.New("spec.clusterIP is not set")
	}
	return nil
}

func specifiesNodePort(o client.Object) error {
	if o == nil {
		return testpredicates.ErrObjectNotFound
	}
	service, ok := o.(*corev1.Service)
	if !ok {
		return testpredicates.WrongTypeErr(o, &corev1.Service{})
	}
	if service.Spec.Ports[0].NodePort == 0 {
		return errors.New("spec.ports[0].nodePort is not set")
	}
	return nil
}

func hasTargetPort(want int) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		service, ok := o.(*corev1.Service)
		if !ok {
			return testpredicates.WrongTypeErr(o, &corev1.Service{})
		}
		got := service.Spec.Ports[0].TargetPort.IntValue()
		if want != got {
			return fmt.Errorf("port %d synced, want %d", got, want)
		}
		return nil
	}
}
