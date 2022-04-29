# CI Pipeline

This is an example for how to trigger a CloudBuild CI pipeline on a config directory.
We will ensure all namespaces have a cost-center label to adhere to the OPA Gatekeeper constraint.

See [our documentation](https://cloud.google.com/anthos-config-management/docs/how-to/policy-agent-ci-pipeline) for how to set up this example.

## Config Overview

This repository contains the following files.

```console
ci-pipeline/
├── cloudbuild.yaml # CloudBuild configuration file with which to set up a trigger
├── config-root
│   ├── cluster
│   │   ├── fulfillmentcenter-crd.yaml
│   │   ├── namespace-reader-clusterrolebinding.yaml
│   │   ├── namespace-reader-clusterrole.yaml
│   │   ├── pod-creator-clusterrole.yaml
│   │   ├── pod-security-policy.yaml
│   │   ├── required-labels-constraint.yaml # OPA Gatekeeper constraint to require cost-center labels on namespaces
│   │   └── required-labels-template.yaml # OPA Gatekeeper template for required labels
│   ├── namespaces
│   │   ├── audit
│   │   │   └── namespace.yaml
│   │   ├── online
│   │   │   └── shipping-app-backend
│   │   │       ├── pod-creator-rolebinding.yaml
│   │   │       ├── quota.yaml
│   │   │       ├── shipping-dev
│   │   │       │   ├── job-creator-rolebinding.yaml
│   │   │       │   ├── job-creator-role.yaml
│   │   │       │   └── namespace.yaml
│   │   │       ├── shipping-prod
│   │   │       │   ├── fulfillmentcenter.yaml
│   │   │       │   └── namespace.yaml
│   │   │       └── shipping-staging
│   │   │           ├── fulfillmentcenter.yaml
│   │   │           └── namespace.yaml
│   │   ├── sre-rolebinding.yaml
│   │   ├── sre-supported-selector.yaml
│   │   └── viewers-rolebinding.yaml
│   └── system
│       ├── README.md
│       └── repo.yaml
└── README.md
```
