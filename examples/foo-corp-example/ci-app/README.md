# Validating your app against company policies in a continuous integration pipeline

This folder contains the code used in the [Validating your app against company policies in a continuous integration pipeline](https://cloud.google.com/anthos-config-management/docs/how-to/app-policy-validation-ci-pipeline)
tutorial.

If your organization uses [Anthos Config Management](https://cloud.google.com/anthos-config-management/docs)
and [Policy Controller](https://cloud.google.com/anthos-config-management/docs/concepts/policy-controller) to manage
policies across its Anthos clusters, then you can validate an app’s deployment
configuration in its continuous integration (CI) pipeline. This tutorial demonstrates
how to achieve this result. It will be useful to you if you are a developer building
a CI pipeline for an app, or a platform engineer building a CI pipeline template
for multiple app teams.

The CI pipeline that you use in this tutorial is implemented with [Cloud Build](https://cloud.google.com/cloud-build/docs)
and is represented below.

![ci-app-pipeline](../screenshots/ci-app-pipeline.svg)

## Config Overview

This repository contains the following files.

```
ci-app/
├── README.md
├── acm-repo/ # Repository for Anthos Config Management
│   ├── README.md
│   ├── cluster/ # cluster scoped objects
│   │   ├── deployment-must-have-owner.yaml # Constraint to enforce an "owner" label on Deployments
│   │   └── requiredlabels.yaml # ConstraintTemplate for K8sRequiredLabels
│   ├── clusterregistry/
│   ├── namespaces/
│   └── system/
│       ├── README.md
│       └── repo.yaml
└── app-repo/ # application repository
    ├── cloudbuild.yaml # Cloud Build configuration
    └── config/ # kustomize configuration
        ├── base/
        │   ├── deployment.yaml
        │   └── kustomization.yaml
        └── prod/
            └── kustomization.yaml
```