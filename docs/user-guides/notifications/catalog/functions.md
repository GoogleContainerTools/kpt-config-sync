# Functions

Config Sync defines a set of helper functions which can be invoked from
[triggers](./triggers.md) and [templates](./templates.md). These are intended
to help with writing triggers and templates.

## strings

This is a set of helper functions intended to help with manipulating strings.

### Join

`strings.Join(elems []string, sep string) string`

Executes the built-in [strings.Join](https://pkg.go.dev/strings#Join) function.

### Hash

`strings.Hash(input string) string`

Takes the input string and returns a `sha1` hash of the string which is base64
encoded.

## utils

This is a set of helper functions intended to help with accessing fields of the
`RootSync`/`RepoSync` object.

### LastCommit

`utils.LastCommit() string`

Returns the commit SHA from the `Syncing` condition, defaulting to an empty string.
Useful in scenarios where `status.lastSyncedCommit` may not be set, such as
triggers/templates for failure conditions.

### ConfigSyncErrors

`utils.ConfigSyncErrors() []v1beta1.ConfigSyncError`

Returns a concatenated list of errors from all the status subfields, i.e.
`status.source.errors + status.rendering.errors + status.sync.errors`. Useful
for getting a complete list of error messages on the `RootSync`/`RepoSync`.

