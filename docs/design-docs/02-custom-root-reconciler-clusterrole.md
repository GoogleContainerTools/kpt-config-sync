# Custom RBAC assignments for `RootSync`

* Author(s): @tomasaschan, @sdowell
* Approver: @janetkuo, @karlkfi
* Status: approved

## Summary

This proposal introduces a way to manage the lifecycle of RBAC bindings for
`RootSync` objects.

### Historical Behavior

When creating a reconciler deployment for a `RootSync`, Config Sync historically [creates a
`ClusterRoleBinding`] for the generated `ServiceAccount` granting it `cluster-admin`.

On the other hand, when creating a reconciler deployment for a `RepoSync`, Config Sync
currently creates `RoleBinding` for the generated `ServiceAccount` granting
it `configsync.gke.io:ns-reconciler`. This default `ClusterRole` grants basic permissions required for the `RepoSync`
reconciler to function, but it's up to users to manage additional `RoleBinding`s.

[creates a `ClusterRoleBinding`]: https://github.com/GoogleContainerTools/kpt-config-sync/blob/v1.16.0/pkg/reconcilermanager/controllers/rootsync_controller.go#L808

### Proposed Behavior

The proposed behavior will allow full configuration of which bindings are
created for a `RootSync` reconciler. Config Sync will manage the
lifecycle of all `RoleBinding` and `ClusterRoleBinding` objects declared using
this new API.

## Motivation

There are several common use cases where a platform admin may want to limit the
scope of what a `RootSync` can manage in the cluster. For example,
a platform admin may want to allow a tenant to use a `RootSync` to manage a
subset of cluster-scoped resources.

However, as Config Sync is currently granting `cluster-admin` to all `RootSync`
reconcilers, any custom role bindings are effectively ignored; the `RootSync`
reconciler will, due to the binding added by Config Sync, have access to do
_anything_ regardless.

To follow the [principle of least privilege], one should ensure the reconciler only has
access to deploy the expected resources.

