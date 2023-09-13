# Explicit-only Namespace Strategy for `RootSync`s

* Author(s): @tomasaschan
* Approver: @sdowell, @janetkuo
* Status: provisional

## Summary

Config Sync currently has a concept of "implicit namespaces", where `RootSync`s can manage
namespace-scoped resources without explicitly also defining the `Namespace` resource itself.
In the current implementation, the `RootSync` will create the namespace and add ownership
annotations in the same way as it would if the `Namespace` was explicitly managed.

This can result in resource fights in the case where `RootSync`s implicitly use a namespace,
if that namespace is also explicitly managed by another `RootSync`. This document outlines
a mechanism to change this behavior to instead require explicit declarations of all namespaces
_somewhere_, and allow resources to reconcile without causing resource fights.

## Motivation

Discussion about this was first sparked in [#191][191], where @tomasaschan set out to implement a
solution for this. #207 outlines the use case without linking it to a suggested implementation.

A simplified setup that exhibits the problems this design doc attempts to address, is the following:

* A `RootSync` named `sync-a` declares `namespace-a` explicitly
* A `RootSync` named `sync-x` declares resources in `namespace-a`, but not `namespace-a` itself
* A `RootSync` named `sync-y` also declares resources in `namespace-a`, but not `namespace-a` itself
* The reconciliation order of these syncs cannot be explicitly managed (e.g. because they are in turn
  managed with a `RootSync`)

If _either_ of `sync-x` or `sync-y` create `namespace-a` first, they will assume ownership of the
namespace, and `sync-a` will not be allowed to create it (and, as a result, all its resources inside
the namespace will also fail to be created).

### Objective

* The configuration described above should be supported without resource fights or reconciliation errors
* Users who depend on implicit namespaces without also declaring them explicitly should need ot make
  no changes to their configuration, and see the same behavior as today.

## Design Overview

The idea is to introduce an annotation that can be set on a `RootSync` which defines how to handle
creation of namespaces: `configsync.gke.io/namespace-strategy`. The possible values are

* `implicit` (the default): same behavior as today, including the failure modes triggering this proposal
* `explicit`: namespaces must either exist in the cluster, or be declared in the configuration managed
  by the `RootSync`; no implicit namespace creation happens.
* (not set: same as `implicit`)

It is possible to extend these options with something more complex in the future, for example a
strategy where explicit declarations "take over ownership", similar to the implementation in [#191][191].
This is **out of scope** for the current proposal.

## User Guide

To take advantage of this new feature, add the annotaiton `configsync.gke.io/namespace-strategy: explicit`
to your `RootSync`:

```yaml
apiVersion: configsync.gke.io/v1beta1
kind: RootSync
metadata:
  annotations:
    configsync.gke.io/namespace-strategy: explicit
  name: root-sync
  namespace: config-management-system
spec:
  # ...
```

With this annotation applied, the behavior for a `RootSync` `sync-a` can be understand as follows:

| Namespace declared in `sync-a` | Namespace managed elsewhere | Behavior                                                                                                      |
| ------------------------------ | --------------------------- | ------------------------------------------------------------------------------------------------------------- |
| ✔️                              | ✖️                           | Namespace created and owned by `sync-a`, resources in namespace also created                                  |
| ✖️                              | ✖️/✔️                         | Resources in namespace managed by `sync-a` fail to be applied until namespace has been created from elsewhere |
| ✔️                              | ✔️                           | Resource fight; resolve by removing the namespace configuration from all but one of the `RootSync`s           |

## Risks and Mitigations

This feature is entirely backwards-compatible, by means of being opt-in.

## Test Plan

Implementation of this feature can include automated tests that verify the behavior described in the
table above.

## Open Issues/Questions

N/A

## Alternatives Considered

There was a previous attempt at supporting the motivating use case in [#191][191], which was eventually
rejected due to being too complex, and subtly changing the behavior of implicit namespaces e.g. when
deleting them. Extensive discussion of this is available in that PR, as well as in [this design doc][doc]

[191]: https://github.com/GoogleContainerTools/kpt-config-sync/pull/191
[doc]: https://docs.google.com/document/d/1QK-vMQkcjmgKqaqI7fBBpejwr2eJsWu7lzO1q3PQqe4/edit
