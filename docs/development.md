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
Unit tests are small focused test that runs quickly. Run them with:
```
make test
```

Config Sync also has e2e tests. These run on [kind] and can take a long time
to finish. Run them with:
```
make test-e2e-go-ephemeral-multi-repo
```

## Build
The project can be built from source with a single command:

```
make build-oss
```

This will build all the docker images needed for Config Sync and generate
the manifests needed to run it. The images will by default be uploaded to 
Google Container Registry under your current gcloud project and the manifests
will be created in .output/oss/manifests under the Config Sync directory.

## Run
Running Config Sync is as simple as applying the generated manifests to your
cluster (from the Config Sync directory):

```
kubectl apply -f .output/oss/manifests
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