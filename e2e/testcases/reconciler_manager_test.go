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

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	jserializer "k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	initialFirstCPU                    = 10
	initialFirstMemory                 = 100
	initialTotalCPU                    = 80
	initialAdjustedTotalCPU            = 250
	initialTotalMemory                 = 600
	autopilotCPUIncrements             = 250
	memoryMB                           = 1048576
	expectedFirstContainerCPU1         = 180
	expectedFirstContainerCPU2         = 430
	expectedFirstContainerMemory1      = 100
	expectedFirstContainerMemory2      = 200
	updatedFirstContainerCPULimit      = "500m"
	updatedFirstContainerMemoryLimit   = "500Mi"
	expectedFirstContainerMemoryLimit1 = "100Mi"
	expectedFirstContainerCPULimit1    = "180m"
)

// TestReconcilerManagerTeardown validates that when a RootSync or RepoSync is
// deleted, the reconciler-manager finalizer handles deletion of the reconciler
// and its dependencies managed by the reconciler-manager.
func TestReconcilerManagerTeardown(t *testing.T) {
	testNamespace := "teardown"
	nt := nomostest.New(t, nomostesting.ACMController,
		ntopts.WithDelegatedControl, ntopts.Unstructured,
		ntopts.NamespaceRepo(testNamespace, configsync.RepoSyncName))

	t.Log("Validate the reconciler-manager deployment")
	reconcilerManager := &appsv1.Deployment{}
	setNN(reconcilerManager, client.ObjectKey{Name: reconcilermanager.ManagerName, Namespace: v1.NSConfigManagementSystem})
	err := nt.Validate(reconcilerManager.Name, reconcilerManager.Namespace, reconcilerManager)
	require.NoError(t, err)

	t.Log("Validate the RootSync")
	rootSync := &v1beta1.RootSync{}
	setNN(rootSync, client.ObjectKey{Name: configsync.RootSyncName, Namespace: v1.NSConfigManagementSystem})
	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.Name, rootSync.Namespace, []testpredicates.Predicate{
		testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
		testpredicates.HasFinalizer(metadata.ReconcilerManagerFinalizer),
	})
	require.NoError(t, err)

	t.Log("Validate the RootSync reconciler and its dependencies")
	var rootSyncFriends []client.Object

	rootSyncReconciler := &appsv1.Deployment{}
	setNN(rootSyncReconciler, core.RootReconcilerObjectKey(rootSync.Name))
	rootSyncFriends = append(rootSyncFriends, rootSyncReconciler)

	// Note: reconciler-manager no longer applies ConfigMaps to configure the
	// reconciler. So we don't need to validate their deletion. The deletion
	// only happens when upgrading from a very old unsupported version.

	rootSyncCRB := &rbacv1.ClusterRoleBinding{}
	setNN(rootSyncCRB, client.ObjectKey{Name: controllers.RootSyncPermissionsName()})
	rootSyncFriends = append(rootSyncFriends, rootSyncCRB)

	rootSyncSA := &corev1.ServiceAccount{}
	setNN(rootSyncSA, client.ObjectKeyFromObject(rootSyncReconciler))
	rootSyncFriends = append(rootSyncFriends, rootSyncSA)

	for _, obj := range rootSyncFriends {
		err := nt.Validate(obj.GetName(), obj.GetNamespace(), obj)
		require.NoError(t, err)
	}

	t.Log("Validate the RepoSync")
	repoSync := &v1beta1.RepoSync{}
	setNN(repoSync, client.ObjectKey{Name: configsync.RepoSyncName, Namespace: testNamespace})
	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSync.Name, repoSync.Namespace, []testpredicates.Predicate{
		testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
		testpredicates.HasFinalizer(metadata.ReconcilerManagerFinalizer),
	})
	require.NoError(t, err)

	t.Log("Validate the RepoSync reconciler and its dependencies")
	var repoSyncFriends []client.Object

	repoSyncReconciler := &appsv1.Deployment{}
	setNN(repoSyncReconciler, core.NsReconcilerObjectKey(repoSync.Namespace, repoSync.Name))
	repoSyncFriends = append(repoSyncFriends, repoSyncReconciler)

	// Note: reconciler-manager no longer applies ConfigMaps to configure the
	// reconciler. So we don't need to validate their deletion. The deletion
	// only happens when upgrading from a very old unsupported version.

	repoSyncRB := &rbacv1.RoleBinding{}
	setNN(repoSyncRB, client.ObjectKey{
		Name:      controllers.RepoSyncPermissionsName(),
		Namespace: testNamespace,
	})
	repoSyncFriends = append(repoSyncFriends, repoSyncRB)

	repoSyncSA := &corev1.ServiceAccount{}
	setNN(repoSyncSA, client.ObjectKeyFromObject(repoSyncReconciler))
	repoSyncFriends = append(repoSyncFriends, repoSyncSA)

	// See nomostest.CreateNamespaceSecret for creation of user secrets.
	// This is a managed secret with a derivative name.
	repoSyncAuthSecret := &corev1.Secret{}
	setNN(repoSyncAuthSecret, client.ObjectKey{
		Name:      controllers.ReconcilerResourceName(repoSyncReconciler.Name, nomostest.NamespaceAuthSecretName),
		Namespace: repoSyncReconciler.Namespace,
	})
	repoSyncFriends = append(repoSyncFriends, repoSyncAuthSecret)

	// See nomostest.CreateNamespaceSecret for creation of user secrets.
	// This is a managed secret with a derivative name.
	// For local kind clusters, the CA Certs are provided to authenticate the git server.
	if nt.GitProvider.Type() == e2e.Local {
		repoSyncCACertSecret := &corev1.Secret{}
		setNN(repoSyncCACertSecret, client.ObjectKey{
			Name:      controllers.ReconcilerResourceName(repoSyncReconciler.Name, nomostest.NamespaceAuthSecretName),
			Namespace: repoSyncReconciler.Namespace,
		})
		repoSyncFriends = append(repoSyncFriends, repoSyncCACertSecret)
	}

	for _, obj := range repoSyncFriends {
		err := nt.Validate(obj.GetName(), obj.GetNamespace(), obj)
		require.NoError(t, err)
	}

	t.Log("Delete the RootSync and wait for it to be not found")
	err = nomostest.DeleteObjectsAndWait(nt, rootSync)
	require.NoError(t, err)

	t.Log("Validate the RootSync reconciler and its dependencies were deleted")
	for _, obj := range rootSyncFriends {
		gvk, err := kinds.Lookup(obj, nt.Scheme)
		require.NoError(t, err)
		rObj, err := kinds.NewObjectForGVK(gvk, nt.Scheme)
		require.NoError(t, err)
		cObj, err := kinds.ObjectAsClientObject(rObj)
		require.NoError(t, err)
		err = nt.ValidateNotFound(obj.GetName(), obj.GetNamespace(), cObj)
		require.NoError(t, err)
	}

	t.Log("Delete the RepoSync and wait for it to be not found")
	err = nomostest.DeleteObjectsAndWait(nt, repoSync)
	require.NoError(t, err)

	t.Log("Validate the RepoSync reconciler and its dependencies were deleted")
	for _, obj := range repoSyncFriends {
		gvk, err := kinds.Lookup(obj, nt.Scheme)
		require.NoError(t, err)
		rObj, err := kinds.NewObjectForGVK(gvk, nt.Scheme)
		require.NoError(t, err)
		cObj, err := kinds.ObjectAsClientObject(rObj)
		require.NoError(t, err)
		err = nt.ValidateNotFound(obj.GetName(), obj.GetNamespace(), cObj)
		require.NoError(t, err)
	}
}

