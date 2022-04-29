# CI Pipeline - Unstructured

This is an example for how to create a CloudBuild CI pipeline on an unstructured directory, so called because the config-root of this directory does not follow the ACM repo structure.
We will change the ConfigMap to adhere to the OPA Gatekeeper constraint.

See [our documentation](https://cloud.google.com/anthos-config-management/docs/how-to/policy-agent-ci-pipeline) for how to set up this example.

## Config Overview

This repository contains the following files.

```console
ci-pipeline-unstructured/
├── config-root
│   ├── configmap.yaml
│   └── constraints
│       ├── banned-key-constraint.yaml # OPA Gatekeeper constraint to ban secrets in ConfigMaps
│       └── banned-key-template.yaml # OPA Gatekeeper template for banned ConfigMap keys
├── cloudbuild.yaml # CloudBuild configuration file with which to set up a trigger
└── README.md
```
