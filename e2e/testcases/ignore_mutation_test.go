// Copyright 2024 Google LLC
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// TestAddIgnoreMutationAndSpecChangesToManagedObject adds the `client.lifecycle.config.k8s.io/mutation`
// annotation as well as the `season` annotation to a resource, and verifies that Config Sync doesn't update
// the `season` annotation based on the source config
func TestAddIgnoreMutationAndSpecChangesToManagedObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add a new namespace")
	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Add the ignore mutation annotation and other spec changes to the namespace")
	namespace = k8sobjects.NamespaceObject(
		namespace.Name,
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		core.Annotation("season", "winter"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("update namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.HasAnnotationKey(metadata.LifecycleMutationAnnotation))))

	nt.T.Log("Remove the ignore mutation annotation from the declared namespace")
	namespace = k8sobjects.NamespaceObject(namespace.Name,
		core.Annotation("season", "fall"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove ignore mutation annotation"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "fall"),
			testpredicates.MissingAnnotation(metadata.LifecycleMutationAnnotation))))
}

// TestDeclareIgnoreMutationForUnmanagedObject declares an unmanaged resource in the source repo with the
// `client.lifecycle.config.k8s.io/mutation` annotation and the `season` annotation, and verifies that
// Config Sync doesn't update the `season` annotation based on the source config
func TestDeclareIgnoreMutationForUnmanagedObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add an unmanaged namespace using kubectl")
	nsObj := k8sobjects.NamespaceObject("bookstore")
	nt.Must(nt.KubeClient.Apply(nsObj))

	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(nsObj); err != nil {
			if !apierrors.IsNotFound(err) && !apierrors.IsForbidden(err) {
				nt.T.Log(err)
			}
		}
	})

	if err := nt.Validate(nsObj.Name, "", &corev1.Namespace{}); err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Declare the unmanaged namespace with the ignore mutation annotation and other spec changes")
	namespace := k8sobjects.NamespaceObject(
		nsObj.Name,
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
			testpredicates.MissingAnnotation("season"))))
}

// TestDeclareUnmanagedObjectWithoutIgnoreAnnotation verifies that the `client.lifecycle.config.k8s.io/mutation` annotation
// is removed when an unmanaged object is declared in the source repo
func TestDeclareUnmanagedObjectWithoutIgnoreAnnotation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add an unmanaged namespace with the ignore-mutation annotation using kubectl ")
	nsObj := k8sobjects.NamespaceObject("bookstore",
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
	)

	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(nsObj); err != nil {
			if !apierrors.IsNotFound(err) && !apierrors.IsForbidden(err) {
				nt.T.Log(err)
			}
		}
	})

	nt.Must(nt.KubeClient.Apply(nsObj))

	if err := nt.Validate(nsObj.Name, "", &corev1.Namespace{}); err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Declare the namespace without the ignore mutation annotation")
	namespace := k8sobjects.NamespaceObject(
		nsObj.Name,
		core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.MissingAnnotation(metadata.LifecycleMutationAnnotation),
		)))
}

// TestMutationIgnoredObjectIsDeleted verifies that a mutation-ignored object is recreated based on
// the source configs
func TestMutationIgnoredObjectIsDeleted(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add namespace with the ignore-mutation annotation to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		core.Annotation("foo", "bar"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", ""), testpredicates.HasAnnotation("foo", "bar"))

	nomostest.WaitForWebhookReadiness(nt)

	// Webhook SHOULD prevent kubectl from deleting a resource managed by Config Sync.
	_, err := nt.Shell.Kubectl("delete", "ns", namespace.Name)
	if err == nil {
		nt.T.Fatalf("got `kubectl delete ns %s` success, want err", namespace.Name)
	}

	nt.T.Log("Remove foo=bar annotation from the declared namespace")
	updatedNamespace := k8sobjects.NamespaceObject(namespace.Name,
		core.Annotation("season", "summer"),
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", updatedNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("update namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("foo", "bar"),
			testpredicates.HasAnnotationKey(metadata.LifecycleMutationAnnotation))))

	nt.T.Log("Modify managed field foo of the declared namespace using kubectl")
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "foo=baz")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite foo=baz` error %v %s, want return nil", err, out)
	}

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", ""), testpredicates.HasAnnotation("foo", "baz"))

	// The reason we need to stop the webhook here is that the webhook denies a request to delete the namespace
	nomostest.StopWebhook(nt)

	nt.Must(nt.Watcher.WatchObject(kinds.Deployment(),
		core.RootReconcilerName(nomostest.DefaultRootSyncID.Name), configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.DeploymentMissingEnvVar(reconcilermanager.Reconciler, reconcilermanager.WebhookEnabled),
		)))

	nt.T.Log("Delete declared namespace using kubectl")
	nt.MustKubectl("delete", "ns", namespace.Name)

	// Remediator SHOULD recreate the namespace
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
			testpredicates.MissingAnnotation("foo"),
		)))
}

// TestMutationIgnoredObjectPruned verifies a resource's state when it is pruned then recreated
func TestMutationIgnoredObjectPruned(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add  namespace to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// prune the namespace
	nt.T.Log("Prune the namespace")
	nt.Must(rootSyncGitRepo.Remove("acme/ns.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("prune namespace-bookstore"))
	nt.Must(nt.WatchForAllSyncs())

	// Namespace should have been pruned
	nt.Must(nt.Watcher.WatchForNotFound(kinds.Namespace(), namespace.Name, ""))

	nt.T.Log("Add back namespace to Git")
	namespace = k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "winter"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "winter"),
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		)))
}

// TestAnnotationDrift verifies that the `season` annotation is correct when modified with Kubectl and
// in the declared config
func TestAnnotationDrift(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Adding namespace to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Add new annotation to the declared namespace")
	updatedNamespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", updatedNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("update namespace"))
	firstCommitHash := rootSyncGitRepo.MustHash(nt.T)

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.MissingAnnotation("season"),
			testpredicates.HasAnnotationKey(metadata.LifecycleMutationAnnotation))))

	nt.T.Log("Add the 'season=winter' annotation to the namespace using kubectl")
	nsObj := namespace.DeepCopy()
	nsObj.Annotations["season"] = "winter"
	nt.Must(nt.KubeClient.Apply(nsObj))

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.SyncTokenAnnotationKey, firstCommitHash),
			testpredicates.HasAnnotation("season", "winter"),
		)))

	// Push a new commit to trigger a new apply.
	nt.T.Log("Add another namespace to Git to run the applier")
	nsObj2 := k8sobjects.NamespaceObject("new-ns")
	nt.Must(rootSyncGitRepo.Add("acme/ns2.yaml", nsObj2))
	nt.Must(rootSyncGitRepo.CommitAndPush("add another namespace"))
	nt.Must(nt.WatchForAllSyncs())
	secondCommitHash := rootSyncGitRepo.MustHash(nt.T)

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.SyncTokenAnnotationKey, secondCommitHash),
			testpredicates.HasAnnotation("season", "winter"),
		)))

	// Modify a managed field
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=fall")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite season=fall` error %v %s, want return nil", err, out)
	}
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "fall"),
		)))
}

