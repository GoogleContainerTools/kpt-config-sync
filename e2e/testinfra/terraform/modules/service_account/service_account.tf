/**
 * Copyright 2022 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

data "google_project" "project" {
}

resource "google_service_account" "gcp_sa" {
  account_id   = var.gcp_sa_id
  display_name = var.gcp_sa_display_name
  description = var.gcp_sa_description
}

resource "google_service_account_iam_member" "k8s_sa_binding" {
  for_each = toset([
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler]", # The default RootSync used in e2e test.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns]", # The default RepoSync used in e2e test.
    # RootSync and RepoSync used in TestNomosStatusNameFilter.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-crontab-sync]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-bookinfo-bookinfo-repo-sync-18]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-bookinfo-crontab-sync-12]",
    # RootSync and RepoSync used in TestMultiSyncs_Unstructured_MixedControl.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-rr1]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-rr2]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-rr3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-nr1-3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-nr2-3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-nr3-3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-nr4-3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-nr5-3]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-ns-2-nr1-3]",
    # RootSync used in TestConflictingDefinitions_RootToRoot.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-root-test]",
    # RepoSync used in TestConflictingDefinitions_RootToRoot.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-rs-test-1-9]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-rs-test-2-9]",
    # RepoSync used in TestNamespaceRepo_Delegated, TestDeleteRepoSync_Delegated_AndRepoSyncV1Alpha1, TestDeleteRepoSync_Centralized_AndRepoSyncV1Alpha1, TestManageSelfRepoSync, TestDeleteNamespaceReconcilerDeployment, TestReconcilerManagerRootSyncCRDMissing.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-bookstore]",
    # RepoSync used in TestNoSSLVerifyV1Alpha1, TestNoSSLVerifyV1Beta1, TestOverrideGitSyncDepthV1Alpha1, TestOverrideGitSyncDepthV1Beta1, TestOverrideReconcilerResourcesV1Alpha1, TestOverrideReconcilerResourcesV1Beta1, TestSplitRSyncsWithDeletion.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-backend]",
    # RepoSync used in TestOverrideRepoSyncLogLevel, TestOverrideReconcilerResourcesV1Alpha1, TestOverrideReconcilerResourcesV1Beta1, TestSplitRSyncsWithDeletion.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-frontend]",
    # RootSync used in TestRootSyncRoleRefs, TestNamespaceStrategyMultipleRootSyncs.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-sync-a]",
    # RepoSync used in TestReconcilerFinalizer_MultiLevelForeground, TestReconcilerFinalizer_MultiLevelMixed.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-rs-test-7]",
    # RootSync used in TestReconcileFinalizerReconcileTimeout.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-nested-root-sync]",
    # RepoSync used in TestReconcilerManagerNormalTeardown.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-teardown]",
    # RepoSync used in TestReconcilerManagerTeardownInvalidRSyncs.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-invalid-teardown]",
    # RepoSync used in TestReconcilerManagerTeardownRepoSyncWithReconcileTimeout.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-reconcile-timeout]",
    # RootSync used in TestNamespaceStrategyMultipleRootSyncs, TestSplitRSyncsWithDeletion.
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-sync-x]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-sync-y]",
    # RootSync and RepoSync used in TestComposition
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-level-1]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-level-1-7]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-level-2-7]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns-level-3-7]",
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[oci-image-verification/oci-signature-verification-sa]",
    # RootSync used in TestMaxRootSyncNameLength
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy]",
    # RepoSync used in TestMaxRepoSyncNameLength
    "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-xxxxxxxxxx-yyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyyy-35]",
  ])
  service_account_id = google_service_account.gcp_sa.name
  role               = "roles/iam.workloadIdentityUser"
  member             = each.value
}

resource "google_project_iam_member" "gcp_sa_role" {
  for_each = toset(var.roles)

  role    = each.value
  member  = "serviceAccount:${google_service_account.gcp_sa.email}"
  project = data.google_project.project.id
}
