# Development
This doc describes how to set up your development environment for Config Sync.

## Requirements
You must have the following tools:
* [go]
* [git]
* [make]
* [docker]

## Checkout the code
The first step is to check out the code for Config Sync to your local
development environment. We recommend that you [create your own fork], but we will
keep things simple here.

```
git clone git@github.com:GoogleContainerTools/kpt-config-sync.git
cd kpt-config-sync
```

## Run tests

### Unit tests
Unit tests are small focused test that runs quickly. Run them with:
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
go test ./e2e/... --usage
```

Below is a non-exhaustive list of some useful arguments for running the e2e tests.
These can be provided on the command line with `go test` or with program arguments in your IDE.

- `--usage` - If true, print usage and exit.
- `--e2e` - If true, run end-to-end tests. (required to run the e2e tests).
- `--debug` - If true, do not destroy cluster and clean up temporary directory after test.
- `--share-test-env` - Specify that the test is using a shared test environment instead of fresh installation per test case.
- `--test-cluster` - The cluster config used for testing. Allowed values are: `kind` and `gke`.
- `--num-clusters` -- Number of clusters to run tests on in parallel (only available for [kind]). Overrides the `--test.parallel` flag.

Here are some useful flags from [go test](https://pkg.go.dev/cmd/go#hdr-Testing_flags):
- `--test.v` - More verbose output.
- `--test.run` -- Run only tests matching the provided regular expression.

### E2E tests (kind)

This section provides instructions on how to run the e2e tests on [kind].

#### Prerequisites

Install [kind]
```shell
# install the kind version specified in https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/scripts/docker-registry.sh#L50
go install sigs.k8s.io/kind@v0.14.0
```

- This will put kind in `$(go env GOPATH)/bin`. This directory will need to be added to `$PATH` if it isn't already.
- After upgrading kind, you usually need to delete all your existing kind clusters so that kind functions correctly.
- Deleting all running kind clusters usually fixes kind issues.

#### Running E2E tests on kind

Run all of the tests (this will take a while):
```
make test-e2e-go-multirepo
```

To execute e2e multi-repo tests locally with kind, build and push the Config Sync
images to the local kind registry and then execute tests using go test.
```shell
make config-sync-manifest-local
go test ./e2e/... --e2e --debug --test.v --test.run (test name regexp)
```

To use already existing images without building everything from scratch, rebuild
only the manifests and then rerun the tests.
```shell
make build-manifests IMAGE_TAG=<tag>
go test ./e2e/... --e2e <additional-options>
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
number of clusters equals the number of test threads. The clusters will be
deleted after the test execution.

```shell
# GKE cluster options can be provided as command line flags
go test ./e2e/... --e2e --test.v --share-test-env --gcp-project=<PROJECT_ID> --test-cluster=gke \
  --create-clusters --num-clusters=5 --gcp-zone=us-central1-a \
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
go test ./e2e/... --e2e --debug --test.v --share-test-env=true --test-cluster=gke --test.run (test name regexp)
```

## Build

The make targets use default values for certain variables which can be
overridden at runtime. For the full list of variables observed by the make
targets, see [Makefile](../Makefile). Below is a non-exhaustive list of some
useful variables observed by the make targets.

- `REGISTRY` - Registry to use for image tags. Defaults to `gcr.io/<gcloud-context>`.
- `IMAGE_TAG` - Version to use for image tags. Defaults to `git describe`.

> **_Note:_**
The full image tags are constructed using `$(REGISTRY)/<image-name>:$(IMAGE_TAG)`.

Here is an example for how these can be provided at runtime:
```shell
make build-images IMAGE_TAG=latest
```

### Check build status

The following command provides information on the current build status. It
parses the local manifests and checks for the build status of docker images
referenced in the manifest.

```shell
make build-status
```

### Use postsubmit artifacts

After a change is submitted to one of the official branches (e.g. `main`, `v1.15`, etc.),
a postsubmit job is triggered which publishes build artifacts. If you have checked
out a commit from the history of such a branch, you can pull the manifests and
use them directly.

```shell
make pull-gcs-postsubmit
```

This will pull the manifests from GCS and store them in `.output/staging/oss`
(the same location as `make config-sync-manifest`). These can then be used for
running e2e tests or deploying directly to a cluster.

To pull and deploy the checked out commit:

```shell
make deploy-postsubmit
```

### Build from source

Config Sync can be built from source with a single command:

```shell
make config-sync-manifest
```

This will build all the docker images needed for Config Sync and generate
the manifests needed to run it. The images will by default be uploaded to 
Google Container Registry under your current gcloud project and the manifests
will be created in `.output/staging/oss` under the Config Sync directory.

### Subcomponents
Individual components of Config Sync can be built/used with the following
commands. By default images will be tagged for the GCR registry in the current
project. This can be overridden by providing the `REGISTRY` variable at runtime.

Build CLI (nomos):
```shell
make build-cli
```
Build Manifests:
```shell
make build-manifests
```
Build Docker images:
```shell
make build-images
```
Push Docker images:
```shell
make push-images
```
Pull Docker images:
```shell
make pull-images
```
Retag Docker images:
```shell
make retag-images \
 OLD_REGISTRY=gcr.io/baz \
 OLD_IMAGE_TAG=foo \
 REGISTRY=gcr.io/bat \
 IMAGE_TAG=bar 
```

## Run
Running Config Sync is as simple as applying the generated manifests to your
cluster (from the Config Sync directory):

```
kubectl apply -f .output/staging/oss
```

The following make target builds Config Sync and installs it into your cluster:

```
make run-oss
```


[go]: https://go.dev/doc/install
[git]: https://docs.github.com/en/get-started/quickstart/set-up-git
[make]: https://www.gnu.org/software/make/
[docker]: https://www.docker.com/get-started
[create your own fork]: https://docs.github.com/en/get-started/quickstart/fork-a-repo
[kind]: https://kind.sigs.k8s.io/
[instructions to provision a dev environment]: ../e2e/testinfra/terraform/README.md
