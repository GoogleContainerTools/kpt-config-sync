/**
 * Copyright 2023 Google LLC
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

resource "google_project_iam_member" "test-runner-iam" {
  for_each = toset([
    "roles/artifactregistry.admin",
    "roles/container.admin",
    "roles/gkehub.admin",
    "roles/monitoring.viewer",
    "roles/secretmanager.admin",
    "roles/source.admin",
    "roles/logging.viewer",
  ])

  role    = each.value
  member  = "serviceAccount:e2e-test-runner@oss-prow-build-kpt-config-sync.iam.gserviceaccount.com"
  project = data.google_project.project.id
}

resource "google_service_account_iam_member" "admin-account-iam" {
  service_account_id = "${data.google_project.project.id}/serviceAccounts/e2e-test-ar-reader@${data.google_project.project.project_id}.iam.gserviceaccount.com"
  role               = "roles/iam.serviceAccountKeyAdmin"
  member             = "serviceAccount:e2e-test-runner@oss-prow-build-kpt-config-sync.iam.gserviceaccount.com"
}