func setNN(obj client.Object, nn types.NamespacedName) {
	obj.SetName(nn.Name)
	obj.SetNamespace(nn.Namespace)
}

func TestManagingReconciler(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	reconcilerDeployment := &appsv1.Deployment{}
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, reconcilerDeployment); err != nil {
		nt.T.Fatal(err)
	}
	generation := reconcilerDeployment.Generation
	originalImagePullPolicy := testpredicates.ContainerByName(reconcilerDeployment, reconcilermanager.Reconciler).ImagePullPolicy
	updatedImagePullPolicy := corev1.PullAlways
	require.NotEqual(nt.T, updatedImagePullPolicy, originalImagePullPolicy)
	managedReplicas := *reconcilerDeployment.Spec.Replicas
	originalTolerations := reconcilerDeployment.Spec.Template.Spec.Tolerations

	// test case 1: The reconciler-manager should manage most of the fields with one exception:
	// - changes to the container resource requirements should be ignored when the autopilot annotation is set.
	nt.T.Log("Manually update the ImagePullPolicy")
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		for i, container := range d.Spec.Template.Spec.Containers {
			if container.Name == reconcilermanager.Reconciler {
				d.Spec.Template.Spec.Containers[i].ImagePullPolicy = updatedImagePullPolicy
			}
		}
	})
	nt.T.Log("Verify the ImagePullPolicy should be reverted by the reconciler-manager")
	generation += 2 // generation bumped by 2 because the change will be first applied then reverted by the reconciler-manager
	err := nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			testpredicates.DeploymentContainerPullPolicyEquals(reconcilermanager.Reconciler, originalImagePullPolicy),
		})
	if err != nil {
		nt.T.Fatal(err)
	}

	// test case 2: the reconciler-manager should manage the replicas field, so that the reconciler can be resumed after pause.
	nt.T.Log("Manually update the replicas")
	newReplicas := managedReplicas - 1
	nt.MustMergePatch(reconcilerDeployment, fmt.Sprintf(`{"spec": {"replicas": %d}}`, newReplicas))
	nt.T.Log("Verify the reconciler-manager should revert the replicas change")
	generation += 2 // generation bumped by 2 because the change will be first applied then reverted by the reconciler-manager
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), hasReplicas(managedReplicas)})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// test case 3:  the reconciler-manager should not revert the change to the fields that are not owned by reconciler-manager
	nt.T.Log("Manually update fields that are not owned by reconciler-manager")
	var modifiedTolerations []corev1.Toleration
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].TerminationMessagePath = "dev/termination-message"
		d.Spec.Template.Spec.Containers[0].Stdin = true
		d.Spec.Template.Spec.Tolerations = append(d.Spec.Template.Spec.Tolerations, corev1.Toleration{Key: "kubernetes.io/arch", Effect: "NoSchedule", Operator: "Exists"})
		modifiedTolerations = d.Spec.Template.Spec.Tolerations
		d.Spec.Template.Spec.PriorityClassName = "system-node-critical"
	})

	nt.T.Log("Verify the reconciler-manager does not revert the change")
	generation++ // generation bumped by 1 because reconicler-manager should not revert this change
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), firstContainerTerminationMessagePathIs("dev/termination-message"),
			firstContainerStdinIs(true), hasTolerations(modifiedTolerations), hasPriorityClassName("system-node-critical")})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)
	// change the fields back to default values
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].TerminationMessagePath = "dev/termination-log"
		d.Spec.Template.Spec.Containers[0].Stdin = false
		d.Spec.Template.Spec.Tolerations = originalTolerations
		d.Spec.Template.Spec.PriorityClassName = ""
	})
	generation++ // generation bumped by 1 because reconciler-manager should not revert this change
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), firstContainerTerminationMessagePathIs("dev/termination-log"),
			firstContainerStdinIs(false), hasTolerations(originalTolerations), hasPriorityClassName("")})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// test case 4: the reconciler-manager should update the reconciler Deployment if the manifest in the ConfigMap has been changed.
	nt.T.Log("Update the Deployment manifest in the ConfigMap")
	nt.T.Cleanup(func() {
		resetReconcilerDeploymentManifests(nt, reconcilermanager.Reconciler, originalImagePullPolicy)
	})
	mustUpdateReconcilerTemplateConfigMap(nt, func(d *appsv1.Deployment) {
		for i, container := range d.Spec.Template.Spec.Containers {
			if container.Name == reconcilermanager.Reconciler {
				d.Spec.Template.Spec.Containers[i].ImagePullPolicy = updatedImagePullPolicy
			}
		}
	})
	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)
	nt.T.Log("Verify the reconciler Deployment has been updated to the new manifest")
	generation++ // generation bumped by 1 to apply the new change in the default manifests declared in the Config Map
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			testpredicates.DeploymentContainerPullPolicyEquals(reconcilermanager.Reconciler, updatedImagePullPolicy),
		})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// test case 5: the reconciler-manager should delete the git-creds volume if not needed
	currentVolumesCount := len(reconcilerDeployment.Spec.Template.Spec.Volumes)
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Switch the auth type from ssh to none")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "none", "secretRef": {"name":""}}}}`)
	nt.T.Log("Verify the git-creds volume is gone")
	generation++ // generation bumped by 1 to delete the git-creds volume
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), gitCredsVolumeDeleted(currentVolumesCount)})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// test case 6: the reconciler-manager should add the gcenode-askpass-sidecar container when needed
	nt.T.Log("Switch the auth type from none to gcpserviceaccount")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"gcpserviceaccount","secretRef":{"name":""},"gcpServiceAccountEmail":"test-gcp-sa-email@test-project.iam.gserviceaccount.com"}}}`)
	nt.T.Log("Verify the gcenode-askpass-sidecar container should be added")
	if nt.IsGKEAutopilot {
		generation += 2 // generation bumped by 2 because the sidecar container will first be added and then the resource requirements will be adjusted by autopilot
	} else {
		generation++ // generation bumped by 1 to apply the new sidecar container
	}
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), templateForGcpServiceAccountAuthType()})
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// test case 7: the reconciler-manager should mount the git-creds volumes again if the auth type requires a git secret
	nt.T.Log("Switch the auth type gcpserviceaccount to ssh")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"ssh","secretRef":{"name":"git-creds"}}}}`)
	nt.T.Log("Verify the git-creds volume exists and the gcenode-askpass-sidecar container is gone")
	generation++ // generation bumped by 1 to add the git-cred volume again
	err = nt.Watcher.WatchObject(kinds.Deployment(), nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{testpredicates.HasGenerationAtLeast(generation), templateForSSHAuthType()})
	if err != nil {
		nt.T.Fatal(err)
	}
}

