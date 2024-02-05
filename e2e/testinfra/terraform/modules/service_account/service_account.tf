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

// Create GSA and workload identity bindings to the KSA(s)
resource "google_service_account" "gcp_sa" {
  account_id   = var.gcp_sa_id
  display_name = var.gcp_sa_display_name
  description = var.gcp_sa_description
}

resource "google_service_account_iam_member" "k8s_sa_binding" {
  service_account_id = google_service_account.gcp_sa.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler]"
}

resource "google_service_account_iam_member" "k8s_sa_ns_binding" {
  service_account_id = google_service_account.gcp_sa.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns]"
}

resource "google_project_iam_member" "gcp_sa_role" {
  role    = var.role
  member  = "serviceAccount:${google_service_account.gcp_sa.email}"
  project = data.google_project.project.id
}

// Create IAM bindings directly to the KSA(s) for BYOID
resource "google_project_iam_member" "k8s_sa_role_rootsync" {
  role    = var.role
  member  = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/root-reconciler]"
  project = data.google_project.project.id
}

resource "google_project_iam_member" "k8s_sa_role_reposync" {
  role    = var.role
  member  = "serviceAccount:${data.google_project.project.project_id}.svc.id.goog[config-management-system/ns-reconciler-test-ns]"
  project = data.google_project.project.id
}