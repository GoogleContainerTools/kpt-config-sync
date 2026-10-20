# Poll until status is complete

* Author(s): ahmadalguydi
* Approver:
* Status: provisional

## Summary

Add `--poll-until` targets to `nomos status` so automation can wait either for
RootSync and RepoSync objects to finish syncing or for their reported managed
resources to become current. The command prints the initial status immediately,
then refreshes it until the selected target is reached or the caller's external
timeout terminates the process.

## Motivation

Issue [#2006](https://github.com/GoogleContainerTools/config-sync/issues/2006)
requests a bounded way to wait for eventual reconciliation without requiring
scripts to parse repeated status output.

## Design Overview

The existing `--poll` loop is extended with optional completion targets:
`--poll-until=synced` waits until each selected repository reports `SYNCED`
without errors; `--poll-until=current` adds the requirement that every reported
managed resource is `Current`. `complete` remains an alias for `current`.
A stalled, pending, reconciling, unavailable, or errored repository keeps
polling. A `Failed` managed resource stops `current` polling with an error
instead of waiting indefinitely. An `Unknown` resource does not count as
current; callers that only need successful Config Sync application can use
`synced` instead. Invalid values fail before any cluster request.

The `--name` filter also scopes the completion check. A requested name must be
present in at least one queried cluster before polling can succeed. Reachable
clusters with no sync objects do not block other clusters from completing.

Either completion target uses a five-second interval when `--poll` is omitted.
An explicit `--poll` interval continues to take precedence. Existing commands
without `--poll-until` retain their current one-shot or indefinite polling
behavior.

The `current` target checks managed-resource status independently of the
`--resources` display flag. A repository with no managed resources is complete
once its sync status is `SYNCED`. `Unknown`, `InProgress`, and other non-current
states keep polling, while `Failed` returns an error. The `synced` target does
not wait for managed-resource status.

## User Guide

Run a status check once as before:

```shell
nomos status
```

Wait until all repositories have finished syncing, refreshing every 10 seconds:

```shell
timeout 2m nomos status --poll=10s --poll-until=synced
```

Wait for all reported managed resources to reach `Current`:

```shell
timeout 2m nomos status --poll=10s --poll-until=current
```

When the requested state is reached, the command exits successfully after
printing that status. `current` exits with an error if any resource reports
`Failed`. If the external timeout expires first, the process is terminated and
the caller can treat that as a reconciliation timeout.

## Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| Polling can increase API traffic. | Require an explicit completion mode and use a conservative five-second default; callers can choose a longer interval. |
| A cluster with no sync objects could hold multi-cluster polling open. | Ignore empty clusters while checking other clusters; require a requested `--name` to appear before success. |
| A resource may never report kstatus `Current`. | Offer `synced` for apply completion and `current` for resource readiness; fail fast for `Failed` resources. |
| Existing users depend on indefinite `--poll`. | Preserve the existing loop unless `--poll-until` is provided. |

## Test Plan

Unit tests cover `synced` and `current` targets, name filtering, empty clusters,
current, unknown, and failed resource states, plus validation of supported flag
values. Existing status tests continue to exercise rendering and cluster-state
collection.

## Open Issues/Questions

### Should more completion states be supported?

Resolution: Yes. `synced` covers Config Sync application, and `current` covers
managed-resource readiness. `complete` is retained as a compatibility alias for
`current`.

## Alternatives Considered

### Shell-side output parsing

Parsing repeated `nomos status` output is fragile and requires callers to know
the output format. A structured completion predicate keeps the behavior inside
the CLI while preserving its human-readable output.
