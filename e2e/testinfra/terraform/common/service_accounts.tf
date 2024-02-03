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

module "e2e-csr-reader-sa" {
  source = "../modules/service_account"
  gcp_sa_id = "e2e-test-csr-reader"
  gcp_sa_display_name = "Test CSR Reader"
  gcp_sa_description = "Service account used to read from Cloud Source Repositories"
  role = "roles/source.reader"
}

module "e2e-gar-reader-sa" {
  source = "../modules/service_account"
  gcp_sa_id = "e2e-test-ar-reader"
  gcp_sa_display_name = "Test GAR Reader"
  gcp_sa_description = "Service account used to read from Artifact Registry"
  role = "roles/artifactregistry.reader"
}

module "e2e-gcr-reader-sa" {
  source = "../modules/service_account"
  gcp_sa_id = "e2e-test-gcr-reader"
  gcp_sa_display_name = "Test GCR Reader"
  gcp_sa_description = "Service account used to read from Container Registry"
  role = "roles/storage.objectViewer"
}

data "google_project" "project" {
}

data "google_compute_default_service_account" "default" {
  depends_on = [
    google_project_service.services["compute.googleapis.com"]
  ]
}

resource "google_project_iam_member" "gce-default-sa-iam" {
  for_each = toset([
    "roles/source.reader",
    "roles/artifactregistry.reader",
    "roles/storage.objectViewer",
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
  ])

  role    = each.value
  member  = "serviceAccount:${data.google_compute_default_service_account.default.email}"
  project = data.google_project.project.id
}

resource "google_service_account" "e2e_metric_writer_sa" {
  account_id = "e2e-test-metric-writer"
  display_name = "Test Metric Writer"
  description = "Service account used to write to Google Cloud Monitoring"
}

resource "google_project_iam_member" "e2e_metric_writer_gcp_role" {
  role = "roles/monitoring.metricWriter"
  member = "serviceAccount:${google_service_account.e2e_metric_writer_sa.email}"
  project = data.google_project.project.id
}

resource "google_service_account_iam_member" "e2e_metric_writer_gke_binding" {
  service_account_id = google_service_account.e2e_metric_writer_sa.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-monitoring/default]"
}

# Grant source reader permissions to the RootSync's KSA with GKE workload identity.
resource "google_project_iam_member" "root-reconciler-wi-sa-iam" {
  for_each = toset([
    "roles/source.reader",
    "roles/artifactregistry.reader",
    "roles/storage.objectViewer",
  ])
  role    = each.value
  member             = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler]"
  project = data.google_project.project.id
}

// A Google service account that doesn't have any permissions.
resource "google_service_account" "e2e_restricted_user_sa" {
  account_id = "e2e-test-restricted-user"
  display_name = "Test service account without reader permission or WI binding"
  description = "Service account without reader permission or WI binding"
  project = data.google_project.project.id
}

// A Google service account that doesn't have any permissions, but has an IAM binding as a WI user.
resource "google_service_account" "e2e_restricted_and_impersonated_user_sa" {
  account_id = "e2e-test-restricted-wi-user"
  display_name = "Test impersonated service account without reader permission"
  description = "Service account that is impersonated but has no reader permission"
  project = data.google_project.project.id
}

resource "google_service_account_iam_member" "e2e_e2e_restricted_and_impersonated_user_gke_binding" {
  service_account_id = google_service_account.e2e_restricted_and_impersonated_user_sa.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler]"
}
