# Templates Catalog

This document provides a catalog of recommended [templates] for common use cases
in Config Sync. This catalog uses email as an example, but these can be modified
to work for other services as well. See the [services] reference doc for the
template configuration API of each service.

The set of [built-in functions](./functions.md) can be invoked from templates.

## reconciler-created

```yaml
  template.reconciler-created: |
    message: &message {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} has been created.
    email:
      subject: *message
```

## reconciler-deleted

```yaml
  template.reconciler-deleted: |
    message: &message {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} has been deleted.
    email:
      subject: *message
```

## reconciler-stalled

```yaml
  template.reconciler-stalled: |
    message: |
      {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} is Stalled.
      
      {{range $index, $e := .sync.status.conditions}}{{if eq $e.type "Stalled"}}{{$e.message}}{{end}}{{end}}
    email:
      subject: |
        {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} is Stalled.
```

## reconciler-reconciling

```yaml
  template.reconciler-reconciling: |
    message: &message The reconciler Deployment for {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} is reconciling.
    email:
      subject: *message
```

## sync-synced

This example shows how to configure Github notifications in addition to email.

```yaml
  template.sync-synced: |
    message: &message {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} is synced to {{.sync.status.lastSyncedCommit}}.
    email:
      subject: *message
    github:
      repoURLPath: "{{.sync.spec.git.repo}}"
      revisionPath: "{{.sync.status.lastSyncedCommit}}"
      status:
        state: success
        label: "continuous-delivery/{{.sync.metadata.namespace}}/{{.sync.metadata.name}}"
        targetURL: ""
```

## sync-failed

```yaml
  template.sync-failed: |
    message: |
      {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} failed to sync.

      {{range $index, $e := call .utils.ConfigSyncErrors}}{{$e.ErrorMessage}}\n\n{{end}}
    email:
      subject: |
        {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} failed to sync.
```

## sync-pending

```yaml
  template.sync-pending: |
    message: &message {{.sync.kind}} {{.sync.metadata.namespace}}/{{.sync.metadata.name}} is currently syncing.
    email:
      subject: *message
```

[services]: https://github.com/argoproj/notifications-engine/tree/a2a20923be59e954476c4f051fba0c85ff29e414/docs/services
[templates]: https://github.com/argoproj/notifications-engine/blob/a2a20923be59e954476c4f051fba0c85ff29e414/docs/templates.md
