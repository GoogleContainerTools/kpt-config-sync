# Triggers Catalog

This document provides a catalog of recommended [triggers] for common use cases
in Config Sync. These triggers are defined to use [templates from the catalog](./templates.md),
but other templates can be provided (in the `send` block) as well.

The set of [built-in functions](./functions.md) can be invoked from triggers.

## on-reconciler-created

The `RootSync`/`RepoSync` object is created. Triggers once for each newly created
`RootSync`/`RepoSync` object.

```yaml
  trigger.on-reconciler-created: |
    - when: True
      oncePer: sync.metadata.namespace + '/' + sync.metadata.name
      send: [reconciler-created]
```

## on-reconciler-deleted

The `RootSync`/`RepoSync` object is deleted. Triggers once for each deletion of a
`RootSync`/`RepoSync` object.

```yaml
  trigger.on-reconciler-deleted: |
    - when: sync.metadata.deletionTimestamp != nil
      oncePer: sync.metadata.namespace + '/' + sync.metadata.name
      send: [reconciler-deleted]
```

## on-reconciler-stalled

The `RootSync`/`RepoSync` object is stalled. Triggers whenever the `Stalled`
condition is true.

```yaml
  trigger.on-reconciler-stalled: |
    - when: any(sync.status.conditions, {.type == 'Stalled' && .status == 'True'})
      send: [reconciler-stalled]
```

## on-reconciler-reconciling

The reconciler Deployment of the `RootSync`/`RepoSync` object is reconciling.
Triggers whenever the `Reconciling` condition is true.

```yaml
  trigger.on-reconciler-reconciling: |
    - when: any(sync.status.conditions, {.type == 'Reconciling' && .status == 'True'})
      send: [reconciler-reconciling]
```

## on-sync-synced

The `RootSync`/`RepoSync` object has synced a new commit successfully without any errors.
Triggers once for each commit that is successfully synced.

```yaml
  trigger.on-sync-synced: |
    - when: any(sync.status.conditions, {.commit != nil && .type == 'Syncing' && .status == 'False' && .message == 'Sync Completed' && .errorSourceRefs == nil && .errors == nil})
      oncePer: sync.status.lastSyncedCommit
      send: [sync-synced]
```

## on-sync-failed

The `RootSync`/`RepoSync` object failed to be synced. Triggers when a `Syncing`
failure occurs, once for a commit or error message.

```yaml
  trigger.on-sync-failed: |
    - when: any(sync.status.conditions, {.type == 'Syncing' && .status == 'False' && (.errorSourceRefs != nil || .errors != nil)}) 
      oncePer: utils.LastCommit() + '-' + strings.Hash(strings.Join(map(utils.ConfigSyncErrors(), {#.ErrorMessage}), ''))
      send: [sync-failed]
```

## on-sync-pending

The `RootSync`/`RepoSync` object is currently processing the commit. Triggers
once for each syncing process, even if the commit SHA remains the same.

```yaml
  trigger.on-sync-pending: |
    - when: any(sync.status.conditions, {.type == 'Syncing' && .status == 'True'})
      oncePer: utils.LastCommit()
      send: [sync-pending]
```

[triggers]: https://github.com/argoproj/notifications-engine/blob/a2a20923be59e954476c4f051fba0c85ff29e414/docs/triggers.md
