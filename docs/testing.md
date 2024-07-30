# Testing

### Unit tests
Unit tests are small focused tests that runs quickly. Run them with:

```
make test
```

### E2E tests

Config Sync also has e2e tests. These can be run on [kind] or GKE and can take
a long time to finish.

The e2e tests will use the most recently built manifests on your local filesystem,
which are written to the `.output/staging` directory. These are created
when running `make` targets such as `build-manifests` and `config-sync-manifest`.
See [building from source](#build-from-source) for more information.

For the complete list of arguments accepted by the e2e tests, see [flags.go](../e2e/flags.go)
or invoke the e2e tests with the `--usage` flag.
```shell
go test ./e2e/testcases/... --usage --e2e -v
```

Below is a non-exhaustive list of some useful arguments for running the e2e tests.
These can be provided on the command line with `go test` or with program arguments in your IDE.
The test output can be very verbose, so running the tests in an IDE can help with
parsing the output using the IDE frontend.

- `--usage` - If true, print usage and exit.
- `--e2e` - If true, run end-to-end tests. (required to run the e2e tests).
- `--debug` - If true, do not destroy cluster and clean up temporary directory after test.
- `--share-test-env` - Specify that the test is using a shared test environment instead of fresh installation per test case.
- `--test-cluster` - The cluster config used for testing. Allowed values are: `kind` and `gke`.
- `--num-clusters` - Number of clusters to run tests on in parallel. Overrides the `--test.parallel` flag.
- `--create-clusters` - Whether to create clusters in the test framework. Allowed values are [`true`, `lazy`, `false`]. If set to `lazy`, the tests will adopt an existing cluster.
- `--destroy-clusters` - Whether to destroy clusters after test execution. Allowed values are [`true`, `auto`, `false`]. If set to `auto`, the tests will only destroy the cluster if it was created by the tests.

Here are some useful flags from [go test](https://pkg.go.dev/cmd/go#hdr-Testing_flags):
- `--test.v` - More verbose output.
- `--test.run` -- Run only tests matching the provided regular expression.

### E2E tests (kind)

This section provides instructions on how to run the e2e tests on [kind].

#### Prerequisites

Install [kind]
```shell
make install-kind
```

- This will put kind in `$(go env GOPATH)/bin`. This directory will need to be added to `$PATH` if it isn't already.
- After upgrading kind, you usually need to delete all your existing kind clusters so that kind functions correctly.
- Deleting all running kind clusters usually fixes kind issues.

#### Quickstart

The following example shows how to run the e2e test case named `TestNamespaceGarbageCollection`.
The test name can be replaced with [any desired regex pattern](https://pkg.go.dev/cmd/go#hdr-Testing_flags).
The entire test suite takes a long time to run, so it's generally recommended to
run targeted tests.

```shell
make config-sync-manifest-local
go test ./e2e/testcases/... --e2e --test.v --test.run TestNamespaceGarbageCollection
```

#### Running E2E tests on kind

Build everything and run all e2e tests. This will take a long time, but can be
tuned using `KIND_E2E_TIMEOUT` and `KIND_NUM_CLUSTERS`.
```
make test-e2e-kind KIND_E2E_TIMEOUT=5h KIND_NUM_CLUSTERS=5
```

To execute e2e tests locally with kind, build and push the Config Sync
images to the local kind registry and then execute tests using `go test`.
Running the tests directly using `go test` gives more flexibility with the
test parameters and allows running smaller subsets of tests.
```shell
make config-sync-manifest-local
go test ./e2e/testcases/... --e2e --debug --test.v --test.run (test name regexp)
```

To use already existing images without building everything from scratch, rebuild
only the manifests and then rerun the tests.
```shell
make build-manifests IMAGE_TAG=<tag>
go test ./e2e/testcases/... --e2e <additional-options>
```

### E2E tests (GKE)

This section provides instructions on how to run the e2e tests on GKE.

#### Prerequisites

Follow the [instructions to provision a dev environment].

To execute e2e multi-repo tests with a GKE cluster, build and push the Config Sync
images to GCR and then use go test. The images will be pushed to the GCP project
from you current gcloud context.

```shell
# Ensure gcloud context is set to correct project
gcloud config set project <PROJECT_ID>
# Build images/manifests and push images
make config-sync-manifest
```

#### Running E2E tests on GKE (test-generated GKE clusters)

The e2e test scaffolding supports creating N clusters before test execution and
reusing them between tests. This enables running the tests in parallel, where the
number of clusters equals the number of test threads. The clusters can be configured
to be deleted or orphaned after test execution.

Provide the `--create-clusters` option with:
- `true` - always create clusters with tests (error if already exist)
- `lazy` - create or adopt cluster if already exists
- `false` - never create clusters with tests (error if not found)

Provide the `--destroy-clusters` option with:
- `true` - always destroy clusters after tests
- `auto` - only destroy clusters if they were created by the tests
- `false` - never destroy clusters after tests

```shell
# GKE cluster options can be provided as command line flags
go test ./e2e/testcases/... --e2e --test.v --share-test-env --gcp-project=<PROJECT_ID> --test-cluster=gke \
  --cluster-prefix=${USER}-test --create-clusters=lazy --destroy-clusters=false --num-clusters=5 --gcp-zone=us-central1-a \
  --test.run (test name regexp)
```

#### Running E2E tests on GKE (pre-provisioned GKE clusters)

The e2e test scaffolding also supports accepting the name of a pre-provisioned
cluster. It will generate the kubeconfig based on the inputs provided. This mode
only supports running a single test thread against a single cluster. The cluster
will not be deleted at the end of the test execution.

```shell
# GKE cluster options can also be provided as env vars
export GCP_PROJECT=<PROJECT_ID>
export GCP_CLUSTER=<CLUSTER_NAME>
# One of GCP_REGION and GCP_ZONE must be set (but not both)
export GCP_REGION=<REGION>
export GCP_ZONE=<ZONE>
# Run the tests with image prefix/tag from previous step and desired test regex
go test ./e2e/testcases/... --e2e --debug --test.v --share-test-env=true --test-cluster=gke --test.run (test name regexp)
```

[kind]: https://kind.sigs.k8s.io/
[instructions to provision a dev environment]: ../e2e/testinfra/terraform/README.md
