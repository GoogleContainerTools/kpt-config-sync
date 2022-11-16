# Directory structure
This directory includes the configs for kubevirt:
  * kubevirt-operator.yaml includes the configs of kubevirt-operator
  * kubevirt-cr.yaml includes the config of a kubvirt CR
  * ns-bookstore1.yaml includes the config of a namespace called bookstore1
  * vm.yaml includes the config of a VirtualMachine CR under the bookstore1
    namespace

# Purpose of this example
* test `nomos hydrate --source-format=unstructured` generates the correct output (the `TestNomosHydrateWithUnknownScopedObject` e2e test in https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/e2e/testcases/cli_test.go);
* verify that Config Sync can apply resources with known scopes instead of applying nothing when resources with unknown scope are encountered https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/e2e/testcases/apply_scoped_resource_test.go (design doc: go/config-sync-apply-scoped-resources).

# Updating kubevirt-operator

We may need to update the kubevirt-operator version sometimes. For example,
https://github.com/GoogleContainerTools/kpt-config-sync/pull/297 updates the
kubevirt version from v0.46.1 to v0.58.0. v0.46.0 uses the policy/v1beta1 API
version of PodDisruptionBudget. which is no longer served in k8s v1.25.
https://kubernetes.io/docs/reference/using-api/deprecation-guide/#poddisruptionbudget-v125

Here are the steps for updating kubevirt-operator:

* Find the kubevirt release you plan to use under https://github.com/kubevirt/kubevirt/releases.
* Download the `kubevirt-operator.yaml`
  ```
  wget https://github.com/kubevirt/kubevirt/releases/download/v0.58.0/kubevirt-operator.yaml -O kubevirt-operator.yaml
  ```
* Add the license header into kubevirt-oprator.yaml
  ```
  addlicense -v -c "Google LLC" -f ../../LICENSE_TEMPLATE -ignore=vendor/** kubevirt-operator.yaml 2>&1 | sed '/ skipping: / d'
  ```
* Update the `../kubevirt-compiled` directory
  ```
  cd ..
  rm -rf kubevirt-compiled
  nomos hydrate --path=kubevirt --source-format=unstructured --output kubevirt-compiled
  addlicense -v -c "Google LLC" -f ../LICENSE_TEMPLATE -ignore=vendor/** kubevirt-compiled 2>&1 | sed '/ skipping: / d'
  ```
