# Getting Started

This document provides an introduction on how to enable and configure Config Sync's
notification feature. This is intended to provide an overview of a typical use case,
however there are many ways to configure notifications to fit your needs. The
notification API is highly configurable by design, so you can design your notification
schema as you see fit. We also provide a catalog of [trigger](./catalog/triggers.md)
and [template](./catalog/templates.md) configurations for common use cases.

Setting up the notification feature requires creating a `ConfigMap` with [services],
[triggers], and [templates] defined.

## Installation

To use the Config Sync notifications feature, you will need to install a version
of Config Sync on your cluster which includes the notification feature. At the
time of writing, this is limited to the [feature/notification branch].

### Using postsubmit artifacts

After a commit is submitted to `feature/notification`, a postsubmit job is triggered
which publishes build artifacts. These artifacts can be used to install Config
Sync.

```shell
mkdir config_sync
gsutil cp gs://kpt-config-sync-ci-postsubmit/d857aad2659189ffaa17fc73651433dd4c778e63/*.yaml config_sync/
kubectl apply -f config_sync/config-sync-manifest.yaml
```
Note: This example includes a valid commit SHA, but other commit SHAs from the
feature branch can be used as well.

### Building from source

Alternatively, you can checkout the `feature/notification` branch locally and
follow the instructions to [build from source](../../installation.md).

## Concepts

### Services

[Services] define _where_ notifications are delivered. The link provides a list
of supported services and their respective API configuration.

### Triggers

[Triggers] define _when_ notifications are delivered. Triggers and templates
are defined by the user in a `ConfigMap` and provided to Config Sync using
`spec.notificationConfig.configMapRef`.

### Templates

[Templates] define the _contents_ of a notification when it is delivered. Triggers
and templates are defined by the user in a `ConfigMap` and provided to Config Sync using
`spec.notificationConfig.configMapRef`. 

### Subscriptions

Subscriptions are used to register a tuple of (service, trigger, template) for
a resource. This combination defines a complete notification configuration, such
that the notification controller will deliver a notification:
- with the contents defined by the template
- when the trigger is fired
- to the service

Subscriptions are created by adding an annotation to a `RootSync` or `RepoSync`
object with the following form:
```yaml
metadata:
  annotations:
    notifications.configsync.gke.io/subscribe.<trigger>.<service>: <recipient>
```

## Example: Configuring notifications for Github and Email whenever a new commit is synced

First we must create a `ConfigMap` and `Secret` which define the notification
configurations. The `ConfigMap` and `Secret` must be created in the same namespace
as the `RootSync`/`RepoSync` object. In this example we set up notifications for a
`RootSync`, so we create the `ConfigMap` and `Secret` in the `config-management-system`
namespace. We define the configuration for:
- A `github` service so that we can send notifications to Github.
- An `email` service so that we can send notifications to email.
- An `on-sync-synced` trigger and `on-synced` template so that we can send notifications
  for whenever a new commit is synced.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: notification-cm
  namespace: config-management-system
data:
  # define a service for github. Set appID/installationID for your github app
  service.github: |
    appID: <appID>
    installationID: <installationID>
    privateKey: $github-privateKey
  # define a service for email. This configures notifications for email (gmail in this case)
  service.email.gmail: |
    username: $email-username
    password: $email-password
    host: smtp.gmail.com
    port: 465
    from: $email-username
  # define an on-sync-synced trigger. This will trigger a notification when a new commit is successfully synced
  trigger.on-sync-synced: |
    - when: any(sync.status.conditions, {.commit != nil && .type == 'Syncing' && .status == 'False' && .message == 'Sync Completed' && .errorSourceRefs == nil && .errors == nil})
      oncePer: sync.status.lastSyncedCommit
      send: [sync-synced]
  # define an on-synced template. This configures the contents of the notifications
  template.sync-synced: |
    message: |
      {{.sync.kind}} {{.sync.metadata.name}} is synced to {{.sync.status.lastSyncedCommit}}!
    email:
      subject: |
        {{.sync.kind}} {{.sync.metadata.name}} is synced.
    github:
      repoURLPath: "{{.sync.spec.git.repo}}"
      revisionPath: "{{.sync.status.lastSyncedCommit}}"
      status:
        state: success
        label: "continuous-delivery/{{.sync.metadata.name}}"
        targetURL: ""
