# Custom `ClusterRole` assignments for `RootSync`s

* Author(s): @tomasaschan
* Approver: @sdowell, @janetkuo
* Status: approved

## Summary

When creating a reconciler deployment for a `RootSync`, Config Sync currently [creates a
`ClusterRoleBinding`] for the generated `ServiceAccount` granting it `cluster-admin`.

This proposal introduces a mechanism to customize this behavior.

[creates a `ClusterRoleBinding`]: https://github.com/GoogleContainerTools/kpt-config-sync/blob/199db6dbfa0b9eb1824925c8c687574de095a294/pkg/reconcilermanager/controllers/rootsync_controller.go#L789

## Motivation

If one wants to allow deploying resources from a source where one doesn't have full
control of the contents of the upstream config source, and want to limit what another
team or third party can accomplish in the cluster, one can imagine limiting what
permissions are granted to the `ServiceAccount` running the reconciler.

However, as Config Sync is currently granting `cluster-admin`, any custom role bindings
are effectively ignored; the reconciler will, due to the binding added by Config Sync,
have access to do _anything_ regardless.

To follow the [principle of least privilege], one should ensure the reconciler only has
access to deploy the expected resources.

See also [#935].

[principle of least privilege]: https://en.wikipedia.org/wiki/Principle_of_least_privilege
[#935]: https://github.com/GoogleContainerTools/kpt-config-sync/issues/935

## Design Overview

By providing the name of a user-defined `ClusterRole` for a `RootSync`, a user can
override which role Config Sync binds to. This configuration is exposed as a new field
on `spec.overrides` for a `RootSync`:

```yaml
spec:
  overrides:
    clusterRole: my-custom-role
```

This role is defaulted to `cluster-admin` in order to stay backwards compatible.

For the case where a single `ClusterRole` is not expressive enough to configure the
permissions a user want, we also allow the special sentinel value `%none%`, which
disables creating the `ClusterRoleBinding` entirely. The value is chosen so that it can
never coincide with the name of an existing `ClusterRole`, since they [do not allow `%`
signs in their names].

[do not allow `%` signs in their names]: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#path-segment-names

## User Guide

To take advantage of this new feature, set `spec.overrides.clusterRole` to the name of a
`ClusterRole` you wish this `RootSync` to be reconciled under; you must create this role
yourself, but Config Sync will create the `ClusterRoleBinding` for you.

```yaml
apiVersion: configsync.gke.io/v1beta1
kind: RootSync
metadata:
  name: root-sync
  namespace: config-management-system
spec:
  overrides:
    clusterRole: my-cluster-role
  # ...
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: my-cluster-role
rules:
# ...
```

You can also use the special value `%none%` and disable automatic binding to any role.
The reconciler will then run with effectively no permissions, until you manually create
some role or cluster role bindings for it.

**Note** that while the name of the relevant service account will _often_ be predictable
as `root-reconciler-<root-reconciler-name>`, this is not guaranteed by Config Sync. You
can always find the service account by its labels; in particular, the labels
`configsync.gke.io/sync-kind` and `configsync.gke.io/sync-name` should be useful.

## Risks and Mitigations

This feature is entirely backwards-compatible, by means of being opt-in.

## Test Plan

Implementation of this feature can includes automated tests that verify the behavior.

## Open Issues/Questions

N/A

## Alternatives Considered

One could imagine exposing settings with slightly different semantics - e.g. a simple
boolean for turning the `ClusterRoleBinding` off, or a setting to change what service
account the reconciler is running with. However, these both put a bigger burden on the
user in order to utilize them even for the simple use cases, which is why changing which
role to bind to is probably the most user-friendly knob to expose. With the inclusion of
a sentinel value to create no binding, the first of these alternatives is effectively
supported, if through a slightly worse API. 