type updateFunc func(deployment *appsv1.Deployment)

func mustUpdateRootReconciler(nt *nomostest.NT, f updateFunc) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		d := &appsv1.Deployment{}
		if err := nt.KubeClient.Get(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, d); err != nil {
			return err
		}
		f(d)
		return nt.KubeClient.Update(d)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func mustUpdateReconcilerTemplateConfigMap(nt *nomostest.NT, f updateFunc) {
	decoder := serializer.NewCodecFactory(nt.Scheme).UniversalDeserializer()
	yamlSerializer := jserializer.NewYAMLSerializer(jserializer.DefaultMetaFactory, nt.Scheme, nt.Scheme)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get ConfigMap
		rmConfigMap := &corev1.ConfigMap{}
		if err := nt.KubeClient.Get(controllers.ReconcilerTemplateConfigMapName, configsync.ControllerNamespace, rmConfigMap); err != nil {
			return err
		}

		// Decode ConfigMap data entry as Deployment
		yamlString := rmConfigMap.Data[controllers.ReconcilerTemplateConfigMapKey]
		rmObj := &appsv1.Deployment{}
		if _, _, err := decoder.Decode([]byte(yamlString), nil, rmObj); err != nil {
			return err
		}

		// Mutate Deployment
		f(rmObj)

		// Encode Deployment and update ConfigMap data entry
		yamlBytes, err := runtime.Encode(yamlSerializer, rmObj)
		if err != nil {
			return err
		}
		rmConfigMap.Data[controllers.ReconcilerTemplateConfigMapKey] = string(yamlBytes)

		// Update ConfigMap
		return nt.KubeClient.Update(rmConfigMap)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func resetReconcilerDeploymentManifests(nt *nomostest.NT, containerName string, pullPolicy corev1.PullPolicy) {
	nt.T.Log("Reset the Deployment manifest in the ConfigMap")
	if err := nomostest.ResetReconcilerManagerConfigMap(nt); err != nil {
		nt.T.Fatalf("failed to reset configmap: %v", err)
	}

	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)

	nt.T.Log("Verify the reconciler Deployment has been reverted to the original manifest")
	err := nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerPullPolicyEquals(containerName, pullPolicy),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func hasGeneration(generation int64) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Generation != generation {
			return fmt.Errorf("expected generation: %d, got: %d", generation, d.Generation)
		}
		return nil
	}
}
func firstContainerTerminationMessagePathIs(terminationMessagePath string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].TerminationMessagePath != terminationMessagePath {
			return fmt.Errorf("expected first container terminationMessagePath is: %s, got: %s", terminationMessagePath, d.Spec.Template.Spec.Containers[0].TerminationMessagePath)
		}
		return nil
	}
}
func firstContainerStdinIs(stdin bool) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Stdin != stdin {
			return fmt.Errorf("expected first container stdin is: %t, got: %t", stdin, d.Spec.Template.Spec.Containers[0].Stdin)
		}
		return nil
	}
}
func hasTolerations(tolerations []corev1.Toleration) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for i, toleration := range d.Spec.Template.Spec.Tolerations {
			if !equality.Semantic.DeepEqual(toleration, tolerations[i]) {
				return fmt.Errorf("expected toleration is: %s, got: %s", tolerations[i].String(), toleration.String())
			}
		}
		return nil
	}
}
func hasPriorityClassName(priorityClassName string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.PriorityClassName != priorityClassName {
			return fmt.Errorf("expected priorityClassName is: %s, got: %s", priorityClassName, d.Spec.Template.Spec.PriorityClassName)
		}
		return nil
	}
}

