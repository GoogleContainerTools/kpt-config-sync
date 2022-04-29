#!/bin/bash

set -euo pipefail

load "../lib/assert"
load "../lib/git"
load "../lib/namespace"
load "../lib/nomos"
load "../lib/setup"
load "../lib/wait"
load "../lib/resource"

FILE_NAME="$(basename "${BATS_TEST_FILENAME}" '.bats')"

test_setup() {
  setup::git::initialize
  setup::git::commit_minimal_repo_contents --skip_commit
}

test_teardown() {
  setup::git::remove_all acme
}

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata

@test "${FILE_NAME}: Check that default fields do not end up in NamespaceConfig" {
  debug::log "Add a deployment"
  git::add ${YAML_DIR}/dir-namespace.yaml acme/namespaces/dir/namespace.yaml
  git::add ${YAML_DIR}/deployment-helloworld.yaml acme/namespaces/dir/deployment.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced

  debug::log "Check that the deployment was created"
  wait::for -t 60 -- kubectl get deployment hello-world -n dir

  debug::log "Check that specified field name is present"
  selector=".spec.resources[0].versions[0].objects[0].metadata.name"
  name=$(kubectl get namespaceconfig dir -ojson | jq -c "${selector}")
  [[ "${name}" == '"hello-world"' ]] || debug::error "NamespaceConfig is missing Deployment hello-world"

  debug::log "Check that unspecified field creationTimestamp is not present"
  selector=".spec.resources[0].versions[0].objects[0].metadata"
  if [[ $(kubectl get namespaceconfig dir -ojson | jq -c "${selector}" | grep creationTimestamp) ]]; then
    debug::error "NamespaceConfig has default field creationTimestamp which was not specified"
  fi
}

@test "${FILE_NAME}: Recreate a sync deleted by the user" {
  debug::log "Add a deployment to a directory"
  git::add ${YAML_DIR}/dir-namespace.yaml acme/namespaces/dir/namespace.yaml
  git::add ${YAML_DIR}/deployment-helloworld.yaml acme/namespaces/dir/deployment.yaml
  git::commit -m "Add a deployment to a directory"
  wait::for -t 60 -- nomos::repo_synced

  debug::log "Ensure that the system created a sync for the deployment"
  wait::for -t 60 -- kubectl get sync deployment.apps

  debug::log "Force-delete the sync from the cluster"
  wait::for -t 60 -- kubectl delete sync deployment.apps

  debug::log "Ensure that the system re-created the force-deleted sync"
  wait::for -t 60 -- kubectl get sync deployment.apps
}

@test "${FILE_NAME}: Check that deleting clusterconfigs is recoverable" {
  git::add ${YAML_DIR}/dir-namespace.yaml acme/namespaces/dir/namespace.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced

  wait::for kubectl get ns dir

  debug::log "Forcefully delete clusterconfigs and verify recovery"
  kubectl delete clusterconfig --all

  git::add "${NOMOS_DIR}/examples/acme/cluster/admin-clusterrole.yaml" acme/cluster/admin-clusterrole.yaml
  git::commit

  wait::for -t 60 -- nomos::repo_synced
  wait::for -t 30 -- kubectl get clusterconfig config-management-cluster-config
}

@test "${FILE_NAME}: Namespace garbage collection" {
  mkdir -p acme/namespaces/accounting
  git::add ${YAML_DIR}/accounting-namespace.yaml acme/namespaces/accounting/namespace.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced
  wait::for kubectl get ns accounting
  git::rm acme/namespaces/accounting/namespace.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced
  wait::for -f -t 60 -- kubectl get ns accounting
}

@test "${FILE_NAME}: Namespace to Policyspace conversion" {
  git::add ${YAML_DIR}/dir-namespace.yaml acme/namespaces/dir/namespace.yaml
  git::commit
  wait::for nomos::repo_synced
  wait::for kubectl get ns dir

  git::rm acme/namespaces/dir/namespace.yaml
  git::add ${YAML_DIR}/subdir-namespace.yaml acme/namespaces/dir/subdir/namespace.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced

  wait::for kubectl get ns subdir
  wait::for -f -- kubectl get ns dir
}

