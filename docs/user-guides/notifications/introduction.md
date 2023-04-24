# Introduction

Config Sync supports sending notifications to external services such as Email,
Slack, Github, etc. These can be configured to trigger on a variety of events
such as syncing a new commit, a sync failure, etc. The feature is built using
[argoproj/notifications-engine](https://github.com/argoproj/notifications-engine).
The underlying API is therefore similar to the notification API of ArgoCD, with
some slight differences when it comes interfacing with Config Sync's
`RootSync`/`RepoSync` API.

The notification feature is configurable for each `RootSync` and `RepoSync` object.
This gives cluster administrators (`RootSync`) and application administrators (`RepoSync`)
alike the autonomy and insulation to configure notification templates and credentials
independently.

See [Getting Started](./getting-started.md) for an introduction on how to use the
feature.
