// Copyright 2025 Google LLC
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

package nomostest

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/kinds"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// StatusWatchNamespace is the namespace for sync status watch resources.
	StatusWatchNamespace = "sync-status-watch"
	// StatusWatchName is the name for sync status watch resources.
	StatusWatchName = "sync-status-watch"
)

const sswImage = testing.TestInfraArtifactRepositoryAddress + "/sync-status-watch-controller:v1.0.0-539a1ddd"

// SetupSyncStatusWatchController builds and deploys the sync status watch controller.
func SetupSyncStatusWatchController(nt *NT) error {
	nt.T.Logf("applying sync status watch controller manifest")
	if err := nt.KubeClient.Create(testSyncStatusWatchNamespace()); err != nil {
		return err
	}
	if err := nt.KubeClient.Create(testSyncStatusWatchServiceAccount()); err != nil {
		return err
	}

	if err := installSyncStatusWatchController(nt); err != nil {
		nt.describeNotRunningTestPods(StatusWatchNamespace)
		return fmt.Errorf("waiting for sync status watch controller to become available: %w", err)
	}
	return nil
}

// TeardownSyncStatusWatchController removes the sync status watch controller.
func TeardownSyncStatusWatchController(nt *NT) error {
	nt.T.Log("tearing down sync status watch controller")
	objs := testSyncStatusWatchObjects()
	for _, o := range objs {
		if err := nt.KubeClient.Delete(o); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Logf("Error deleting %T %s/%s: %v", o, o.GetNamespace(), o.GetName(), err)
		}
	}
	if err := nt.KubeClient.Delete(testSyncStatusWatchNamespace()); err != nil && !apierrors.IsNotFound(err) {
		nt.T.Logf("Error deleting Namespace: %v", err)
	}

	nt.T.Log("Sync status watch controller teardown complete")
	return nil
}

func installSyncStatusWatchController(nt *NT) error {
	objs := testSyncStatusWatchObjects()

	for _, o := range objs {
		if err := nt.KubeClient.Apply(o); err != nil {
			return fmt.Errorf("applying %v %s: %w", o.GetObjectKind().GroupVersionKind(),
				client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, err)
		}
	}

	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), StatusWatchName, StatusWatchNamespace)
}

// testSyncStatusWatchObjects returns all objects needed for the sync-status-watch controller
func testSyncStatusWatchObjects() []client.Object {
	return []client.Object{
		testSyncStatusWatchClusterRole(),
		testSyncStatusWatchClusterRoleBinding(),
		testSyncStatusWatchDeployment(),
	}
}

func testSyncStatusWatchNamespace() *corev1.Namespace {
	return k8sobjects.NamespaceObject(StatusWatchNamespace)
}

func testSyncStatusWatchServiceAccount() *corev1.ServiceAccount {
	return k8sobjects.ServiceAccountObject(
		StatusWatchName,
		core.Namespace(StatusWatchNamespace),
	)
}

func testSyncStatusWatchClusterRole() *rbacv1.ClusterRole {
	cr := k8sobjects.ClusterRoleObject(
		core.Name(StatusWatchName),
	)
	cr.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{"configsync.gke.io"},
			Resources: []string{"rootsyncs", "reposyncs"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
	return cr
}

func testSyncStatusWatchClusterRoleBinding() *rbacv1.ClusterRoleBinding {
	crb := k8sobjects.ClusterRoleBindingObject(core.Name(StatusWatchName))
	crb.RoleRef = rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     StatusWatchName,
	}
	crb.Subjects = []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      StatusWatchName,
			Namespace: StatusWatchNamespace,
		},
	}
	return crb
}

func testSyncStatusWatchDeployment() *appsv1.Deployment {
	deployment := k8sobjects.DeploymentObject(
		core.Name(StatusWatchName),
		core.Namespace(StatusWatchNamespace),
		core.Labels(map[string]string{"app": StatusWatchName}),
	)

	deployment.Spec = appsv1.DeploymentSpec{
		Selector: &v1.LabelSelector{
			MatchLabels: map[string]string{"app": StatusWatchName},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: map[string]string{"app": StatusWatchName},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: StatusWatchName,
				Containers: []corev1.Container{
					{
						Name:            StatusWatchName,
						Image:           sswImage,
						ImagePullPolicy: corev1.PullAlways,
					},
				},
			},
		},
		Strategy: appsv1.DeploymentStrategy{
			Type: appsv1.RecreateDeploymentStrategyType,
		},
	}
	return deployment
}
