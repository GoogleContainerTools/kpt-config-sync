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

See [testing](./testing.md).

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