// TestDriftKubectlAnnotateConfigSyncAnnotation modifies a
// managed field of a resource that has the
// `client.lifecycle.config.k8s.io/mutation` annotation, and verifies that
// Config Sync does not correct it.
func TestDriftKubectlAnnotateConfigSyncAnnotation(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"),
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// The reason we need to stop the webhook here is that the webhook denies a request to modify Config Sync metadata
	// even if the resource has the `client.lifecycle.config.k8s.io/mutation` annotation.
	nomostest.StopWebhook(nt)
	// Stopping the webhook causes the reconciler to restart. Wait so that we aren't
	// racing with the applier and are actually testing the remediator.
	nt.Must(nt.Watcher.WatchObject(kinds.Deployment(),
		core.RootReconcilerName(rootSyncID.Name), configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.DeploymentMissingEnvVar(reconcilermanager.Reconciler, reconcilermanager.WebhookEnabled),
		)))

	// Modify a Config Sync annotation
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=fall", metadata.ManagementModeAnnotationKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=fall` error %v %s, want return nil", metadata.ManagementModeAnnotationKey, err, out)
	}

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.IsManagementEnabled(),
		)))
}

// TestDriftKubectlAnnotateDeleteManagedFieldsWithIgnoreMutationAnnotation
// deletes a managed field of a resource that has the
// `client.lifecycle.config.k8s.io/mutation` annotation, and verifies that
// Config Sync does not correct it.
func TestDriftKubectlAnnotateDeleteManagedFieldsWithIgnoreMutationAnnotation(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"),
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// Delete a managed field
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season-")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD NOT correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.MissingAnnotation("season"))))

	// The reason we need to stop the webhook here is that the webhook denies a request to modify Config Sync metadata
	// even if the resource has the `client.lifecycle.config.k8s.io/mutation` annotation.
	nomostest.StopWebhook(nt)
	// Stopping the webhook causes the reconciler to restart. Wait so that we aren't
	// racing with the applier and are actually testing the remediator.
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(),
			core.RootReconcilerName(rootSyncID.Name), configsync.ControllerNamespace,
			testwatcher.WatchPredicates(
				testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
				testpredicates.DeploymentMissingEnvVar(reconcilermanager.Reconciler, reconcilermanager.WebhookEnabled),
			))
	})
	tg.Go(func() error {
		// Note: this proves that the applier DOES currently honor the ignore-mutation annotation.
		return nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
			testwatcher.WatchPredicates(testpredicates.MissingAnnotation("season")))
	})
	nt.Must(tg.Wait())

	// Delete a Config Sync annotation
	out, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ManagementModeAnnotationKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` error %v %s, want return nil", metadata.ManagementModeAnnotationKey, err, out)
	}

	time.Sleep(10 * time.Second)

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.IsManagementEnabled(),
		)))
}

// TestAddIgnoreMutationAnnotationDirectly verifies the behavior of the applier when the
// `client.lifecycle.config.k8s.io/mutation` annotation is added to a resource using kubectl
func TestAddIgnoreMutationAnnotationDirectly(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
	// Webhook SHOULD deny the requests since this annotation is a part of the Config Sync metadata.
	ignoreMutation := fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
	_, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` success, want err", ignoreMutation)
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)
	// Stopping the webhook causes the reconciler to restart. Wait so that we aren't
	// racing with the applier and are actually testing the remediator.
	nt.Must(nt.Watcher.WatchObject(kinds.Deployment(),
		core.RootReconcilerName(rootSyncID.Name), configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.DeploymentMissingEnvVar(reconcilermanager.Reconciler, reconcilermanager.WebhookEnabled),
		)))

	// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
	ignoreMutation = fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` error %v %s, want return nil", ignoreMutation, err, out)
	}

	// Remediator SHOULD remove this field
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.MissingAnnotation(metadata.LifecycleMutationAnnotation),
		),
		testwatcher.WatchTimeout(30*time.Second)))
}

// TestKubectlAddAnnotation adds a new field with kubectl into a resource managed
// by Config Sync that has the `client.lifecycle.config.k8s.io/mutation` annotation,
// and verifies that Config Sync does not remove this field.
func TestKubectlAddAnnotation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// Add a new annotation to the namespace object
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season=summer")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season=summer` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD NOT remove this field
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.HasAnnotation("season", "summer"))))
}
