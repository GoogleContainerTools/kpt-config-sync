# Deletion Propagation

Historically, Config Sync has handled applying and pruning resource objects
based on their presence or absence from a source of truth (Git, OCI, Helm).

In Config Sync v1.15.0, a new feature was added to delete all previously applied
objects, even if they still exist in the source of truth. This feature is
called Deletion Propagation, because it allows you to enable the feature on a
Sync object (RootSync or RepoSync) and delete the Sync object, then Config Sync
will automatically propagate that deletion to every object managed by that Sync
object, according to its current inventory (ResourceGroup object).

This feature is useful in a number of cases, like migrating to a new namespace
or cluster, cleaning up after a demo or experiment, uninstalling an application
or other software package, etc.

## Usage

Deletion Propagation is disabled by default, when the annotation is not present.

To enable Deletion Propagation, set the following annotation on the RootSync or
RepoSync object:

```yaml
configsync.gke.io/deletion-propagation-policy: Foreground
```

To disable Deletion Propagation, either remove the annotation or explicitly
change the value to `Orphan`:

```yaml
configsync.gke.io/deletion-propagation-policy: Orphan
```

## Example

To delete all the objects managed by the RootSync named `example`, first patch
the RootSync to enable Deletion Propagation:

```bash
kubectl patch RootSync example --namespace config-management-system --type merge \
  --patch '{"metadata":{"annotations":{"configsync.gke.io/deletion-propagation-policy":"Foreground"}}}'
```

Then delete the RootSync and wait for it to be deleted:

```bash
kubectl delete RootSync example --namespace config-management-system --wait
```

## Implementation Details

Deletion Propagation is implemented using
[Kubernetes Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/).
In this case, when the user adds the annotation to the Sync object, the
reconciler watching that Sync object will see the annotation and add a Finalizer
to the Sync object. This Finalizer tells Kubernetes to delay deletion of the
Sync object until the reconciler has had a chance to perform deletion
propagation. When the reconciler is finished, it will remove the Finalizer,
allowing Kubernetes to finish deleting the Sync object.

## Possible Deadlock

If you enable Deletion Propagation on a RepoSync, and you then delete the
Namespace that the RepoSync is in before the RepoSync has finished deleting, it
can cause a deadlock which can block deletion of the RepoSync and Namespace.
This is because the RepoSync's reconciler often depends on resource objects in
that same namespace, namely the RoleBinding that grants the reconciler
permission to manage resources in that namespace.

One way to mitigate this problem is to manage the RepoSync, its RoleBinding,
and the Namespace using a RootSync and a single source of truth. This way you
can add an explicit dependency from the RepoSync to the RoleBinding using the
`depends-on` annotation.

## Why not use Kubernetes Garbage Collection?

Config Sync Deletion Propagation uses similar terminology to
[Kubernetes Garbage Collection](https://kubernetes.io/docs/concepts/architecture/garbage-collection/).
However, the implementations are pretty different.

There are multiple reasons why Config Sync doesn't use Kubernetes Garbage
Collection to implement Deletion Propagation:
1. Kubernetes Garbage Collection does not support cross-namespace owner
    references, which are required when using a RootSync to manage objects in
    a different namespace from the RootSync.
2. Kubernetes Garbage Collection uses Background deletion by default, but for
    Config Sync we wanted to keep the existing behavior reverse compatible.
    So Config Sync uses Orphan as the default behavior instead.
3. Kubernetes Garbage Collection allows the client that makes the delete call
    to specify what strategy to use (Foreground, Background, Orphan), but this
    makes the feature hard to configure declaratively. For Config Sync, we
    used an annotation instead to make it possible to enable Deletion
    Propagation with GitOps, when using a Sync object to manage other Sync
    objects.
4. Kubernetes Garbage Collection supports Background deletion, but Background
    deletion is not possible to implement with the current Config Sync design,
    because the reconciler-manager deletes the reconciler after its Sync object
    is deleted, which would interrupt its Deletion Propagation. It might be
    possible to support Background Deletion Propagation in the future, but it
    will require changes in the reconciler-manager to use a Finalizer to delete
    reconcilers and other objects, instead of Kubernetes Garbage Collection.

Similarities:
- Both support Foreground and Orphan strategies
- Both use a Finalizer for Foreground deletion

Differences:
- Only Kubernetes Garbage Collection supports the Background strategy
- Config Sync Deletion Propagation doesn't have a `blockOwnerDeletion` feature
    to block deletion of the Sync object until all its managed objects are
    deleted. This is the default behavior when Deletion Propagation is enabled. 
