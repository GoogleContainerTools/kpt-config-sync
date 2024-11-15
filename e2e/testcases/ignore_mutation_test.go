package e2e

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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

func TestAddIgnoreMutationObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Adding a new namespace")
	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Add the ignore mutation to the namespace")
	updatedNamespace := k8sobjects.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", updatedNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("update namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.HasAnnotationKey(metadata.LifecycleMutationAnnotation))))
}

func TestDeclareIgnoreMutationForUnmanagedObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add an unmanaged namespace using kubectl")
	nsObj := k8sobjects.NamespaceObject("bookstore")
	nt.Must(nt.KubeClient.Apply(nsObj))

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

func TestDeclareExistingObjectWithIgnoreAnnotation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add an umanaged namespace with the ignore-mutation annotation using kubectl ")
	nsObj := k8sobjects.NamespaceObject("bookstore",
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
	)

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
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
			testpredicates.MissingAnnotation("season"))))
}

func TestIgnoreObjectIsDeleted(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add namespace with the ignore-mutation annotation to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		core.Annotation("foo", "bar"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", ""), testpredicates.HasAnnotation("foo", "bar"))

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

	time.Sleep(10 * time.Second)

	// Remediator SHOULD NOT correct it
	err = nt.Validate("bookstore", "", &corev1.Namespace{}, testpredicates.HasAnnotation("foo", "baz"))
	if err != nil {
		nt.T.Fatal(err)
	}

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

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
			testpredicates.MissingAnnotation("foo"),
		)))
}

func TestPruningIgnoredObject(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Adding namespace to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// prune the namespace
	nt.Must(rootSyncGitRepo.Remove("acme/ns.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Prune namespace-bookstore"))
	// check for error
	nt.Must(nt.WatchForAllSyncs())

	// Should have been pruned
	nt.Must(nt.Watcher.WatchForNotFound(kinds.Namespace(), namespace.Name, ""))

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

func TestDeclare(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Adding namespace to Git")
	namespace := k8sobjects.NamespaceObject("bookstore",
		core.Annotation("season", "summer"), core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Remove label for declared namespace")
	updatedNamespace := k8sobjects.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", updatedNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("update namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation("season", "summer"),
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		)))

}

func TestAddUpdateAdd(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add namespace to Git")
	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// The reason we need to stop the webhook here is that the webhook denies a request to modify Config Sync metadata
	// even if the resource has the `client.lifecycle.config.k8s.io/mutation` annotation.
	nomostest.StopWebhook(nt)
	// Stopping the webhook causes the reconciler to restart. Wait so that we aren't
	// racing with the applier and are actually testing the remediator.
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Deployment(),
			core.RootReconcilerName(nomostest.DefaultRootSyncID.Name), configsync.ControllerNamespace,
			testwatcher.WatchPredicates(
				testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
				testpredicates.DeploymentMissingEnvVar(reconcilermanager.Reconciler, reconcilermanager.WebhookEnabled),
			))
	})
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
			testwatcher.WatchPredicates(testpredicates.MissingAnnotation(metadata.LifecycleMutationAnnotation)))
	})
	nt.Must(tg.Wait())

	nt.T.Log("Add the 'ignore annotation' annotation to the namespace using kubectl")
	// Delete a Config Sync annotation
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s=`%s`", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate --overwrite namespace bookstore %s=`%s`` error %v %s, want return nil", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation, err, out)
	}
	// nsObj := namespace.DeepCopy()
	// nsObj.Annotations[metadata.LifecycleMutationAnnotation] = metadata.IgnoreMutation
	// nt.Must(nt.KubeClient.Apply(nsObj))

	time.Sleep(10 * time.Second)

	// Test remediator
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace.Name, "",
		testwatcher.WatchPredicates(
			testpredicates.MissingAnnotation(metadata.LifecycleMutationAnnotation),
		)))

	// //TODO: Tests applier?
	// nt.T.Log("Add another namespace to Git to run the applier")
	// nsObj2 := k8sobjects.NamespaceObject("new-ns")
	// nt.Must(rootSyncGitRepo.Add("acme/ns2.yaml", nsObj2))
	// nt.Must(rootSyncGitRepo.CommitAndPush("add another namespace"))

	// nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), nsObj.Name, "",
	// 	testwatcher.WatchPredicates(
	// 		testpredicates.HasAnnotation("season", "winter"),
	// 	)))
}
