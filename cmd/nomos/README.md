# Nomos CLI Development

We distribute a binary called `nomos` for allowing users to get information
about their nomos repository and the status of Anthos Configuration Management
on their clusters.

## CLI Patterns

1. **Structure.** The base `nomos` command prints basic usage and functionality
is implemented in sub-commands.

   ```nomos sub-command [options...]```

2. **Cobra.** We use [Cobra](https://github.com/spf13/cobra) to define
subcommands and flags. See [main.go](main.go).

3. **Multi-Cluster.** Commands default to showing information for all
available clusters, with the option of limiting the scope to a cluster or set of
clusters. We assume a 1:1 correspondence between clusters and context, and if
displayed in a column these are called `CONTEXT`. Cluster name and context name
are not necessarily the same string.

4. **Orthogonality.** Subcommands provide
[orthogonal](https://en.wikipedia.org/wiki/Orthogonality_(programming))
functions and information. Two commands should avoid showing the same
information or providing alternative ways of doing the same thing.

5. **Errors.** Tables display `ERROR` or an enum from a library that expresses
the error in at most three words. For example, `VET ERROR` or `NOT INSTALLED`.
Whenever tables display an error state, the CLI prints the full error message
below the table, per-context if relevant.

6. **Consistency.** Commands use consistent flags, output formatting, and
terminology.

7. **Minimalism.** Commands provide the minimum features to support
requirements. Where possible, we prune unnecessary flags and features which do
not provide customer value.

8. **Long descriptions.** Command long descriptions are complete sentences 
beginning with a capital letter.

9. **Flag descriptions.** As with `kubectl get --help`, flag descriptions begin
with a capital letter and end with a period.

## Development Patterns

1. **Design Doc.** Authors of new commands and significant changes to
existing commands write a design docs detailing the functionality of the new
sub-command. Relevant team members must approve these docs before
implementation.

2. **DRY (Don't repeat yourself).** Authors extract parallel code in commands to
libraries.

3. **Markdown Doc.** Authors of CLI commands (and updates) write and update a
`README.md` file with the state of the command.

4. **Feature Flags.** Authors hide CLI commands behind compile-time feature
flags until released. These commands are functional, but not displayed to users.
Set `cobra.Command.Hidden` to true.

5. **Tabular Library.** The CLI prints tabular output with the Kubernetes
library.