func hasReplicas(replicas int32) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if *d.Spec.Replicas != replicas {
			return fmt.Errorf("expected replicas: %d, got: %d", replicas, *d.Spec.Replicas)
		}
		return nil
	}
}

func firstContainerMemoryLimitIs(memoryLimit string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String() != memoryLimit {
			return fmt.Errorf("expected memory limit of the first container: %s, got: %s",
				memoryLimit, d.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
		}
		return nil
	}
}

func firstContainerCPULimitIs(cpuLimit string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String() != cpuLimit {
			return fmt.Errorf("expected CPU limit of the first container: %s, got: %s",
				cpuLimit, d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String())
		}
		return nil
	}
}

func gitCredsVolumeDeleted(volumesCount int) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if len(d.Spec.Template.Spec.Volumes) != volumesCount-1 {
			return fmt.Errorf("expected volumes count: %d, got: %d",
				volumesCount-1, len(d.Spec.Template.Spec.Volumes))
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return fmt.Errorf("the git-creds volume should be gone for `none` auth type")
			}
		}
		return nil
	}
}

func templateForGcpServiceAccountAuthType() testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return fmt.Errorf("the git-creds volume should not exist for `gcpserviceaccount` auth type")
			}
		}
		for _, container := range d.Spec.Template.Spec.Containers {
			if container.Name == controllers.GceNodeAskpassSidecarName {
				return nil
			}
		}
		return fmt.Errorf("the %s container has not be created yet", controllers.GceNodeAskpassSidecarName)
	}
}

