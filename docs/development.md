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

### E2E tests (kind)

Config Sync also has e2e tests. These run on [kind] and can take a long time
to finish.

#### Prerequisites

Install [kind]
```shell
# install the kind version specified in https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/scripts/docker-registry.sh#L50
go install sigs.k8s.io/kind@v0.14.0
```

- This will put kind in $(go env GOPATH)/bin. This directory will need to be added to $PATH if it isn't already.
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
make __push-local-images
go test ./e2e/... --e2e --multirepo --debug --test.v --image-tag=(IMAGE_TAG) --test.run (test name regexp)
```

### E2E tests (GKE)

Config Sync e2e tests can also target clusters on GKE.

#### Prerequisites

Follow the [instructions to provision a dev environment].

#### Running E2E tests on GKE

To execute e2e multi-repo tests with a GKE cluster, build and push the Config Sync
images to GCR and then use go test. The images will be pushed to the GCP project
from you current gcloud context and the tests will execute on the cluster set as the
current context in your kubeconfig.
```shell
# Ensure gcloud context is set to correct project
gcloud config set project <PROJECT_ID>
# Ensure kubectl context is set to correct cluster
kubectl config set-context <CONTEXT>
# Build images
make build-images
# Push images to GCR
make push-images
# Run the tests with image prefix/tag from previous step and desired test regex
go test ./e2e/... --e2e --multirepo --debug --test.v --share-test-env=true --test.parallel=1 --image-prefix=(IMAGE_PREFIX) --image-tag=(IMAGE_TAG) --test-cluster=gke --test.run (test name regexp)
```

## Build
Config Sync can be built from source with a single command:

```
make config-sync-manifest
```

This will build all the docker images needed for Config Sync and generate
the manifests needed to run it. The images will by default be uploaded to 
Google Container Registry under your current gcloud project and the manifests
will be created in .output/staging/oss under the Config Sync directory.

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