```

The values preceded by `$` are credential values and must be stored in a kubernetes
`Secret`. The `Secret` must also be created in the same namespace:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: notification-secret
  namespace: config-management-system
stringData:
  # set the value for each of these
  email-username: <username>
  email-password: <password>
  github-privateKey: <private-key>
type: Opaque
```

The values of `email-username` and `email-password` define the credentials for the
account which will send the notifications. The value of `github-privateKey` is the
[private key for a Github App](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/managing-private-keys-for-github-apps).

With the `ConfigMap` and `Secret` created, we can now configure the `RootSync` to
use them and enable notifications.

```yaml
apiVersion: configsync.gke.io/v1beta1
kind: RootSync
metadata:
  name: root-sync-1
  namespace: config-management-system
  # To register a notification for a RSync, add an annotation of the following form:
  # notifications.configsync.gke.io/subscribe.<trigger>.<service>: <recipient>
  # The <trigger> and <service> referenced should be included in notificationConfig.configmapRef
  annotations:
    # this annotation registers notifications for gmail to be sent to <recipient>
    notifications.configsync.gke.io/subscribe.on-sync-synced.gmail: <recipient-email>
    # this annotation registers notifications for github commit statuses
    notifications.configsync.gke.io/subscribe.on-sync-synced.github: ""
spec:
  sourceFormat: unstructured
  git:
    repo: <sync-repo>
    branch: <sync-branch>
    auth: none
  notificationConfig:
    secretRef: # this tells Config Sync to use the Secret we created
      name: notification-secret
    configmapRef: # this tells Config Sync to use the ConfigMap we created
      name: notification-cm
```

This configuration informs Config Sync to enable the notification feature. Config
Sync will use the `notificationConfig` to ingest the `ConfigMap` and `Secret` that
we created in the previous step. The annotations register various combinations
of notifications with the notification controller. In this example, we configure
the `on-sync-synced` trigger to deliver notifications to both the `gmail` and
`github` services which we defined in the `ConfigMap`.


## Debugging notifications

Config Sync uses a Custom Resource Definition called `Notification` in order to
surface the status of the notification controller. This includes information
such as errors and which notifications have been delivered for a particular commit.

The following command can be used to get the notification status. Note the name
and namespace of the `Notification` are the same as the `RootSync`/`RepoSync`.
```shell
kubectl get notifications.configsync.gke.io -A -oyaml
```

For deeper debugging, you can also read the logs of the notification controller.
The notification controller runs as a sidecar container in the reconciler
deployment. The following command can be used to read the logs:
```shell
kubectl logs -n config-management-system <RECONCILER_NAME> -c notification
```
Where `RECONCILER_NAME` is the name of the reconciler deployment. The reconciler
deployment will be of the form `root-reconciler-${ROOT_SYNC_NAME}-*` for `RootSync`
and `ns-reconciler-${REPO_SYNC_NAME}-*` for `RepoSync`.


[services]: https://github.com/argoproj/notifications-engine/tree/a2a20923be59e954476c4f051fba0c85ff29e414/docs/services
[triggers]: https://github.com/argoproj/notifications-engine/blob/a2a20923be59e954476c4f051fba0c85ff29e414/docs/triggers.md
[templates]: https://github.com/argoproj/notifications-engine/blob/a2a20923be59e954476c4f051fba0c85ff29e414/docs/templates.md
[feature/notification branch]: https://github.com/GoogleContainerTools/kpt-config-sync/tree/feature/notification