func templateForSSHAuthType() testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for _, container := range d.Spec.Template.Spec.Containers {
			if container.Name == controllers.GceNodeAskpassSidecarName {
				return fmt.Errorf("the gcenode-askpass-sidecar container should not exist for `ssh` auth type")
			}
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return nil
			}
		}
		return fmt.Errorf("the git-creds volume has not be created yet")
	}
}

func totalContainerMemoryRequestIs(memoryRequest int64) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		memoryTotal := getTotalContainerMemoryRequest(d)

		if int64(memoryTotal) != (memoryRequest * memoryMB) {
			return fmt.Errorf("expected total memory request of all containers: %d, got: %d",
				memoryRequest, memoryTotal)
		}
		return nil
	}
}

func getTotalContainerMemoryRequest(d *appsv1.Deployment) int {
	memoryTotal := 0

	for _, container := range d.Spec.Template.Spec.Containers {
		memoryTotal += int(container.Resources.Requests.Memory().Value())
	}

	return memoryTotal
}

func totalContainerCPURequestIs(expectedCPURequest int64) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		actualCPUTotal := getTotalContainerCPURequest(d)

		if int64(actualCPUTotal) != expectedCPURequest {
			return fmt.Errorf("expected total CPU request of all containers: %d, got: %d",
				expectedCPURequest, actualCPUTotal)
		}
		return nil
	}
}

