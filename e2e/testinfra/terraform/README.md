# Terraform Config

Config Sync uses Terraform to manage its GCP resources
used for end-to-end testing. This is primarily used for provisioning
test infra for CI jobs, but can also be used to provision test infra for
development.

## Install Terraform

Follow the instructions to [install the Terraform CLI].

# Dev Environments

The Terraform config for dev environments is defined in the [dev directory].
This configuration is designed to be configurable for an arbitrary GCP project
for development purposes.
As a result some variables must be provided at runtime, such as project name
and backend bucket name.

## Prerequisites

### Enable Cloud Source Repositories API

Follow the instructions to [enable the Cloud Source Repositories API].

### Provision tfstate GCS Bucket

Terraform supports a number of backends to store its state. We use GCS.
Create a GCS bucket in your project which will be used as the backend to store
Terraform's state. You will provide the name of this bucket during `terraform init`.

e.g.
```shell
gcloud storage buckets create gs://<PROJECT_ID>-tfstate --project=<PROJECT_ID>
```

## Usage

See the [variable definitions file] for an exhaustive list of variables.
These can be provided as [command line variables] at runtime.

For example:
```shell
export TF_VAR_project=my-project
terraform init -backend-config="bucket=<bucket-name>"
terraform plan
terraform apply
```

> **_Note:_**
If running on a new project, the first apply may fail due to google APIs not yet
being enabled. If this occurs, wait briefly and re-run `terraform apply`.
The APIs will have been enabled from the first apply and available for the
second apply.

To delete the provisioned resources:
```shell
terraform destroy
```

# Prow Environment

The Terraform config for our prow environment is defined in the [prow directory].

```shell
export TF_VAR_project=oss-prow-build-kpt-config-sync
export TF_VAR_prow=true
terraform init -backend-config="bucket=oss-prow-build-kpt-config-sync-tfstate"
terraform apply
```


[enable the Cloud Source Repositories API]: https://cloud.google.com/source-repositories/docs/create-code-repository#before-you-begin
[variable definitions file]: ./variables.tf
[command line variables]: https://www.terraform.io/language/values/variables#variables-on-the-command-line
[install the Terraform CLI]: https://learn.hashicorp.com/tutorials/terraform/install-cli
[prow directory]: ./prow
[dev directory]: ./dev