See also [#935].

[principle of least privilege]: https://en.wikipedia.org/wiki/Principle_of_least_privilege
[#935]: https://github.com/GoogleContainerTools/kpt-config-sync/issues/935

## Design Overview

### `RootSync`s

By providing the name of a list user-defined `RoleRef`s for a `RootSync`, a user can
override which role Config Sync binds to. This configuration is exposed as a new field
on `spec.overrides` for a `RootSync`:

```yaml
kind: RootSync
metadata:
  name: my-root-sync
  namespace: config-management-system
spec:
  overrides:
    roleRefs:
    - kind: ClusterRole # Creates a ClusterRoleBinding for ClusterRole my-cluster-role
      name: my-cluster-role
    - kind: ClusterRole # Creates a RoleBinding in my-tenant-namespace for ClusterRole tenant-cluster-role
      name: tenant-cluster-role
      namespace: my-tenant-namespace
    - kind: Role # Creates a RoleBinding in my-tenant-namespace for Role my-tenant-role
      name: my-tenant-role
      namespace: my-tenant-namespace
```

For `RootSync` objects, a default `cluster-admin` `ClusterRoleBinding` will be applied
when `spec.override.roleRefs` is empty or nil, for reverse compatibility.

For convenience, `RootSync` reconcilers will also be bound to a base `ClusterRole`
which gives the reconciler the permissions for basic functionality. This will
be comparable to the pre-existing base ClusterRole for `RepoSync` reconcilers,
which includes permissions such as status writing on `RepoSync` objects. Leaving
this permission to the user to manage would create unneeded toil.

### Lifecycle management

Given this API provides configuration for a list of RoleRefs, it requires some form
of lifecycle management to clean up stale bindings. For example if a user removes
one roleRef from the list of roleRefs, they would reasonably expect that the
binding will be garbage collected.

This essentially requires for the `reconciler-manager` to be able to track an
inventory of bindings that were previously created for a given `RootSync`.
This can be accomplished by applying a label whenever a new binding is created,
and then querying using a label selector on subsequent reconciliation loops.

The following label will be applied to binding objects:
```yaml
metadata:
  labels:
    configsync.gke.io/sync-kind: <RSYNC_KIND>
    configsync.gke.io/sync-name: <RSYNC_NAME>
    configsync.gke.io/sync-namespace: <RSYNC_NAMESPACE>
```

These are the standard labels applied to other objects managed by the `reconciler-manager`
which are associated with a `RootSync` or `RepoSync`.

When a `RootSync` is deleted, the managed `RoleBindings` and `ClusterRoleBindings`
will not be deleted until the reconciler finalizer completes. This ensures that
the `RootSync` reconciler should have the permissions that it needs to run its
finalizer.

## User Guide

### `RootSync`

To use this feature for a `RootSync`, set `spec.overrides.roleRefs` to reference
any number of `ClusterRole` or `Role` objects you wish this `RootSync` to be bound
to. If you wish to create a `RoleBinding` rather than a `ClusterRoleBinding`,
set the `namespace` field of the `roleRef` to the desired Namespace.
You must create the `ClusterRole`/`Role` yourself, but Config Sync will create the
`ClusterRoleBinding`/`RoleBinding` for you.

```yaml
apiVersion: configsync.gke.io/v1beta1
kind: RootSync
metadata:
  name: my-root-sync
  namespace: config-management-system
spec:
  overrides:
    roleRefs:
    - kind: ClusterRole # Create ClusterRoleBinding to my-cluster-role
      name: my-cluster-role
    - kind: ClusterRole # Create RoleBinding to my-tenant-role
      name: my-tenant-role
      namespace: my-ns
    - kind: Role # Create RoleBinding to my-role
      name: my-role
      namespace: my-ns
  # ...
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: my-cluster-role
rules:
# ...
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: my-tenant-role
rules:
# ...
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: my-role
  namespace: my-ns
rules:
# ...
```

**Note** that while the name of the relevant service account will _often_ be predictable
as `root-reconciler-<root-reconciler-name>`, this is not guaranteed by Config Sync. You
can always find the service account by its labels; in particular, the labels
`configsync.gke.io/sync-kind` and `configsync.gke.io/sync-name` should be useful.

## Risks and Mitigations

This feature is entirely backwards-compatible, by means of being opt-in.

## Test Plan

Implementation of this feature will include automated tests that verify the behavior.

## Open Issues/Questions

N/A

## Alternatives Considered

One could imagine exposing settings with slightly different semantics - e.g. a simple
boolean for turning the `ClusterRoleBinding` off, or a setting to change what service
account the reconciler is running with. However, these both put a bigger burden on the
user in order to utilize them even for the simple use cases, which is why changing which
roles to bind to is probably the most user-friendly knob to expose. Exposing an
API which binds to a single `ClusterRole` was also considered, but this was decided
against due to lack of flexibility. The proposed solution allows for binding to
an arbitrary number of `Role`/`ClusterRole` objects, and should fit most expected
use cases for users.

### `RepoSync`

Consideration was also made to extend a similar API for `RepoSync` objects. While
this sort of API would be compatible with the [centralized control pattern], it
would be a breaking change for the [delegated control pattern]. In delegated
control scenarios, a tenant is given permission to create/update RepoSyncs and
create RoleBindings to RepoSync reconcilers. The tenant should only be able to
grant permissions to a RepoSync reconciler for which the tenant has the same
permissions. If the API is surfaced on the RepoSync itself, this makes privilege
escalation possible for tenants using delegated control.

[centralized control pattern]: https://cloud.google.com/anthos-config-management/docs/how-to/multiple-repositories#managed-control
[delegated control pattern]: https://cloud.google.com/anthos-config-management/docs/how-to/multiple-repositories#kubernetes-api