func getTotalContainerCPURequest(d *appsv1.Deployment) int {
	cpuTotal := 0

	for _, container := range d.Spec.Template.Spec.Containers {
		cpuTotal += int(container.Resources.Requests.Cpu().MilliValue())
	}

	return cpuTotal
}

func firstContainerCPURequestIs(cpuRequest int64) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() != cpuRequest {
			return fmt.Errorf("expected CPU request of the first container: %d, got: %d",
				cpuRequest, d.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue())
		}
		return nil
	}
}

func firstContainerMemoryRequestIs(memoryRequest int64) testpredicates.Predicate {
	memoryRequest *= memoryMB
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return testpredicates.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() != memoryRequest {
			return fmt.Errorf("expected memory request of the first container: %d, got: %d",
				memoryRequest, d.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().MilliValue())
		}
		return nil
	}
}

func TestAutopilotReconcilerAdjustment(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController, ntopts.Unstructured)

	// push DRY configs to sync source to enable hydration-controller
	nt.T.Log("Add the namespace-repo root directory to enable hydration")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/namespace-repo", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	nt.T.Log("Update RootSync to sync from the namespace-repo directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "namespace-repo"}}}`)
	syncDirMap := map[types.NamespacedName]string{
		nomostest.RootSyncNN(configsync.RootSyncName): "namespace-repo",
	}
	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	reconcilerDeployment := &appsv1.Deployment{}
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, reconcilerDeployment); err != nil {
		nt.T.Fatal(err)
	}
	firstContainerName := reconcilerDeployment.Spec.Template.Spec.Containers[0].Name

	generation := reconcilerDeployment.Generation
	var expectedTotalCPU int64
	var expectedTotalMemory int64
	var expectedFirstContainerCPU int64
	var expectedFirstContainerMemory int64
	var expectedFirstContainerMemoryLimit string
	var expectedFirstContainerCPULimit string
	firstContainerCPURequest := reconcilerDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
	firstContainerMemoryRequest := reconcilerDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() / memoryMB
	input := map[string]corev1.ResourceRequirements{}
	output := map[string]corev1.ResourceRequirements{}

	// default container resource requests defined in the reconciler template: manifests/templates/reconciler-manager-configmap.yaml, total CPU/memory: 80m/600Mi
	// - hydration-controller: 10m/100Mi (disabled by default)
	// - reconciler: 50m/200Mi
	// - git-sync: 10m/200Mi
	// - otel-agent: 10m/100Mi
	// initial generation = 1, total memory 629145600 (600Mi)

	if nt.IsGKEAutopilot {
		// with autopilot adjustment, CPU of container[0] is increased to 180m
		// bringing the total to 250m
		expectedTotalCPU = initialAdjustedTotalCPU
		expectedFirstContainerCPU = expectedFirstContainerCPU1
	} else {
		expectedTotalCPU = initialTotalCPU
		expectedFirstContainerCPU = initialFirstCPU
	}
	expectedTotalMemory = initialTotalMemory

	nt.T.Log("Validating initial container request value")
	err := nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)
	// increase CPU and memory request to above current request autopilot increment
	nt.T.Log("Increasing CPU and memory request to above current request autopilot increment")
	updatedFirstContainerCPURequest := firstContainerCPURequest + 10
	updatedFirstContainerMemoryRequest := firstContainerMemoryRequest + 100
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))
	// generation = 2
	generation++

	if nt.IsGKEAutopilot {
		// hydration-controller input: 190m/200Mi
		// with autopilot adjustment, CPU of container[0]/hydration-controller is increased to 430m
		// bringing the total to 500m
		expectedTotalCPU += autopilotCPUIncrements
		expectedFirstContainerCPU = expectedFirstContainerCPU2
		expectedFirstContainerMemory = expectedFirstContainerMemory2
	} else {
		expectedTotalCPU = initialTotalCPU + 10
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
	}
	expectedTotalMemory += 100
	// Waiting for reconciliation and validation
	nomostest.Wait(nt.T, "the first container resource requests to be increased", nt.DefaultWaitTimeout, func() error {
		rd := &appsv1.Deployment{}
		err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, rd,
			testpredicates.HasGenerationAtLeast(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
		if err != nil {
			return err
		}
		if nt.IsGKEAutopilot {
			input, output, err = util.AutopilotResourceMutation(rd.Annotations[metadata.AutoPilotAnnotation])
			if err != nil {
				return err
			}
		}
		generation = rd.GetGeneration()
		return nil
	})

	// manually increase first container CPU and memory request to above current user override request
	nt.T.Log("Manually update the first container CPU and Memory request")
	manualFirstContainerCPURequest := updatedFirstContainerCPURequest + 10
	manualFirstContainerMemoryRequest := updatedFirstContainerMemoryRequest + 10

	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
			"memory": resource.MustParse(fmt.Sprintf("%dMi", manualFirstContainerMemoryRequest)),
		}
	})
	nt.T.Log("Verify the reconciler-manager does revert the manual memory/CPU request change")
	generation += 2
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)
	// decrease CPU to below autopilot increment but above current request
	nt.T.Log("Decreasing CPU request to above current request but below autopilot increment")
	if nt.IsGKEAutopilot {
		inputRequest := input[firstContainerName].Requests
		inputCPU := inputRequest.Cpu().MilliValue()
		outputRequest := output[firstContainerName].Requests
		outputCPU := outputRequest.Cpu().MilliValue()
		updatedFirstContainerCPURequest = (inputCPU + outputCPU) / 2
		// autopilot will not adjust CPU and generation
		expectedFirstContainerCPU = expectedFirstContainerCPU2
		expectedFirstContainerMemory = expectedFirstContainerMemory2
	} else {
		// since standard cluster doesnt have resource adjustment annotation
		// validation data are assigned separately than autopilot cluster
		updatedFirstContainerCPURequest -= 10
		expectedTotalCPU -= 10
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
		generation++
	}
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))

	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// Manually decrease cpu request to below current cpu request, but the output cpu request after autopilot adjustment are greater than current cpu request
	nt.T.Log("Manually decrease the first container CPU request")
	manualFirstContainerCPURequest = updatedFirstContainerCPURequest - 10
	if nt.IsGKEAutopilot {
		generation++
	} else {
		generation += 2
	}
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu": resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
		}
	})
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)
	// Manually decrease cpu and memory requests to below current requests, and the output resource requests after autopilot adjustment still below current requests
	nt.T.Log("Manually decrease the first container CPU and Memory request")
	manualFirstContainerCPURequest = initialFirstCPU
	manualFirstContainerMemoryRequest = initialFirstMemory
	generation += 2
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
			"memory": resource.MustParse(fmt.Sprintf("%dMi", manualFirstContainerMemoryRequest)),
		}
	})
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)
	// decrease cpu and memory request to below current request autopilot increment
	nt.T.Log("Reverting CPU and memory back to initial value")
	updatedFirstContainerCPURequest = initialFirstCPU
	updatedFirstContainerMemoryRequest = initialFirstMemory
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))
	// increment generation for both standard and autopilot
	generation++
	if nt.IsGKEAutopilot {
		// hydration-controller input: 10m/100Mi
		// with autopilot adjustment, first container is adjusted to 180m
		// bringing the total CPU request to 250
		expectedTotalCPU -= autopilotCPUIncrements
		expectedTotalMemory = initialTotalMemory
		expectedFirstContainerCPU = expectedFirstContainerCPU1
		expectedFirstContainerMemory = expectedFirstContainerMemory1
	} else {
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
		expectedTotalCPU = initialTotalCPU
		expectedTotalMemory = initialTotalMemory
	}

	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// limit change
	// The reconciler-manager allows manual change to resource limits on non-Autopilot clusters only if the resource limits are neither defined by us in reonciler-manager-configmap
	// nor users through spec.override.resources field.
	// All the changes to resource limits on Autopilot cluster will be ignored, since the resource limits are always same with resource requests on Autopilot cluster

	// Manually update resource limits before override resource limits through API
	nt.T.Log("Manually update the first container CPU and Memory limit")
	manualFirstContainerCPULimit := "600m"
	manualFirstContainerMemoryLimit := "600Mi"
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			"cpu":    resource.MustParse(manualFirstContainerCPULimit),
			"memory": resource.MustParse(manualFirstContainerMemoryLimit),
		}
	})
	nt.T.Log("Verify the reconciler-manager does not revert the manual resource limits change when user does not override the memory limit ")
	if nt.IsGKEAutopilot {
		expectedFirstContainerMemoryLimit = expectedFirstContainerMemoryLimit1
		expectedFirstContainerCPULimit = expectedFirstContainerCPULimit1
	} else {
		expectedFirstContainerMemoryLimit = manualFirstContainerMemoryLimit
		expectedFirstContainerCPULimit = manualFirstContainerCPULimit
	}

	generation++ // generation bumped by 1 because the memory limits are still not owned by reconciler manager
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit),
			firstContainerCPULimitIs(expectedFirstContainerCPULimit),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	// override the resource limits through spec.override.resources field
	nt.T.Log("Updating the container limits")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryLimit":"%s", "cpuLimit":"%s"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryLimit, updatedFirstContainerCPULimit))

	if nt.IsGKEAutopilot {
		// overriding the limit should not trigger any changes
		expectedFirstContainerCPU = expectedFirstContainerCPU1
		expectedFirstContainerMemory = expectedFirstContainerMemory1
		expectedFirstContainerMemoryLimit = expectedFirstContainerMemoryLimit1
		expectedFirstContainerCPULimit = expectedFirstContainerCPULimit1

	} else {
		expectedFirstContainerMemoryLimit = updatedFirstContainerMemoryLimit
		expectedFirstContainerCPULimit = updatedFirstContainerCPULimit
		generation++
	}
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory),
			firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit),
			firstContainerCPULimitIs(expectedFirstContainerCPULimit),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	generation = getDeploymentGeneration(nt, nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem)

	//manually update the resource limits after user override the resource limits through spec.override field, in this case, reconciler-manager own the resource limits field
	nt.T.Log("Manually update the first container CPU and Memory limit")
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			"cpu":    resource.MustParse(manualFirstContainerCPULimit),
			"memory": resource.MustParse(manualFirstContainerMemoryLimit),
		}
	})
	nt.T.Log("Verify the reconciler-manager does revert the manual resource limits change after user override the resource limits through API")
	if nt.IsGKEAutopilot {
		generation++
	} else {
		generation += 2
	}
	err = nt.Watcher.WatchObject(kinds.Deployment(),
		nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
		[]testpredicates.Predicate{
			testpredicates.HasGenerationAtLeast(generation),
			firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit),
			firstContainerCPULimitIs(expectedFirstContainerCPULimit),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func getDeploymentGeneration(nt *nomostest.NT, name, namespace string) int64 {
	dep := &appsv1.Deployment{}
	if err := nt.KubeClient.Get(name, namespace, dep); err != nil {
		nt.T.Fatal(err)
	}
	return dep.GetGeneration()
}