@test "${FILE_NAME}: Sync deployment and replicaset" {
  # Test the ability to fix a mistake: overlapping replicaset and deployment.
  # Readiness behavior is undefined for this race condition.
  # One or both of the Deployment and ReplicaSet may become unhealthy.
  # But regardless, the user should be able to correct the situation.
  debug::log "Add a replicaset"
  git::add ${YAML_DIR}/dir-namespace.yaml acme/namespaces/dir/namespace.yaml
  git::add ${YAML_DIR}/replicaset-helloworld.yaml acme/namespaces/dir/replicaset.yaml
  git::commit
  # This sync may block until reconcile timeout is reached,
  # because ReplicaSet or Deployment may never reconcile.
  # So this wait timeout must be longer than the reconcile timeout (5m).
  wait::for -t 600 -- nomos::repo_synced

  debug::log "check that the replicaset was created"
  wait::for -t 60 -- resource::check -r replicaset -n dir -l "app=hello-world"

  debug::log "Add a corresponding deployment"
  git::add ${YAML_DIR}/deployment-helloworld.yaml acme/namespaces/dir/deployment.yaml
  git::commit
  wait::for -t 600 -- nomos::repo_synced

  debug::log "check that the deployment was created"
  wait::for -t 60 -- kubectl get deployment hello-world -n dir

  debug::log "Remove the deployment"
  git::rm acme/namespaces/dir/deployment.yaml
  git::commit
  # This sync may block until reconcile timeout is reached,
  # because the ReplicaSet is re-applied before deleting the Deployment.
  # So this wait timeout must be longer than the reconcile timeout (5m).
  wait::for -t 600 -- nomos::repo_synced

  debug::log "check that the deployment was removed and replicaset remains"
  wait::for -f -t 60 -- kubectl get deployment hello-world -n dir
  wait::for -t 60 -- resource::check -r replicaset -n dir -l "app=hello-world"
}

@test "${FILE_NAME}: RoleBindings updated" {
  git::add "${NOMOS_DIR}/examples/acme/namespaces/eng/backend/namespace.yaml" acme/namespaces/eng/backend/namespace.yaml
  git::add "${NOMOS_DIR}/examples/acme/namespaces/eng/backend/bob-rolebinding.yaml" acme/namespaces/eng/backend/br.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced
  wait::for -- kubectl get rolebinding -n backend bob-rolebinding

  run kubectl get rolebindings -n backend bob-rolebinding -o yaml
  assert::contains "acme-admin"

  git::update ${YAML_DIR}/robert-rolebinding.yaml acme/namespaces/eng/backend/br.yaml
  git::commit
  wait::for -t 60 -- nomos::repo_synced

  run kubectl get rolebindings -n backend bob-rolebinding
  assert::contains "NotFound"
  run kubectl get rolebindings -n backend robert-rolebinding -o yaml
  assert::contains "acme-admin"
}

function manage_namespace() {
  local ns="${1}"
  debug::log "Add an unmanaged resource into the namespace as a control"
  debug::log "We should never modify this resource"
  wait::for -t 5 -- \
    kubectl apply -f "${YAML_DIR}/reserved_namespaces/unmanaged-service.${ns}.yaml"

  debug::log "Add resource to manage"
  git::add "${YAML_DIR}/reserved_namespaces/service.yaml" \
    "acme/namespaces/${ns}/service.yaml"
  debug::log "Start managing the namespace"
  git::add "${YAML_DIR}/reserved_namespaces/namespace.${ns}.yaml" \
    "acme/namespaces/${ns}/namespace.yaml"
  git::commit -m "Start managing the namespace"
  wait::for -t 60 -- nomos::repo_synced

  debug::log "Wait until managed service appears on the cluster"
  wait::for -t 30 -- kubectl get services "some-service" --namespace="${ns}"

  debug::log "Remove the namespace directory from the repo"
  git::rm "acme/namespaces/${ns}"
  git::commit -m "Remove the namespace from the managed set of namespaces"
  wait::for -t 60 -- nomos::repo_synced

  debug::log "Wait until the managed resource disappears from the cluster"
  wait::for -f -t 60 -- kubectl get services "some-service" --namespace="${ns}"

  debug::log "Ensure that the unmanaged service remained"
  wait::for -t 60 -- \
    kubectl get services "some-other-service" --namespace="${ns}"
  kubectl delete service "some-other-service" \
    --ignore-not-found --namespace="${ns}"
}

# System namespace deletion is forbidden.
# We need to unmanage those namespaces in case we hit the forbidden error
# when resetting the repos to the initial state.
function unmanage_namespace() {
  local ns="${1}"
  debug::log "stop managing the system namespace"

  git::add "${YAML_DIR}/reserved_namespaces/unmanaged-namespace.${ns}.yaml" \
    "acme/namespaces/${ns}/namespace.yaml"
  git::commit -m "Stop managing the namespace"
  wait::for -t 60 -- nomos::repo_synced
}

@test "${FILE_NAME}: Namespace default can be managed" {
  manage_namespace "default"
  unmanage_namespace "default"
}

@test "${FILE_NAME}: Namespace gatekeeper-system can be managed" {
  namespace::create gatekeeper-system
  manage_namespace "gatekeeper-system"
  kubectl delete ns gatekeeper-system --ignore-not-found
}

@test "${FILE_NAME}: Namespace kube-system can be managed" {
  manage_namespace "kube-system"
  unmanage_namespace "kube-system"
}

@test "${FILE_NAME}: Namespace kube-public can be managed" {
  manage_namespace "kube-public"
  unmanage_namespace "kube-public"
}

function clean_test_configmaps() {
  kubectl delete configmaps -n new-prj --all > /dev/null
  kubectl delete configmaps -n newer-prj --all > /dev/null
  wait::for -f -- kubectl -n new-prj configmaps map1
  wait::for -f -- kubectl -n newer-prj configmaps map2
}
